package engine

import (
	"context"
	"io/fs"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
)

// Backup copies the database at database to into, whole or not at all,
// without opening an engine over it: no migration, no session, no ports
// tree (sqlite.Backup). So a build backs up a database it would migrate
// before its first migration, and one it can't otherwise open.
func Backup(ctx context.Context, database, into string) (store.Backup, error) {
	return sqlite.Backup(ctx, database, into, sqlite.Options{})
}

// KeptArchives is where the database's kept archives are, every
// checkout's (ArchiveDirectory), and how many bytes they hold: what a
// backup of the database leaves out.
func KeptArchives(database string) (string, int64) {
	directory := archivesRoot(database)
	var total int64
	_ = filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return directory, total
}
