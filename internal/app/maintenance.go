package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
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
	engine := workflow.Engine{State: store, Repository: repository.ID}
	if !options.DryRun {
		if config.Tart.ArtifactDirectory == "" {
			config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
		}
		engine.Provider = &tart.Provider{State: store, Repository: repository.ID, Config: config.Tart}
	}
	return engine.Collect(ctx, options)
}
