package sweep

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// updated is a World with jq updated and tidied in its own branch, and
// the GitHub stand-in.
func updated(t *testing.T) (cmdtest.World, *forgetest.GitHub) {
	t.Helper()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	for _, args := range [][]string{{"start", "jq-update"}, {"update", "jq", "-b", "jq-update"}, {"tidy", "-b", "jq-update", "-y"}} {
		_, errs, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err, "%v: %s", args, errs)
	}
	return w, g
}

// Two runs of one workflow, each in a world of its own, compare the same:
// ids, times and the temporary root are normalized, nothing else.
func TestTwoCleanRunsCompareTheSame(t *testing.T) {
	t.Parallel()
	a, ga := updated(t)
	b, gb := updated(t)
	require.Empty(t, differences(snapshot(t, a, ga), snapshot(t, b, gb)))
}

// Two worlds made different on purpose compare different: a commit with
// another parent, the same status over different content, an event
// again, a pull request left a draft, and a row pointing at another
// record (docs/designs/fault-sweep.md, "The oracle and the stand-ins are
// tested first").
func TestTheOracleSeesWhatItShould(t *testing.T) {
	t.Parallel()
	// Each case in a world of its own.
	fresh := func(t *testing.T) (cmdtest.World, *forgetest.GitHub, world, string) {
		w, g := updated(t)
		return w, g, snapshot(t, w, g), filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	}

	t.Run("a commit with another parent", func(t *testing.T) {
		t.Parallel()
		w, g, clean, worktree := fresh(t)
		// The same tree and message on another parent.
		head := strings.TrimSpace(testsupport.Git(t, worktree, "rev-parse", "HEAD"))
		tree := strings.TrimSpace(testsupport.Git(t, worktree, "rev-parse", "HEAD^{tree}"))
		message := testsupport.Git(t, worktree, "log", "-1", "--format=%B")
		root := strings.TrimSpace(testsupport.Git(t, worktree, "rev-list", "--max-parents=0", "HEAD"))
		other := strings.TrimSpace(testsupport.Git(t, worktree, "commit-tree", tree, "-p", root, "-m", message))
		require.NotEqual(t, head, other)
		testsupport.Git(t, worktree, "update-ref", "refs/heads/dockhand/jq-update", other, head)
		require.NotEmpty(t, differences(clean, snapshot(t, w, g)))
	})
	t.Run("the same status over different content", func(t *testing.T) {
		t.Parallel()
		w, g, _, worktree := fresh(t)
		portfile := filepath.Join(worktree, "textproc/jq/Portfile")
		data, err := os.ReadFile(portfile)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(portfile, append(data, "# one\n"...), 0o644))
		one := snapshot(t, w, g)
		require.NoError(t, os.WriteFile(portfile, append(data, "# two\n"...), 0o644))
		two := snapshot(t, w, g)
		require.NotEmpty(t, differences(one, two))
	})
	t.Run("an event read as state again", func(t *testing.T) {
		t.Parallel()
		w, g, clean, _ := fresh(t)
		db, err := sql.Open("sqlite", filepath.Join(w.Home, ".dockhand", "dockhand.db"))
		require.NoError(t, err)
		defer db.Close()
		_, err = db.Exec(`CREATE TEMP TABLE copied AS SELECT * FROM events ORDER BY sequence DESC LIMIT 1`)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE copied SET sequence = sequence + 1000, kind = 'serve.submit'`)
		require.NoError(t, err)
		result, err := db.Exec(`INSERT INTO events SELECT * FROM copied`)
		require.NoError(t, err)
		added, _ := result.RowsAffected()
		require.EqualValues(t, 1, added)
		require.NotEmpty(t, differences(clean, snapshot(t, w, g)))
	})
	t.Run("a pull request left a draft", func(t *testing.T) {
		t.Parallel()
		w, g, _, _ := fresh(t)
		_, errs, err := cmdtest.Dockhand(t, "submit", "-b", "jq-update", "--no-check", "--yes")
		require.NoError(t, err, errs)
		submitted := snapshot(t, w, g)
		for number := range g.PRs {
			_, err := g.MarkDraft(t.Context(), g.PRs[number].Ref)
			require.NoError(t, err)
		}
		require.NotEmpty(t, differences(submitted, snapshot(t, w, g)))
	})
	t.Run("a row pointing at another record", func(t *testing.T) {
		t.Parallel()
		w, g, _, _ := fresh(t)
		db, err := sql.Open("sqlite", filepath.Join(w.Home, ".dockhand", "dockhand.db"))
		require.NoError(t, err)
		defer db.Close()
		before := snapshot(t, w, g)
		var edit string
		require.NoError(t, db.QueryRow(`SELECT id FROM edits LIMIT 1`).Scan(&edit))
		_, err = db.Exec(`UPDATE edits SET branch_id = 'br_aaaaaaaaaaaaaaaa' WHERE id = ?`, edit)
		require.NoError(t, err)
		require.NotEmpty(t, differences(before, snapshot(t, w, g)))
	})
}
