package app

import (
	"context"
	"errors"
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
	ID record.RepositoryID
	// CommonDir is the registered Git common directory.
	CommonDir string
	// CheckoutMissing means the common directory no longer exists; nothing
	// can resume that registration's work, so releasing it is always safe.
	CheckoutMissing bool
}

// CollectResult is the retention result plus the registrations it covered.
type CollectResult struct {
	workflow.RetentionResult
	Registrations []Registration `json:",omitempty"`
}

func Collect(ctx context.Context, config Config, options CollectOptions) (CollectResult, error) {
	retention := options.Retention
	result := CollectResult{RetentionResult: workflow.RetentionResult{Before: time.Now().UTC().Add(-retention.OlderThan), DryRun: retention.DryRun, Items: []workflow.CleanupItem{}}}
	if retention.OlderThan < 0 {
		return result, state.ErrInvalid
	}
	var registered []record.Repository
	if !options.AllRepositories {
		root := config.Repository
		if root == "" {
			root = "."
		}
		repo, err := git.Open(ctx, root, config.GitExecutable)
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
		if !retention.DryRun {
			engine.Providers["tart"] = &tart.Provider{State: store, Repository: repository.ID, Config: config.Tart}
		}
		engine.Providers["github"] = &githubverify.Provider{State: store, Repository: repository.ID, Directory: filepath.Join(filepath.Dir(store.Path()), "github-verification")}
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
	return result, nil
}
