// Package sqlitetest gives tests a new dockhand database without each
// running every migration: a fresh database takes the schema's whole
// history, about fifty migrations, each in its own transaction synced to
// the disk, and hundreds of tests open one. Only tests import it.
//
// The store's own tests of opening and migrating, its guards among them,
// open a new file with sqlite.Open as ever; a test that is about what a
// first open does does so too.
package sqlitetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/store/sqlite"
)

var (
	once     sync.Once
	template []byte
	failure  error
)

// Fresh writes at path, making its directory, a database at this build's
// schema that holds nothing, as sqlite.Open makes a new one, and returns
// path. The test binary migrates one database the first time it's asked,
// in a temporary directory it then removes, and each call copies it, so
// opening the copy migrates nothing.
func Fresh(t testing.TB, path string) string {
	t.Helper()
	once.Do(func() { template, failure = migrate() })
	if failure != nil {
		t.Fatalf("sqlitetest: %v", failure)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, template, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// migrate makes a new database with sqlite.Open, closes it, and returns
// its file. Closing the last connection checkpoints the write-ahead log
// into the file and removes it, so the file alone is the database; one
// left beside it would mean the file isn't.
func migrate() ([]byte, error) {
	directory, err := os.MkdirTemp("", "dockhand-sqlitetest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "dockhand.db")
	s, err := sqlite.Open(context.Background(), path, sqlite.Options{})
	if err != nil {
		return nil, err
	}
	if err := s.Close(); err != nil {
		return nil, err
	}
	switch _, err := os.Stat(path + "-wal"); {
	case err == nil:
		return nil, errors.New("the migrated database's write-ahead log outlived its close")
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	return os.ReadFile(path)
}
