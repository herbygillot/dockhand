package cmd

// Golden transcripts: every lifecycle verb's stdout, stderr and exit
// code, pinned byte for byte against testdata/golden/<name>.golden.
// A unit test proves a line is present; a golden proves the whole
// surface is what it was — the padding, the ordering, the phrasing a
// user's eye and a script's grep both depend on — and shows exactly
// what moved when it moves.
//
//	go test ./internal/cmd/ -run Golden            # compare
//	go test ./internal/cmd/ -run Golden -update    # re-record
//
// The fixtures and the harness are in golden_fixtures_test.go; what is
// normalized, and why nothing else is, is documented there on
// normalize.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

var update = flag.Bool("update", false, "rewrite golden files")

// checkGolden compares a rendered transcript with its golden, or
// records it under -update. A mismatch fails with a unified diff and
// the command that re-records.
func checkGolden(t *testing.T, name string, tr transcript, rw ...rewrite) {
	t.Helper()
	got := normalize(tr.render(), rw)
	path := filepath.Join("testdata", "golden", name+".golden")
	remedy := fmt.Sprintf("go test ./internal/cmd/ -run '^%s$' -update", t.Name())
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "%s: no golden recorded; record it with:\n  %s", name, remedy)
	if string(want) == got {
		return
	}
	t.Errorf("%s: the transcript changed\n%s\nif the change is intended, re-record it with:\n  %s",
		path, unifiedDiff(string(want), got), remedy)
}

// unifiedDiff renders a line diff between two texts in unified style:
// changed lines marked - and +, three lines of context, hunks
// separated by their starting line numbers. go-cmp is not vendored,
// and a golden mismatch has to show where, not merely that.
func unifiedDiff(want, got string) string {
	a, b := splitLines(want), splitLines(got)
	n, m := len(a), len(b)
	// Longest common subsequence, suffix-indexed: lcs[i][j] is the LCS
	// length of a[i:] and b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		kind byte
		line string
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}

	const contextLines = 3
	keep := make([]bool, len(ops))
	for k, o := range ops {
		if o.kind == ' ' {
			continue
		}
		for c := max(0, k-contextLines); c <= min(len(ops)-1, k+contextLines); c++ {
			keep[c] = true
		}
	}
	var out strings.Builder
	out.WriteString("--- want (golden)\n+++ got\n")
	ai, bi := 1, 1
	inHunk := false
	for k, o := range ops {
		if keep[k] {
			if !inHunk {
				fmt.Fprintf(&out, "@@ -%d +%d @@\n", ai, bi)
				inHunk = true
			}
			out.WriteByte(o.kind)
			out.WriteString(o.line)
			if !strings.HasSuffix(o.line, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		} else {
			inHunk = false
		}
		if o.kind != '+' {
			ai++
		}
		if o.kind != '-' {
			bi++
		}
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// A golden no test pins is a claim nobody checks: a case renamed or
// dropped leaves its file behind, still reading as a pinned surface.
// The cases here are one function each rather than a table, so the
// sweep reads this file for the names checkGolden is called with —
// the same list a reader would grep for — and fails on any golden
// outside it. The bump fixture under testdata/golden/ports is a
// directory, not a golden, and is skipped as such.
func TestGoldenNoStrays(t *testing.T) {
	src, err := os.ReadFile("golden_test.go")
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Join("testdata", "golden"))
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, isGolden := strings.CutSuffix(e.Name(), ".golden")
		require.True(t, isGolden, "testdata/golden/%s is not a golden; goldens end in .golden", e.Name())
		require.Contains(t, string(src), fmt.Sprintf("checkGolden(t, %q", name),
			"stale golden testdata/golden/%s: no test pins it — delete it, or restore the case that did", e.Name())
	}
}

// capture (golden_fixtures_test.go) renders a failing action's stderr
// line the way main does — the "dockhand:" prefix cobra prints, then
// the message — so the twenty goldens that go through it pin main's
// rendering only while this holds. The bump goldens go through
// execute itself and pin the real path.
func TestGoldenCaptureRendersMainsPrefix(t *testing.T) {
	root, rc := newRoot("test")
	t.Cleanup(rc.Close)
	require.Equal(t, "dockhand:", root.ErrPrefix(),
		"capture writes this prefix by hand; change both, or route the goldens through execute")
	require.True(t, root.SilenceUsage, "capture prints no usage after an error, as main does not")
}

// ---- status ----------------------------------------------------------

func TestGoldenStatusHuman(t *testing.T) {
	tartAbsent(t)
	repo, fake := goldenStatesRepo(t)
	rs, out, errb := goldenState(repo, fake)
	tr := capture(t, rs, out, errb, statusAction{})
	checkGolden(t, "status_human", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenStatusJSON(t *testing.T) {
	tartAbsent(t)
	repo, fake := goldenStatesRepo(t)
	rs, out, errb := goldenState(repo, fake)
	tr := capture(t, rs, out, errb, statusAction{json: true})
	checkGolden(t, "status_json", tr, rewrite{repo.Root, "<repo>"})
}

// The deferred pump: status starts what was deferred once a slot is
// free. tart is stubbed present so the gate opens; the pre-flight
// evaluates through a prefix that holds nothing, so its degradation
// line is the same on every machine.
func TestGoldenStatusPump(t *testing.T) {
	tartOnPath(t)
	repo, sha := goldenLifecycleRepo(t)
	deferredNote(t, repo, sha, (&verify.CapacityError{Busy: 2, Cap: 2}).Error())
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.PrefixPath = goldenNoPrefix
	tr := capture(t, rs, out, errb, statusAction{})
	checkGolden(t, "status_pump", tr, rewrite{repo.Root, "<repo>"})
}

// The worker audit's two sentences, which no golden pinned while the
// audit ran tart directly and every status fixture stubbed tart away.
// It is asked of the provider now, so the fake answers it and the
// machine's own tart is stubbed absent — which is the whole of what
// changed: a wired provider reports its workers on a machine the audit
// used to skip for want of tart on PATH.
//
// One worker per case the filters distinguish: the running job's, which
// the notes account for and which must not appear; another checkout's,
// named with its owner; this checkout's own, named without one; and an
// unattributed worker, which is a worker rather than an error.
func TestGoldenStatusOrphans(t *testing.T) {
	tartAbsent(t)
	repo, sha := goldenLifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("dockhand-worker-mine")})
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "dockhand-worker-mine"},
		{Name: "dockhand-worker-elsewhere", Owner: "/elsewhere/ports"},
		{Name: "dockhand-worker-ours", Owner: repo.Root},
		{Name: "dockhand-worker-nameless"},
	}}
	rs, out, errb := goldenState(repo, fake)
	tr := capture(t, rs, out, errb, statusAction{})
	checkGolden(t, "status_orphans", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenStatusPR(t *testing.T) {
	tartAbsent(t)
	repo, gh := goldenPromotedRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, statusAction{})
	checkGolden(t, "status_pr", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenStatusPRNoClean(t *testing.T) {
	tartAbsent(t)
	repo, gh := goldenPromotedRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, statusAction{noClean: true})
	checkGolden(t, "status_pr_no_clean", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenStatusPRJSON(t *testing.T) {
	tartAbsent(t)
	repo, gh := goldenPromotedRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, statusAction{json: true})
	checkGolden(t, "status_pr_json", tr, rewrite{repo.Root, "<repo>"})
}

// A namespace with nothing in it: the sentence names the checkout,
// because run from the wrong one "no branches" is true and useless.
// tart reads as present so the two phases an empty pass could still
// reach — the drain and the worker audit — are reached and say nothing;
// with it absent the case would prove only that the gate is shut.
func TestGoldenStatusEmpty(t *testing.T) {
	tartOnPath(t)
	repo := goldenRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, statusAction{})
	checkGolden(t, "status_empty", tr, rewrite{repo.Root, "<repo>"})
}

// The same emptiness as a document: `branches` is an empty array and
// not null, which is what a consumer iterating it depends on.
func TestGoldenStatusEmptyJSON(t *testing.T) {
	tartOnPath(t)
	repo := goldenRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, statusAction{json: true})
	checkGolden(t, "status_empty_json", tr, rewrite{repo.Root, "<repo>"})
}

// ---- clean -----------------------------------------------------------

// The sweep's own empty pass: the same sentence status prints, and no
// worker audit under it — clean asks one question of each branch and
// has none to ask.
func TestGoldenCleanEmpty(t *testing.T) {
	tartOnPath(t)
	repo := goldenRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, cleanAction{})
	checkGolden(t, "clean_empty", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenClean(t *testing.T) {
	repo, gh := goldenPromotedRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, cleanAction{})
	checkGolden(t, "clean", tr, rewrite{repo.Root, "<repo>"})
}

// ---- cancel ----------------------------------------------------------

// The tip's running job is canceled; a running job on the sha the
// branch was amended away from is released as stale on the way.
func TestGoldenCancel(t *testing.T) {
	repo, old := goldenLifecycleRepo(t)
	writeRuns(t, repo, old, map[string]platRun{"Testos": runningOn("fake-1")})
	tip := growBranch(t, repo, "dockhand/jq-1.8", "version 1.8\nrevision 1\n", "jq: rebuild")
	writeRuns(t, repo, tip, map[string]platRun{"Testos": runningOn("fake-2")})
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, cancelAction{target: "jq"})
	checkGolden(t, "cancel", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenCancelKept(t *testing.T) {
	repo, sha := goldenLifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": keptOn("fake-1", "Failed to build jq: boom")})
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, cancelAction{target: "jq"})
	checkGolden(t, "cancel_kept", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenCancelNothing(t *testing.T) {
	repo, sha := goldenLifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": passedOn("fake-1")})
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, cancelAction{target: "jq"})
	checkGolden(t, "cancel_nothing", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenCancelUnknownTarget(t *testing.T) {
	repo, _ := goldenLifecycleRepo(t)
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, cancelAction{target: "nope"})
	checkGolden(t, "cancel_unknown_target", tr, rewrite{repo.Root, "<repo>"})
}

// ---- discard ---------------------------------------------------------

// A promoted branch holding a running worker and a kept failure: both
// released, the fork copy deliberately left, the branch gone.
func TestGoldenDiscard(t *testing.T) {
	repo, sha := goldenPromoteRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{
		"Testos": runningOn("fake-1"),
		"Oldos":  keptOn("fake-9", "Failed to build jq: boom"),
	})
	require.NoError(t, repo.Push(t.Context(), "herby", "dockhand/jq-1.8"))
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	tr := capture(t, rs, out, errb, discardAction{target: "jq"})
	checkGolden(t, "discard", tr, rewrite{repo.Root, "<repo>"})
}

// With no provider wired, the branch still goes; the worker it held
// is named as the thing nobody released.
func TestGoldenDiscardUnwiredProvider(t *testing.T) {
	repo, sha := goldenLifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("fake-1")})
	rs, out, errb := goldenState(repo, nil)
	tr := capture(t, rs, out, errb, discardAction{target: "jq"})
	checkGolden(t, "discard_unwired_provider", tr, rewrite{repo.Root, "<repo>"})
}

// ---- promote ---------------------------------------------------------

// Each promote golden pins, with the streams, the pr create or pr edit
// call the verb made — the PR template is part of the surface.

func TestGoldenPromote(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot", createURL: "https://github.com/macports/macports-ports/pull/999"}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteRepromote(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":77,"state":"open","html_url":"https://x/77","title":"jq: update to 1.8"}]`}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_repromote", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteForce(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":77,"state":"open","html_url":"https://x/77","title":"jq: update to 1.7"}]`}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq", force: true})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_force", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteDuplicate(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		searchHit: `{"items":[{"number":123,"title":"jq: update to 1.8","state":"open","html_url":"https://x/123"}]}`}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_duplicate", tr, rewrite{repo.Root, "<repo>"})
}

// Unverified, with every complaint the verb can raise on the way: a
// running job canceled, a blocked platform named, another open PR on
// the same port noted — and the PR body saying so.
func TestGoldenPromoteUnverified(t *testing.T) {
	repo, sha := goldenPromoteRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{
		"Testos": {Job: spentGuest("fake-8"), Run: record.Run{State: record.Blocked,
			Detail: "dependency olm (nomaintainer) fails to build; the change itself is untested"}},
		"Oldos": runningOn("fake-9"),
	})
	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/1",
		searchHit: `{"items":[{"number":124,"title":"jq: fix the build on Oldos","state":"open","html_url":"https://x/124"}]}`}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_unverified", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteMerged(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":50,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/50"}]`}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_merged", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteFailed(t *testing.T) {
	repo, sha := goldenPromoteRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": keptOn("kept-1", "Failed to build jq: boom")})
	gh := &ghFake{login: "herbygillot"}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq"})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_failed", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenPromoteNoPR(t *testing.T) {
	repo, _ := goldenPromoteRepo(t)
	gh := &ghFake{login: "herbygillot"}
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.Gh = gh.run
	tr := capture(t, rs, out, errb, promoteAction{target: "jq", noPR: true})
	tr.sections = ghSections(gh)
	checkGolden(t, "promote_no_pr", tr, rewrite{repo.Root, "<repo>"})
}

// ---- verify <branch> -------------------------------------------------

// A branch verification needs tart stubbed present, so submission is
// attempted, and the pre-flight's prefix stated as one holding no
// installation, so its warning is the same everywhere.

func TestGoldenVerifyBranchDeferred(t *testing.T) {
	tartOnPath(t)
	repo, sha := goldenLifecycleRepo(t)
	deferredNote(t, repo, sha, (&verify.CapacityError{Busy: 2, Cap: 2}).Error())
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.PrefixPath = goldenNoPrefix
	tr := capture(t, rs, out, errb, verifyAction{target: "jq"})
	checkGolden(t, "verify_branch_deferred", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenVerifyBranchDeferredAtCapacity(t *testing.T) {
	tartOnPath(t)
	repo, sha := goldenLifecycleRepo(t)
	deferredNote(t, repo, sha, (&verify.CapacityError{Busy: 2, Cap: 2}).Error())
	rs, out, errb := goldenState(repo, &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}})
	rs.PrefixPath = goldenNoPrefix
	tr := capture(t, rs, out, errb, verifyAction{target: "jq"})
	checkGolden(t, "verify_branch_deferred_at_capacity", tr, rewrite{repo.Root, "<repo>"})
}

func TestGoldenVerifyBranchRunning(t *testing.T) {
	tartOnPath(t)
	repo, sha := goldenLifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("fake-1")})
	rs, out, errb := goldenState(repo, &verifytest.Fake{})
	rs.PrefixPath = goldenNoPrefix
	tr := capture(t, rs, out, errb, verifyAction{target: "jq"})
	checkGolden(t, "verify_branch_running", tr, rewrite{repo.Root, "<repo>"})
}

// ---- bump ------------------------------------------------------------

// The bump goldens run the whole command tree against the fixture
// port under testdata/golden/ports: the planner evaluates through the
// real MacPorts evaluator (so they skip without one, or fail when
// DOCKHAND_TEST_REQUIRE demands it) and fetches the new release over
// file:// from beside the port, so no network is involved.

func TestGoldenBumpPlan(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortdir(t)
	tr := captureExecute(t, "bump", "--to", "2.0", "--plan", portdir)
	checkGolden(t, "bump_plan", tr, rewrite{portdir, "<portdir>"})
}

func TestGoldenBumpDiff(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortRepo(t)
	tr := captureExecute(t, "bump", "--to", "2.0", "--diff", portdir)
	checkGolden(t, "bump_diff", tr, rewrite{portdir, "<portdir>"})
}

func TestGoldenBumpInPlace(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortdir(t)
	tr := captureExecute(t, "bump", "--to", "2.0", "--in-place", portdir)
	tr.sections = []section{fileSection(t, "Portfile (after)", filepath.Join(portdir, "Portfile"))}
	checkGolden(t, "bump_in_place", tr, rewrite{portdir, "<portdir>"})
}

// --replace over a standing branch, the one path where a demolition is
// reported inside another verb's output. Four lines whose order is the
// whole claim: the announcement, the fork copy's fate and the checkout
// reassurance on stderr, and — between them, on stdout — the sentence
// saying the branch went. That last one is a returned fact rather than
// something discard prints, so nothing else in the suite notices when
// it stops being printed here.
//
// The flag was --force until S10, when it split: this is the half that
// acts on the branch, and bump's --recheck is the half that re-derives
// the port. The transcript is the same demolition either way, which is
// why the golden moves only in the word the announcement prints.
//
// --no-verify because the transcript must not depend on whether the
// machine running it has tart: the replacement is the subject, and
// submitting a verification is a different verb's story.
func TestGoldenBumpReplace(t *testing.T) {
	testenv.PortTclsh(t)
	portdir, repo := goldenPortForkRepo(t)
	first := captureExecute(t, "bump", "--to", "2.0", "--no-verify", portdir)
	require.Equal(t, 0, first.exit, "the mint --replace replaces must succeed first:\n%s", first.render())
	branches, err := repo.Branches(t.Context(), git.BranchNamespace)
	require.NoError(t, err)
	require.Len(t, branches, 1, "one minted branch stands, to be replaced")
	// Pushed so the branch has a tracking remote: the fork copy's line
	// is part of what --replace says, and it is said only for a branch
	// that has one.
	require.NoError(t, repo.Push(t.Context(), "herby", branches[0]))

	tr := captureExecute(t, "bump", "--to", "2.0", "--no-verify", "--replace", portdir)
	checkGolden(t, "bump_replace", tr, rewrite{portdir, "<portdir>"})
}

// goldenPortForkRepo is goldenPortRepo with the two remotes a promoted
// branch has, and the repository handed back so a case can push to
// them. The bump fixture's own helpers keep only the portdir, which is
// all the other bump goldens need.
func goldenPortForkRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	pinGitDates(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	portdir := copyBumpee(t, root)
	repo := gittest.Init(t, testFinder(), root, nil)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	return portdir, repo
}
