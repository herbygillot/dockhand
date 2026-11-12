package sweep

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
		_, err = db.Exec(`UPDATE copied SET sequence = sequence + 1000, kind = 'waiting'`)
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

// A checkpoint is named by its place among those not abandoned: a retry's
// tidy-2 after an abandoned tidy-1 reads as the clean run's tidy-1, in its
// row, its refs and its event, and the abandoned row is allowed; a ref
// left under the abandoned number still shows (#129).
func TestCheckpointsAreNamedByTheirPlace(t *testing.T) {
	t.Parallel()
	clean := world{
		"refs clone":        {"refs/dockhand/checkpoints/tidy-1 commit:a", "refs/dockhand/checkpoints/tidy-1-index commit:b"},
		"table checkpoints": {`number=1 kind="tidy" state="applied"`},
		"table events":      {`kind="branch.tidy" message="tidied 0 commits into 1 commit (checkpoint tidy-1)"`},
	}
	retried := world{
		"refs clone":        {"refs/dockhand/checkpoints/tidy-2 commit:a", "refs/dockhand/checkpoints/tidy-2-index commit:b"},
		"table checkpoints": {`number=1 kind="tidy" state="abandoned"`, `number=2 kind="tidy" state="applied"`},
		"table events":      {`kind="branch.tidy" message="tidied 0 commits into 1 commit (checkpoint tidy-2)"`},
	}
	require.Empty(t, differences(renumber(clean), renumber(retried)))
	left := retried
	left["refs clone"] = append(slices.Clone(left["refs clone"]), "refs/dockhand/checkpoints/tidy-1 commit:a")
	require.NotEmpty(t, differences(renumber(clean), renumber(left)), "a ref left under the abandoned number shows")
}

// The oracle keeps what normalizing erased (#129's fix list, 5 to 7, from
// the outside read's probes): a time's order among its row's times, a
// record's name covering the records it points at, a duplicate row, and
// an event read back as state.
func TestTheOracleKeepsWhatTheDesignKeeps(t *testing.T) {
	t.Parallel()
	// An attempt ending before it starts, on one day.
	started := world{"table attempts": {`id="att_aaaaaaaaaaaaaaaa" started_at=«t:1794448800000» ended_at=«t:1794452400000»`}}
	reversed := world{"table attempts": {`id="att_aaaaaaaaaaaaaaaa" started_at=«t:1794452400000» ended_at=«t:1794448800000»`}}
	require.NotEmpty(t, differences(normalize(started, "/unused"), normalize(reversed, "/unused")), "an attempt ending before it starts differs")
	// Evidence pointing at the attempt of another run, whose port is curl.
	runs := []string{`id="run_cccccccccccccccc" port="jq"`, `id="run_dddddddddddddddd" port="curl"`}
	attempts := []string{`id="att_aaaaaaaaaaaaaaaa" run_id="run_cccccccccccccccc" state="passed"`, `id="att_bbbbbbbbbbbbbbbb" run_id="run_dddddddddddddddd" state="passed"`}
	jq := world{"table attempts": attempts, "table runs": runs, "table evidence": {`attempt_id="att_aaaaaaaaaaaaaaaa"`}}
	curl := world{"table attempts": attempts, "table runs": runs, "table evidence": {`attempt_id="att_bbbbbbbbbbbbbbbb"`}}
	require.NotEmpty(t, differences(normalize(jq, "/unused"), normalize(curl, "/unused")), "evidence pointing at curl rather than jq differs")
	require.Empty(t, differences(normalize(jq, "/unused"), normalize(jq, "/unused")))
	// A duplicate row.
	require.NotEmpty(t, differences(world{"table holds": {`run_id="run:a" port="jq"`}}, world{"table holds": {`run_id="run:a" port="jq"`, `run_id="run:a" port="jq"`}}), "an extra identical row differs")
	// A newer waiting event, which LatestRunEvent reads as the run's state.
	waited := []string{`sequence=1 run_id="run:a" kind="waiting" message="old wait"`}
	require.NotEmpty(t, eventDifferences(waited, append(slices.Clone(waited), `sequence=2 run_id="run:a" kind="waiting" message="wrong current wait"`)), "an added waiting event changes what the run is said to be doing")
}

// Every event kind a store reader is asked for by kind outside tests,
// LatestRunEvent, LatestBranchEvent or CountEvents, is one the oracle
// compares exactly, and no other is (#129's fix list, 7): the list held
// serve.submit, which serve's count no longer reads, and not the progress
// and waiting events status, wait and the driver read.
func TestTheEventsReadAsStateAreListed(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	kinds := map[string]string{}
	sources, err := filepath.Glob(filepath.Join(root, "model", "*.go"))
	require.NoError(t, err)
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok || len(spec.Values) != len(spec.Names) {
				return true
			}
			for i, name := range spec.Names {
				if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.HasPrefix(name.Name, "Event") {
					kinds[name.Name], _ = strconv.Unquote(lit.Value)
				}
			}
			return true
		})
	}
	read := map[string]bool{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// The store's own implementations answer the readers; they ask
		// nothing of them.
		if err == nil && d.IsDir() && filepath.Dir(path) == filepath.Join(root, "store") {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "LatestRunEvent" && selector.Sel.Name != "LatestBranchEvent" && selector.Sel.Name != "CountEvents" {
				return true
			}
			for _, arg := range call.Args {
				kind, ok := arg.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if value, ok := kinds[kind.Sel.Name]; ok {
					read[value] = true
				}
			}
			return true
		})
		return nil
	}))
	require.NotEmpty(t, read, "the listing found the readers")
	require.ElementsMatch(t, slices.Sorted(maps.Keys(read)), stateEvents)
}
