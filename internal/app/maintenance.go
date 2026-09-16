package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
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

func Collect(ctx context.Context, config Config, options workflow.RetentionOptions) (workflow.RetentionResult, error) {
	empty := workflow.RetentionResult{Before: time.Now().UTC().Add(-options.OlderThan), DryRun: options.DryRun, Items: []workflow.CleanupItem{}}
	if options.OlderThan < 0 {
		return empty, state.ErrInvalid
	}
	root := config.Repository
	if root == "" {
		root = "."
	}
	repo, err := git.Open(ctx, root, config.GitExecutable)
	if err != nil {
		return empty, err
	}
	if _, err = os.Stat(config.DBPath); errors.Is(err, os.ErrNotExist) {
		return empty, nil
	} else if err != nil {
		return empty, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: options.DryRun})
	if err != nil {
		return empty, err
	}
	defer store.Close()
	repository, err := store.FindRepository(ctx, repo.CommonDir)
	if errors.Is(err, state.ErrNotFound) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if config.Tart.ArtifactDirectory == "" {
		config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	engine := workflow.Engine{State: store, Repository: repository.ID, Providers: map[string]verify.Provider{}}
	if !options.DryRun {
		engine.Providers["tart"] = &tart.Provider{State: store, Repository: repository.ID, Config: config.Tart}
	}
	engine.Providers["github"] = &githubverify.Provider{State: store, Repository: repository.ID, Directory: filepath.Join(filepath.Dir(store.Path()), "github-verification")}
	result, err := engine.Collect(ctx, options)
	if err != nil {
		return result, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return result, err
	}
	roots := []string{filepath.Join(config.Tart.ArtifactDirectory, "indexes"), filepath.Join(cache, "dockhand", "indexes")}
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
		items, err := portindex.Collect(ctx, root, result.Before, options.DryRun)
		for _, item := range items {
			result.Items = append(result.Items, workflow.CleanupItem{Action: "prune-index-cache", Path: item.Path, Completed: item.Completed})
		}
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
