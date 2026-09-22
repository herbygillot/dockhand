package app

import (
	"context"
	"errors"
	"github.com/herbygillot/dockhand/internal/scratch"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	githubverify "github.com/herbygillot/dockhand/internal/verify/github"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
)

func BackupDatabase(ctx context.Context, config Config, destination string) (state.Backup, error) {
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true, AllowOlderSchema: true})
	if err != nil {
		return state.Backup{}, err
	}
	defer store.Close()
	return store.Backup(ctx, destination)
}

func CheckDatabase(ctx context.Context, config Config) error {
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true, AllowOlderSchema: true})
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Check(ctx)
}

func MigrateDatabase(ctx context.Context, config Config) error {
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{RequireExisting: true})
	if err != nil {
		return err
	}
	return store.Close()
}

// CollectOptions selects what gc covers. AllRepositories reaches every
// registration in the database, including ones whose checkout is gone and
// could otherwise never release their environments.
type CollectOptions struct {
	Retention       workflow.RetentionOptions
	AllRepositories bool
}

// Registration describes one repository gc visited.
type Registration struct {
	ID record.RepositoryID `json:"id"`
	// CommonDir is the registered Git common directory.
	CommonDir string `json:"common_dir"`
	// CheckoutMissing means the common directory no longer exists; nothing
	// can resume that registration's work, so releasing it is always safe.
	CheckoutMissing bool `json:"checkout_missing,omitempty"`
}

// DatabaseCheck is db check's result.
type DatabaseCheck struct {
	Valid bool `json:"valid"`
}

// DatabaseMigration is db migrate's result.
type DatabaseMigration struct {
	Current bool `json:"current"`
}

// CollectResult is the retention result plus the registrations it covered.
type CollectResult struct {
	workflow.RetentionResult
	Registrations []Registration `json:"registrations,omitempty"`
}

func Collect(ctx context.Context, config Config, options CollectOptions) (CollectResult, error) {
	retention := options.Retention
	result := CollectResult{RetentionResult: workflow.RetentionResult{Before: time.Now().UTC().Add(-retention.OlderThan), DryRun: retention.DryRun, Items: []workflow.CleanupItem{}}}
	if retention.OlderThan < 0 {
		return result, state.ErrInvalid
	}
	var registered []record.Repository
	if !options.AllRepositories {
		repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
		if err != nil {
			return result, err
		}
		registered = []record.Repository{{CommonDir: repo.CommonDir}}
	}
	if _, err := os.Stat(config.DBPath); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: retention.DryRun})
	if err != nil {
		return result, err
	}
	defer store.Close()
	if options.AllRepositories {
		registered, err = store.Repositories(ctx)
		if err != nil {
			return result, err
		}
	} else {
		repository, err := store.FindRepository(ctx, registered[0].CommonDir)
		if errors.Is(err, state.ErrNotFound) {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		registered = []record.Repository{repository}
	}
	if config.Tart.ArtifactDirectory == "" {
		config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	for _, repository := range registered {
		registration := Registration{ID: repository.ID, CommonDir: repository.CommonDir}
		if _, err := os.Stat(repository.CommonDir); errors.Is(err, os.ErrNotExist) {
			registration.CheckoutMissing = true
		}
		result.Registrations = append(result.Registrations, registration)
		engine := workflow.Engine{State: store, Repository: repository.ID, Providers: map[string]verify.Provider{}}
		if !registration.CheckoutMissing {
			// Branch cleanup needs the checkout; a registration without one still collects resources.
			if repo, err := git.Open(ctx, checkoutRoot(repository.CommonDir), config.GitExecutable); err == nil {
				engine.Repo = repo
			}
		}
		if !retention.DryRun {
			engine.Providers[verify.ProviderTart] = &tart.Provider{State: store, Repository: repository.ID, Config: config.Tart}
		}
		engine.Providers[verify.ProviderGitHub] = &githubverify.Provider{State: store, Repository: repository.ID, Directory: filepath.Join(filepath.Dir(store.Path()), "github-verification")}
		collected, err := engine.Collect(ctx, retention)
		result.Before = collected.Before
		for i := range collected.Items {
			collected.Items[i].Repository = repository.ID
		}
		result.Items = append(result.Items, collected.Items...)
		if err != nil {
			return result, err
		}
	}
	shared, err := indexCacheDirectory(config)
	if err != nil {
		return result, err
	}
	// The artifact-directory cache is the legacy Tart location; it is only collected.
	roots := []string{shared, filepath.Join(config.Tart.ArtifactDirectory, "indexes")}
	seen := map[string]bool{}
	for _, root := range roots {
		root, err = filepath.Abs(root)
		if err != nil {
			return result, err
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		items, err := portindex.Collect(ctx, root, result.Before, retention.DryRun)
		for _, item := range items {
			result.Items = append(result.Items, workflow.CleanupItem{Action: "prune-index-cache", Path: item.Path, Completed: item.Completed})
		}
		if err != nil {
			return result, err
		}
	}
	// Run roots of processes that died without cleaning up, told apart by
	// the lock a live process holds on its root, and the transient
	// directories of builds before run roots, told apart only by age: an
	// hour outlives any one operation of theirs, and --older-than governs
	// retained evidence, which these are not.
	legacyBefore := time.Now().Add(-legacyAge)
	stale, err := scratch.Stale(legacyBefore)
	if !retention.DryRun {
		stale, err = scratch.Sweep(legacyBefore)
	}
	for _, directory := range stale {
		result.Items = append(result.Items, workflow.CleanupItem{Action: "remove-stale-run-directory", Path: directory, Completed: !retention.DryRun})
	}
	return result, err
}

// checkoutRoot maps a registration's common Git directory to its main
// checkout; a bare or unusual layout is opened as recorded.
// legacyAge is how old a transient directory of an earlier build must be
// before gc removes it.
const legacyAge = time.Hour

func checkoutRoot(commonDir string) string {
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir)
	}
	return commonDir
}
