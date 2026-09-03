package cmd

// The write verbs at selector scale.
//
// What is pinned here is the contract a script depends on: one row per
// port on stdout, in a shape that does not change; a census on stderr;
// exit 0 when every port was handled or declined and 83 when something
// broke; and the refusals for the flags that mean one port and cannot
// mean many.
//
// The verb under test is bump-revision wherever a real mint is needed.
// It is the one write intent that goes nowhere near the network, so a
// sweep of it is a sweep of the whole road — resolve, filter, plan,
// mint, report — with nothing stubbed and nothing fetched.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// sweepPortfile is a port with a revision to increment and nothing
// else: no distfiles, no checksums, no upstream. A revision bump needs
// none of it, which is what makes this road testable with no network.
func sweepPortfile(name, extra string) string {
	return "PortSystem          1.0\n\n" + extra +
		"name                " + name + "\n" +
		"version             1.0\n" +
		"revision            2\n" +
		"categories          devel\n" +
		"maintainers         nomaintainer\n" +
		"license             MIT\n" +
		"description         synthetic sweep target\n" +
		"long_description    ${description}\n"
}

// sweepTree writes a ports tree of the given ports under devel/, each
// under its own name so each has its own branch, and returns the root.
// The PortGroup directory is what makes it a ports tree.
func sweepTree(t *testing.T, ports map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	for name, body := range ports {
		dir := filepath.Join(root, "devel", name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(body), 0o644))
	}
	return root
}

// threePorts is the standard fixture: three ordinary ports in one
// category, in a git repository so branches can be minted.
func threePorts(t *testing.T) (string, *git.Repo) {
	t.Helper()
	pinGitDates(t)
	root := sweepTree(t, map[string]string{
		"alpha": sweepPortfile("alpha", ""),
		"beta":  sweepPortfile("beta", ""),
		"gamma": sweepPortfile("gamma", ""),
	})
	return root, gittest.Init(t, testFinder(), root, nil)
}

// revbumpSweep runs `bump-revision` over a selector against a tree.
func revbumpSweep(t *testing.T, root string, args ...string) transcript {
	t.Helper()
	return captureExecute(t, append([]string{"bump-revision", "--tree", root,
		"--reason", "a stated reason", "--no-verify"}, args...)...)
}

// rows parses a sweep's stdout as NDJSON, failing on any line that is
// not one object — the stream's whole promise.
func rows(t *testing.T, stdout string) []sweepRow {
	t.Helper()
	var out []sweepRow
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		var r sweepRow
		require.NoError(t, json.Unmarshal([]byte(line), &r), "not a JSON object: %q", line)
		out = append(out, r)
	}
	return out
}

// byPort indexes rows for assertions that do not care about completion
// order.
func byPort(rs []sweepRow) map[string]sweepRow {
	m := make(map[string]sweepRow, len(rs))
	for _, r := range rs {
		m[r.Port] = r
	}
	return m
}

// The row's shape, pinned. Field order and names are what a consumer
// writes its jq against, and the twin is inlined rather than nested so
// `.code` is where every other dockhand document puts it.
func TestSweepRowSchema(t *testing.T) {
	b, err := json.Marshal(sweepRow{
		Port:    "jq",
		Outcome: outcomeMinted,
		Twin:    exitcode.Of(exitcode.OK, ""),
		Detail:  "jq: update to 1.8.1",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"port":"jq","outcome":"minted","code":0,"family":"success","reason":"","detail":"jq: update to 1.8.1"}`,
		string(b))
	assert.Equal(t, []string{"port", "outcome", "code", "family", "reason", "detail"}, jsonKeys(t, b),
		"the field order is part of the contract, and the twin is inline rather than nested")

	// The two fields a clean row has nothing to say in are absent
	// rather than empty, which is how every other decline document in
	// the tree behaves.
	b, err = json.Marshal(sweepRow{Port: "jq", Outcome: outcomeAdvanced, Twin: exitcode.Of(exitcode.OK, "")})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "detail")
	assert.NotContains(t, string(b), "remedy")
}

// jsonKeys is an object's keys in the order they were written, which is
// what a schema pinned by field order needs and what an unordered
// comparison cannot see.
func jsonKeys(t *testing.T, b []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	open, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), open)
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		require.NoError(t, err)
		name, ok := k.(string)
		require.True(t, ok, "an object key is a string")
		keys = append(keys, name)
		var v json.RawMessage
		require.NoError(t, dec.Decode(&v))
	}
	return keys
}

// A sweep that mints: one row per port, a census on stderr, exit 0, and
// a branch for each.
func TestSweepMintsEveryPortAndReportsOneRowEach(t *testing.T) {
	testenv.PortTclsh(t)
	root, repo := threePorts(t)

	tr := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, tr.exit, "a sweep that handled every port exits 0:\n%s", tr.render())

	got := rows(t, tr.stdout)
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, outcomeMinted, r.Outcome, "%s: %+v", r.Port, r)
		assert.Equal(t, exitcode.OK, r.Code)
		assert.Contains(t, r.Detail, "a stated reason", "the detail is the plan's own summary")
	}
	// Rows arrive as ports finish, which on a pool of evaluators is not
	// the order they were dispatched in. What is guaranteed is that
	// every target in produces exactly one row out.
	assert.ElementsMatch(t, []string{"alpha", "beta", "gamma"}, portsOf(got))

	assert.Contains(t, tr.stderr, "3 ports swept\n")
	assert.Contains(t, tr.stderr, "minted           3  (100.0%)\n")

	branches, err := repo.Branches(t.Context(), git.BranchNamespace)
	require.NoError(t, err)
	assert.Len(t, branches, 3)
}

// portsOf is the rows' ports in the order they were written.
func portsOf(rs []sweepRow) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Port)
	}
	return out
}

// Resume is re-running the command. The second sweep meets its own
// branches, reports every port advanced, mints nothing new, and leaves
// every tip exactly where the first one put it.
func TestSweepRerunAdvancesOverWhatItAlreadyMinted(t *testing.T) {
	testenv.PortTclsh(t)
	root, repo := threePorts(t)

	first := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, first.exit, first.render())
	before := branchTips(t, repo)

	again := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, again.exit, "a sweep with nothing left to do is a success:\n%s", again.render())

	for _, r := range rows(t, again.stdout) {
		assert.Equal(t, outcomeAdvanced, r.Outcome, "%s: %+v", r.Port, r)
		assert.Contains(t, r.Detail, "already carries this change")
	}
	assert.Contains(t, again.stderr, "advanced         3  (100.0%)\n")
	assert.Equal(t, before, branchTips(t, repo), "a rerun writes nothing")
}

// The interrupted case the resume rests on: a port already minted —
// here by the single-port road, which is what a killed sweep leaves
// behind — is stepped over, and the ports it never reached are minted.
func TestSweepResumesPastWhatAnInterruptedRunMinted(t *testing.T) {
	testenv.PortTclsh(t)
	root, repo := threePorts(t)

	one := captureExecute(t, "bump-revision", "--tree", root, "--reason", "a stated reason",
		"--no-verify", filepath.Join(root, "devel", "alpha"))
	require.Equal(t, 0, one.exit, one.render())
	require.NotEmpty(t, one.stdout, "the single-port road still speaks")
	require.NotContains(t, one.stdout, `"outcome":`, "and it speaks prose, not rows")
	alphaTip, err := repo.RevParse(t.Context(), "dockhand/alpha-rev3")
	require.NoError(t, err)

	tr := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, tr.exit, tr.render())

	got := byPort(rows(t, tr.stdout))
	require.Len(t, got, 3)
	assert.Equal(t, outcomeAdvanced, got["alpha"].Outcome, "1..N are skipped")
	assert.Equal(t, outcomeMinted, got["beta"].Outcome)
	assert.Equal(t, outcomeMinted, got["gamma"].Outcome)

	now, err := repo.RevParse(t.Context(), "dockhand/alpha-rev3")
	require.NoError(t, err)
	assert.Equal(t, alphaTip, now, "the branch the interrupted run minted is untouched")
}

// The resume-by-rerun gate, end to end and against a real kill.
//
// The simulation above proves a rerun steps over a branch that is
// already standing. What it cannot prove is that an interrupted sweep
// leaves the repository in a state a rerun can step over at all: the
// kill lands between two ports, and whether the run had got as far as
// writing a port's row decides whether that port has a branch.
//
// So this one interrupts the real thing — the context dies under a
// sweep that is partway through, which is what a Ctrl-C is — and then
// reruns the identical command. The second run must cover every port
// exactly once: advanced for the ports the first run minted, minted for
// every port it did not. Resume is re-running the command. There is no
// journal, and nothing is passed between the two runs but the branches
// the first one left in the repository.
func TestSweepResumeAfterARealInterrupt(t *testing.T) {
	testenv.PortTclsh(t)
	// More ports than the run can have in hand when the kill lands, or
	// there is no interruption to test. The loop buffers two results per
	// evaluator and holds one more in flight per evaluator, so with the
	// eight a bump-revision sweep uses, the first twenty-four ports are
	// taken off the queue before the first row is even drained. A
	// smaller category is entirely consumed by the workers, leaves
	// nothing stranded, and finishes reporting success.
	const ports = 40
	pinGitDates(t)
	bodies := make(map[string]string, ports)
	names := make([]string, 0, ports)
	for i := range ports {
		n := fmt.Sprintf("p%02d", i)
		bodies[n] = sweepPortfile(n, "")
		names = append(names, n)
	}
	root := sweepTree(t, bodies)
	repo := gittest.Init(t, testFinder(), root, nil)

	killed := interruptedSweep(t, root, 1, "category:devel")
	assert.NotEqual(t, 0, killed.exit, "a sweep that was stopped did not finish")

	first := rows(t, killed.stdout)
	require.Less(t, len(first), ports, "a kill that reached every port proves nothing")
	done := map[string]bool{}
	for _, r := range first {
		// An interrupt fabricates nothing. The results already buffered
		// when the cancellation lands must be dropped, not realized
		// against a dead context and reported broken: a census calling
		// an interrupted port "failed" is a sweep lying about ports the
		// very next run mints without complaint.
		require.NotEqual(t, outcomeFailed, r.Outcome,
			"%s was interrupted, not broken: %+v", r.Port, r)
		if r.Outcome == outcomeMinted {
			done[r.Port] = true
		}
	}
	require.NotEmpty(t, done, "the kill landed after at least one mint")
	minted := branchTips(t, repo)
	require.Len(t, minted, len(done), "one branch per minted row, and nothing else")
	// The tail a user actually reads says the sweep was stopped, and
	// counts nothing it did not do.
	assert.NotContains(t, killed.stderr, "failed",
		"an interrupted sweep's census invents no failures:\n%s", killed.stderr)

	// The resume: the identical command, and nothing told it what
	// happened.
	again := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, again.exit, again.render())

	got := byPort(rows(t, again.stdout))
	require.Len(t, got, ports, "the rerun covers every port exactly once")
	for _, n := range names {
		if done[n] {
			assert.Equal(t, outcomeAdvanced, got[n].Outcome,
				"%s was minted before the kill and must be stepped over", n)
			continue
		}
		assert.Equal(t, outcomeMinted, got[n].Outcome,
			"%s was never minted and must be minted now", n)
	}

	// What the interrupted run wrote is exactly where it left it, and
	// the tree now holds one branch per port.
	after := branchTips(t, repo)
	for b, sha := range minted {
		assert.Equal(t, sha, after[b], "the rerun rewrote %s", b)
	}
	assert.Len(t, after, ports, "the two runs together covered the category")
}

// interruptedSweep runs a bump-revision sweep and cancels its context
// from inside the row stream, once stopAfter rows have been written.
// Rows are drained on the run's own goroutine, so cancelling from the
// writer lands between two ports rather than racing one.
func interruptedSweep(t *testing.T, root string, stopAfter int, args ...string) transcript {
	t.Helper()
	t.Setenv("DOCKHAND_TREE", "")
	t.Setenv("DOCKHAND_PREFIX", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errb bytes.Buffer
	w := &cancelAfterRows{w: &out, after: stopAfter, cancel: cancel}
	code := execute(ctx, "test", append([]string{"bump-revision", "--tree", root,
		"--reason", "a stated reason", "--no-verify"}, args...), w, &errb)
	return transcript{exit: code, stdout: out.String(), stderr: errb.String()}
}

// cancelAfterRows passes bytes through and cancels once it has seen the
// given number of NDJSON rows go by.
type cancelAfterRows struct {
	w      io.Writer
	after  int
	seen   int
	cancel context.CancelFunc
}

func (c *cancelAfterRows) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.seen += bytes.Count(p[:n], []byte("\n"))
	if c.cancel != nil && c.seen >= c.after {
		c.cancel()
		c.cancel = nil
	}
	return n, err
}

// branchTips is every dockhand branch and where it points.
func branchTips(t *testing.T, repo *git.Repo) map[string]string {
	t.Helper()
	branches, err := repo.Branches(t.Context(), git.BranchNamespace)
	require.NoError(t, err)
	tips := make(map[string]string, len(branches))
	for _, b := range branches {
		sha, err := repo.RevParse(t.Context(), b)
		require.NoError(t, err)
		tips[b] = sha
	}
	return tips
}

// --plan over a selector plans every port and writes no plan documents:
// the rows are the report, and a stream carrying both would be two
// languages on one pipe.
func TestSweepPlanOnlyWritesRowsAndMintsNothing(t *testing.T) {
	testenv.PortTclsh(t)
	root, repo := threePorts(t)

	tr := revbumpSweep(t, root, "--plan", "category:devel")
	require.Equal(t, 0, tr.exit, tr.render())

	got := rows(t, tr.stdout)
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, outcomePlanned, r.Outcome)
	}
	branches, err := repo.Branches(t.Context(), git.BranchNamespace)
	require.NoError(t, err)
	assert.Empty(t, branches, "--plan changes nothing")
}

// Census exit semantics. A tree that is no git checkout cannot take a
// branch, so every port ends in the tree band — which is a hard error,
// not a decline — and the sweep exits 83 having said so per port.
func TestSweepExitsPartialOnHardErrors(t *testing.T) {
	testenv.PortTclsh(t)
	root := sweepTree(t, map[string]string{
		"alpha": sweepPortfile("alpha", ""),
		"beta":  sweepPortfile("beta", ""),
	})

	tr := revbumpSweep(t, root, "category:devel")
	assert.Equal(t, exitcode.SweepHardErrors, tr.exit, tr.render())

	got := rows(t, tr.stdout)
	require.Len(t, got, 2)
	for _, r := range got {
		assert.Equal(t, outcomeFailed, r.Outcome, "%s: %+v", r.Port, r)
		assert.Equal(t, "tree", r.Family)
	}
	assert.Contains(t, tr.stderr, "failed           2  (100.0%)\n")
	// The prose the engine wrote about a port that broke is replayed
	// under that port's name rather than dropped.
	assert.Contains(t, tr.stderr, "dockhand: 2 of 2 ports ended with an error that was not a decline")
}

// A decline is not a failure. A port already at the revision the sweep
// would set it to is the commonest thing a real sweep meets, and it
// exits 0.
func TestSweepExitsZeroWithDeclines(t *testing.T) {
	testenv.PortTclsh(t)
	root, _ := threePorts(t)
	// A port whose revision comes from nowhere a line can be written
	// under: no version carrier, so the revision has no anchor.
	dir := filepath.Join(root, "devel", "delta")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(
		"PortSystem          1.0\n\nname                delta\n"+
			"categories          devel\nmaintainers         nomaintainer\n"+
			"license             MIT\ndescription         d\nlong_description    d\n"), 0o644))

	tr := revbumpSweep(t, root, "--plan", "category:devel")
	require.Equal(t, 0, tr.exit, "declines do not make a sweep fail:\n%s", tr.render())

	got := byPort(rows(t, tr.stdout))
	require.Contains(t, got, "delta")
	assert.Equal(t, outcomeDeclined, got["delta"].Outcome)
	assert.Equal(t, "declined", got["delta"].Family)
	assert.NotEmpty(t, got["delta"].Remedy, "a decline names what to do about it")
	assert.Contains(t, tr.stderr, "declined         1  (25.0%)\n")
}

// A port its maintainer pinned is kept out before any work is done, and
// the row carries the Portfile's own words so the person reading it can
// weigh what the sweep could not.
func TestSweepExcludesAPinnedPortWithItsQuote(t *testing.T) {
	testenv.PortTclsh(t)
	pinGitDates(t)
	root := sweepTree(t, map[string]string{
		"alpha":  sweepPortfile("alpha", ""),
		"pinned": sweepPortfile("pinned", "# Note: do not update past 1.0, 2.0 needs a newer toolchain\n"),
	})
	gittest.Init(t, testFinder(), root, nil)

	tr := revbumpSweep(t, root, "--plan", "category:devel")
	require.Equal(t, 0, tr.exit, tr.render())

	got := byPort(rows(t, tr.stdout))
	require.Contains(t, got, "pinned")
	assert.Equal(t, outcomeExcluded, got["pinned"].Outcome)
	assert.Equal(t, "declined", got["pinned"].Family)
	assert.Contains(t, got["pinned"].Detail, "do not update past 1.0")
	assert.Contains(t, got["pinned"].Remedy, "name the port on its own")
	assert.Equal(t, outcomePlanned, got["alpha"].Outcome, "the rest of the sweep is unaffected")
}

// Every flag whose content is singular, refused by name once the arity
// is known — which is only after the selector has been resolved.
func TestSweepRefusesTheSingularFlags(t *testing.T) {
	testenv.PortTclsh(t)
	root, _ := threePorts(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"replace", []string{"--replace"}, "--replace replaces one port's in-flight branch"},
		{"riders", []string{"--riders"}, "needs a batching strategy nobody has ruled on"},
		{"diff", []string{"--diff"}, "--diff is an output mode of its own"},
		{"in-place", []string{"--in-place"}, "--in-place would leave 3 uncommitted edits"},
		{"closes", []string{"--closes", "12345"}, "--closes writes one ticket's trailer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := revbumpSweep(t, root, append(tc.args, "category:devel")...)
			assert.Equal(t, exitcode.Usage, tr.exit, tr.render())
			assert.Contains(t, tr.stderr, tc.want)
			assert.Empty(t, tr.stdout, "a refusal writes no rows")
		})
	}
}

// --verify and --trace cannot ride with --no-verify, so they are asked
// on their own: both are singular for their own reasons, and both are
// refused before any provider is consulted.
func TestSweepRefusesTheVerificationFlags(t *testing.T) {
	testenv.PortTclsh(t)
	root, _ := threePorts(t)

	for _, tc := range []struct{ flag, want string }{
		{"--verify", "--verify gates one mint on one build"},
		{"--trace", "--trace follows one submitted verification"},
	} {
		t.Run(strings.TrimPrefix(tc.flag, "--"), func(t *testing.T) {
			tr := captureExecute(t, "bump-revision", "--tree", root,
				"--reason", "a stated reason", tc.flag, "category:devel")
			assert.Equal(t, exitcode.Usage, tr.exit, tr.render())
			assert.Contains(t, tr.stderr, tc.want)
		})
	}
}

// bump's own singular flag: one version cannot be many ports' version.
func TestSweepRefusesToAtScale(t *testing.T) {
	testenv.PortTclsh(t)
	root, _ := threePorts(t)

	tr := captureExecute(t, "bump", "--tree", root, "--to", "2.0", "--plan", "category:devel")
	assert.Equal(t, exitcode.Usage, tr.exit, tr.render())
	assert.Contains(t, tr.stderr, "--to names one version")
}

// A bare token that is both a category and a port is refused on the
// write verbs, where guessing wrong is a hundred branches. The single
// port that a bare token names keeps meaning what it always meant,
// which is the case below it.
func TestSweepRefusesAnAmbiguousBareToken(t *testing.T) {
	testenv.PortTclsh(t)
	pinGitDates(t)
	root := sweepTree(t, map[string]string{
		"alpha": sweepPortfile("alpha", ""),
		"beta":  sweepPortfile("beta", ""),
	})
	// A category named devel holding a port named devel, which is the
	// shape of the thirteen real collisions.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel", "devel"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel", "devel", macports.PortfileName),
		[]byte(sweepPortfile("devel", "")), 0o644))
	gittest.Init(t, testFinder(), root, nil)

	tr := revbumpSweep(t, root, "--plan", "devel")
	assert.Equal(t, exitcode.Usage, tr.exit, tr.render())
	assert.Contains(t, tr.stderr, "names both the category (3 ports) and the port at")
	assert.Contains(t, tr.stderr, "say `category:devel`")
}

// A selector that names exactly one port is the single-port road, byte
// for byte: prose on stderr, no rows, no census.
func TestSelectorNamingOnePortIsTheSinglePortRoad(t *testing.T) {
	testenv.PortTclsh(t)
	pinGitDates(t)
	root := sweepTree(t, map[string]string{"alpha": sweepPortfile("alpha", "")})
	gittest.Init(t, testFinder(), root, nil)

	byCategory := revbumpSweep(t, root, "--plan", "category:devel")
	byName := revbumpSweep(t, root, "--plan", "alpha")
	byPath := revbumpSweep(t, root, "--plan", filepath.Join(root, "devel", "alpha"))

	require.Equal(t, 0, byCategory.exit, byCategory.render())
	assert.Equal(t, byPath.stdout, byCategory.stdout, "one port is one plan document, whichever selector found it")
	assert.Equal(t, byPath.stderr, byCategory.stderr)
	assert.Equal(t, byPath.stdout, byName.stdout)
	assert.Equal(t, byPath.stderr, byName.stderr)
	assert.Contains(t, byPath.stdout, `"intent": "bump-revision"`, "the plan document, not a row")
	assert.NotContains(t, byPath.stderr, "total")
}

// The band table, ruled on once and asserted here so a code added to a
// band later cannot quietly change what a sweep's exit means.
func TestHardBand(t *testing.T) {
	for code, hard := range map[int]bool{
		exitcode.OK:                  false,
		exitcode.PlanDeclined:        false,
		exitcode.BranchInFlight:      false,
		exitcode.AlreadyCurrent:      false,
		exitcode.DuplicatePR:         false,
		exitcode.FetchFailed:         false,
		exitcode.LatestUnresolved:    false,
		exitcode.VerifyQueued:        false,
		exitcode.VerifyAwaitingSlot:  false,
		exitcode.VerifyFailed:        false,
		exitcode.Failure:             true,
		exitcode.Usage:               true,
		exitcode.NoMacPorts:          true,
		exitcode.EvalStartup:         true,
		exitcode.NotPortsTree:        true,
		exitcode.Drift:               true,
		exitcode.MintedSubmitErrored: true,
		exitcode.SweepHardErrors:     true,
		99:                           true,
	} {
		assert.Equal(t, hard, hardBand(code), "exit %d (%s)", code, exitcode.Family(code))
	}
}

// The census counts what it is given and reports only what happened: a
// tail of nine zeroes buries the two numbers that matter.
func TestCensusReportsOnlyWhatHappened(t *testing.T) {
	var c census
	c.add(sweepRow{Port: "a", Outcome: outcomeMinted, Twin: exitcode.Of(exitcode.OK, "")})
	c.add(sweepRow{Port: "b", Outcome: outcomeDeclined, Twin: exitcode.Of(exitcode.PlanDeclined, "")})
	c.add(sweepRow{Port: "c", Outcome: outcomeDeclined, Twin: exitcode.Of(exitcode.AlreadyCurrent, "")})
	c.add(sweepRow{Port: "d", Outcome: outcomeFailed, Twin: exitcode.Of(exitcode.Drift, "")})

	assert.Equal(t, "4 ports swept\n"+
		"  minted           1  (25.0%)\n"+
		"  declined         2  (50.0%)\n"+
		"  failed           1  (25.0%)\n", c.String())
	assert.Equal(t, 1, c.hard)
	assert.Equal(t, "d", c.first, "the message names somewhere to start")
	assert.Equal(t, exitcode.SweepHardErrors,
		ExitCode(&SweepFailedError{Hard: c.hard, Total: c.total, First: c.first}))
}

// Mint freely; submit to capacity; queue the rest. A machine with no
// slot free is the normal ending for most of a large sweep: every
// branch is minted, every verification is recorded queued for the
// deferred pump, and the sweep is a success because nothing failed.
func TestSweepMintsFreelyAndQueuesWhatCapacityRefuses(t *testing.T) {
	testenv.PortTclsh(t)
	_, repo := threePorts(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}})

	tr := capture(t, rs, out, errb, intentAction{
		def:    bumpRevisionVerb.Definition,
		params: intent.Params{Target: "category:devel", Reason: "a stated reason"},
	})
	assert.Equal(t, 0, tr.exit, "a queued verification is nobody's problem yet:\n%s", tr.render())

	got := rows(t, tr.stdout)
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, outcomeMinted, r.Outcome, "%s: %+v", r.Port, r)
		assert.Equal(t, "pending", r.Family, "the branch stands and the run is queued")
		assert.Equal(t, exitcode.VerifyQueued, r.Code)
	}
	branches, err := repo.Branches(t.Context(), git.BranchNamespace)
	require.NoError(t, err)
	assert.Len(t, branches, 3, "capacity gates the verification, never the mint")
}

// A newer change to a port whose older change is still in flight: the
// new branch is minted beside the old one, the row names what it set
// aside, and the old branch is still there. Ruling 4's other half —
// a superseded branch is an end state, not a discard.
func TestSweepSupersedesTheOlderBranchForAPort(t *testing.T) {
	testenv.PortTclsh(t)
	root, repo := threePorts(t)

	first := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, first.exit, first.render())

	// The tree moves on, on disk and on the primary branch both: alpha
	// is now at the revision its branch was going to give it, so the
	// next sweep has a newer change to make. Committed as well as
	// written, because a plan is held to the Portfile it was made
	// against and an uncommitted edit is drift.
	alpha := filepath.Join(root, "devel", "alpha", macports.PortfileName)
	src, err := os.ReadFile(alpha)
	require.NoError(t, err)
	moved := strings.Replace(string(src), "revision            2", "revision            3", 1)
	require.NoError(t, os.WriteFile(alpha, []byte(moved), 0o644))
	primary, err := repo.PrimaryBranch(t.Context())
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "scratch", primary, "devel/alpha/Portfile", moved, "alpha: revision 3")
	gittest.MoveBranch(t, repo, primary, sha)
	require.NoError(t, repo.DeleteBranch(t.Context(), "scratch"))

	tr := revbumpSweep(t, root, "category:devel")
	require.Equal(t, 0, tr.exit, tr.render())

	got := byPort(rows(t, tr.stdout))
	assert.Equal(t, outcomeSuperseded, got["alpha"].Outcome)
	assert.Contains(t, got["alpha"].Detail, "superseding dockhand/alpha-rev3")
	assert.Equal(t, outcomeAdvanced, got["beta"].Outcome, "the ports that did not move are stepped over")

	assert.True(t, repo.HasBranch(t.Context(), "dockhand/alpha-rev3"),
		"the superseded branch is an end state of its own; nothing discards it")
	assert.True(t, repo.HasBranch(t.Context(), "dockhand/alpha-rev4"))
}

// The fetching road at selector scale. refresh-checksums opens one
// fetch session over one Tcl interpreter, which cannot be shared, so
// its sweep runs on a single evaluator — and that is also what keeps it
// polite, since eight concurrent distfile pulls at one host is the
// abuse the ruling is about. What is proven here is that the road
// works: two ports, two fetches, two rows, one at a time.
func TestRefreshSweepFetchesOnePortAtATime(t *testing.T) {
	testenv.PortTclsh(t)
	pinGitDates(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	// Two copies of the bump fixture, each renamed, each with its own
	// distfile beside it under the name its distname derives. The
	// tarball bytes are the same file, so the recorded checksums are
	// already true and each port declines — which is the answer, and
	// reaching it needs the whole fetch.
	src := filepath.Join("testdata", "golden", "ports", "devel", "bumpee")
	for _, name := range []string{"alef", "bet"} {
		dir := filepath.Join(root, "devel", name)
		require.NoError(t, os.CopyFS(dir, os.DirFS(src)))
		body, err := os.ReadFile(filepath.Join(dir, macports.PortfileName))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
			[]byte(strings.Replace(string(body), "name                bumpee", "name                "+name, 1)), 0o644))
		for _, v := range []string{"1.0", "2.0"} {
			tar, err := os.ReadFile(filepath.Join(dir, "files", "upstream", "bumpee-"+v+".tar.gz"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "files", "upstream", name+"-"+v+".tar.gz"), tar, 0o644))
		}
	}
	gittest.Init(t, testFinder(), root, nil)

	tr := captureExecute(t, "refresh-checksums", "--tree", root, "--plan", "category:devel")
	require.Equal(t, 0, tr.exit, "every port declined, which is a success:\n%s", tr.render())

	got := byPort(rows(t, tr.stdout))
	require.Len(t, got, 2)
	for name, r := range got {
		assert.Equal(t, outcomeDeclined, r.Outcome, "%s: %+v", name, r)
		assert.Equal(t, "declined", r.Family)
		assert.Contains(t, r.Detail, "match what upstream serves")
	}
	// Said once for the sweep, not once per port.
	assert.Equal(t, 1, strings.Count(tr.stderr, "supply-chain"),
		"the caution belongs to the change, and a sweep makes one change of a kind")
}
