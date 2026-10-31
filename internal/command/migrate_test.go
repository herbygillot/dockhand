package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
	"github.com/herbygillot/dockhand/internal/term"
)

// A database a release last opened is migrated by a development build
// only at the person's word on a terminal: answered no, nothing changes
// and the refusal stands; answered yes, it migrates, keeping the old form,
// where no command test reached the question (the test analysis's step
// b).
func TestAReleasesDatabaseMigratesAtThePersonsWord(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	// The database where dockhand keeps it: one DOCKHAND_DB names, as a
	// copy, migrates unasked.
	path := filepath.Join(w.Home, ".dockhand", "dockhand.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, sqlite.CreateAt(path, 32, "INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);\n"+
		"INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_1','foreground',101,'p','v0.4.0 (5897a828e1c2)',1,1,2);"))

	ask := func(answer string) (string, error) {
		var out, errs bytes.Buffer
		err := cmdtest.RunFor(t, t.Context(), []string{"status"}, term.Streams{In: strings.NewReader(answer), Out: &out, Err: &errs}.AsTerminal())
		return errs.String(), err
	}
	said, err := ask("n\n")
	require.ErrorContains(t, err, "v0.4.0")
	require.Contains(t, said, "? Migrate it anyway? Its schema 32 form is kept at "+path+".schema-32. [y/N] ")
	require.NoFileExists(t, path+".schema-32", "nothing kept or migrated when the answer is no")

	said, err = ask("y\n")
	require.NoError(t, err, said)
	require.FileExists(t, path+".schema-32", "the release's form is kept")
	_, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err, "migrated, it opens unasked")
}
