package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
)

// HoldService takes the one lock a serve holds for its database, so a second
// serve against the same state refuses to start rather than doubling every
// poll. Closing the result releases it.
func HoldService(ctx context.Context, config Config) (io.Closer, error) {
	path := filepath.Join(filepath.Dir(config.DBPath), "serve.lock")
	wait, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	held, err := filelock.Acquire(wait, path, filelock.Exclusive)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("another dockhand serve holds %s", path)
	}
	return held, err
}

// RegisteredCheckouts lists the checkout directory of every repository
// registered in the state database that still exists on disk, for a serve
// that has no checkout of its own.
func RegisteredCheckouts(ctx context.Context, config Config) ([]string, error) {
	if _, err := os.Stat(config.DBPath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer store.Close()
	registered, err := store.Repositories(ctx)
	if err != nil {
		return nil, err
	}
	var checkouts []string
	for _, repository := range registered {
		checkout := repository.CommonDir
		if filepath.Base(checkout) == ".git" {
			checkout = filepath.Dir(checkout)
		}
		if info, err := os.Stat(checkout); err != nil || !info.IsDir() {
			continue
		}
		checkouts = append(checkouts, checkout)
	}
	return checkouts, nil
}
