// Package backup is dockhand backup: a consistent copy of the database,
// to a file or a directory, made without opening the engine (#92,
// docs/designs/db-backup.md).
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// Command is dockhand backup <file|directory>.
func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup <file | directory>",
		Short: "Copy the database, consistently, to a file",
		Long: `Copies Dockhand's database (--db, DOCKHAND_DB, or ~/.dockhand/dockhand.db)
to a file, consistently, even while serve or a check is writing to it.
The copy is one SQLite file, readable only by you.

Given a directory, the copy is named by the time it was made:
dockhand-2026-11-05-2114.db. An existing file is never replaced.

Backup reads the database without migrating it, so a newer Dockhand can
back up a database before its first migration, and an older one can back
up a database it can't otherwise open.

The copy holds the database alone. Kept build archives and check logs
are not in it: a check run from the copy builds again what it can't
find. dockhand usage configuration says how to put a backup back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := s.ComposeOptions()
			places := options.Places()
			database, err := options.DatabasePath()
			if err != nil {
				return err
			}
			into, err := places.Absolute(args[0])
			if err != nil {
				return err
			}
			if info, err := os.Stat(into); err == nil && info.IsDir() {
				into = filepath.Join(into, "dockhand-"+time.Now().Format("2006-01-02-1504")+".db")
			}
			made, err := engine.Backup(cmd.Context(), database, into)
			if err != nil {
				return err
			}
			archives, kept := engine.KeptArchives(database)
			result := map[string]any{"source": made.Source, "copy": made.Copy, "schema": made.Schema, "bytes": made.Bytes}
			if made.Build != "" {
				result["build"], result["opened_at"] = made.Build, made.OpenedAt.UTC()
			}
			streams.Emit(result)
			words := fmt.Sprintf("Copied %s, schema %d", places.Tilde(made.Source), made.Schema)
			if made.Build != "" {
				words += fmt.Sprintf(", last opened by dockhand %s at %s", made.Build, made.OpenedAt.Local().Format("2006-01-02 15:04"))
			}
			words += fmt.Sprintf(", to %s (%s).", places.Tilde(made.Copy), prose.Bytes(made.Bytes))
			left := "Kept archives and check logs aren't"
			if kept > 0 {
				left = fmt.Sprintf("Kept archives (%s in %s) and check logs aren't", prose.Bytes(kept), places.Tilde(archives))
			}
			out := streams.Prose()
			fmt.Fprintln(out, words)
			fmt.Fprintln(out, left+" in the copy: a check run from it builds again what it can't find, and says once which archives it dropped.")
			return nil
		},
	}
	settings.SupportsJSON(cmd)
	return cmd
}
