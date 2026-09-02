package cmd

// Fixtures and harness for the golden transcripts (golden_test.go).
//
// A transcript is what the user saw: the exit code and both streams,
// plus whatever product a verb leaves behind that the streams do not
// show (a PR body handed to gh, a Portfile rewritten in place). The
// goldens pin them byte for byte, so the fixtures here are built to be
// the same on every machine: commit dates are pinned before any commit
// is made (a sha is a function of its dates), job start times are a
// constant, the MacPorts prefix a verb would evaluate with is stated
// as one that holds no installation (so the pre-flight's degradation
// line reads the same with or without MacPorts), and tart is stubbed
// present or absent by name rather than found on PATH. What remains
// genuinely nondeterministic — temporary paths, and the age of a
// running job — is normalized by the replacers documented on
// normalize, and nothing else.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// goldenDate is the author and committer date every fixture commit
// carries. With names, emails, trees and messages already fixed, the
// date is the last input to a commit sha; pinning it makes the shas
// in a transcript the same everywhere, so they are pinned raw rather
// than normalized away.
const goldenDate = "2026-09-01T00:00:00Z"

// goldenStart is the start time seeded into every running job. status
// renders a running job's age from it, which normalize then replaces;
// the JSON rendering carries it verbatim.
var goldenStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// goldenNoPrefix is a MacPorts prefix that holds no installation. The
// verify pre-flight evaluates the staged Portfile through rs.Prefix,
// which on a MacPorts machine would spawn the real evaluator and on a
// Go-only one fail to find a prefix — two different lines. Stating a
// prefix that resolves to nothing makes the pre-flight's own warning
// the one line every machine prints.
const goldenNoPrefix = "/nonexistent/dockhand-golden-prefix"

// pinGitDates fixes the commit dates for the rest of the test. It must
// run before the first commit a fixture makes; the git package's
// scrubbed environment passes these two variables through.
func pinGitDates(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", goldenDate)
	t.Setenv("GIT_COMMITTER_DATE", goldenDate)
}

// goldenLifecycleRepo is lifecycleRepo with its dates pinned: the one
// minted branch, dockhand/jq-1.8, at the same sha on every machine.
func goldenLifecycleRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	pinGitDates(t)
	return lifecycleRepo(t)
}

// goldenPromoteRepo is promoteRepo with its dates pinned.
func goldenPromoteRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	pinGitDates(t)
	return promoteRepo(t)
}

// goldenRepo is a ports-tree-shaped repository with no dockhand branch
// yet: sysutils/jq, which every minted branch changes, and devel/olm,
// the nomaintainer dependency a blocked verdict names. gittest pins the
// default branch, so the fixture reads the same under any git config.
func goldenRepo(t *testing.T) *git.Repo {
	t.Helper()
	pinGitDates(t)
	return gittest.Init(t, testFinder(), "", map[string]string{
		"sysutils/jq/Portfile": "version 1.7\n",
		"devel/olm/Portfile":   "version 3.2.16\nmaintainers nomaintainer\n",
	})
}

// mintBranch mints dockhand/jq-<suffix> off the primary branch, moving
// sysutils/jq to the given version, and returns its tip.
func mintBranch(t *testing.T, repo *git.Repo, suffix, version string) string {
	t.Helper()
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	return gittest.Commit(t, repo, "dockhand/jq-"+suffix, primary, "sysutils/jq/Portfile",
		"version "+version+"\n", "jq: update to "+version)
}

// growBranch adds a commit on top of a branch — the human fixup that
// leaves the tip unverified while the verdict sits a commit behind —
// and returns the new tip.
func growBranch(t *testing.T, repo *git.Repo, branch, content, message string) string {
	t.Helper()
	scratch := branch + "-scratch"
	sha := gittest.Commit(t, repo, scratch, branch, "sysutils/jq/Portfile", content, message)
	gittest.MoveBranch(t, repo, branch, sha)
	require.NoError(t, repo.DeleteBranch(context.Background(), scratch))
	return sha
}

// writeRuns records runs on a commit's note for port jq, keeping any
// the note already holds.
func writeRuns(t *testing.T, repo *git.Repo, sha string, runs map[string]lifecycle.Run) {
	t.Helper()
	ctx := context.Background()
	n, err := lifecycle.LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	for plat, r := range runs {
		n.Runs[plat] = r
	}
	require.NoError(t, lifecycle.WriteNote(ctx, repo, n))
}

// runningRun is a linted run in flight on the fake provider, started
// at the pinned time.
func runningRun(jobID string) lifecycle.Run {
	return lifecycle.Run{State: "running",
		Job: verify.Job{Provider: "fake", ID: jobID, Started: goldenStart}, Linted: true}
}

// goldenStatesRepo is one branch per state the renderers distinguish,
// plus the three shapes of an unnoted tip. Half the verdicts are
// seeded settled; the other half are seeded running with the fake
// scripted to settle them during the status pass, so the transcript
// covers settle's own rendering — the kept handle, the lint evidence,
// the refusal, the dependency diagnosis — and not only the seeded
// strings.
func goldenStatesRepo(t *testing.T) (*git.Repo, *verifytest.Fake) {
	t.Helper()
	repo := goldenRepo(t)
	ctx := context.Background()
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	base, err := repo.RevParse(ctx, primary)
	require.NoError(t, err)

	// Settled by this pass, from scripted verdicts.
	for _, c := range []struct{ suffix, version, job string }{
		{"blocked", "2.3", "fake-blocked"},
		{"errored", "2.6", "fake-errored"},
		{"failed", "2.7", "fake-failed"},
		{"passed", "2.8", "fake-passed"},
		{"running", "2.9", "fake-running"},
		{"unsupported", "3.2", "fake-unsupported"},
		{"vanished", "3.3", "fake-vanished"},
	} {
		sha := mintBranch(t, repo, c.suffix, c.version)
		writeRuns(t, repo, sha, map[string]lifecycle.Run{"Testos": runningRun(c.job)})
	}

	// Seeded settled, in the shapes cancel, deferral and supersession
	// leave behind.
	writeRuns(t, repo, mintBranch(t, repo, "canceled", "2.4"), map[string]lifecycle.Run{
		"Testos": {State: "canceled", Job: verify.Job{Provider: "fake", ID: "fake-canceled", Started: goldenStart},
			Detail: "canceled by the user"},
	})
	writeRuns(t, repo, mintBranch(t, repo, "deferred", "2.5"), map[string]lifecycle.Run{
		"Testos": {State: "deferred", Detail: (&verify.CapacityError{Busy: 2, Cap: 2}).Error()},
	})
	writeRuns(t, repo, mintBranch(t, repo, "superseded", "3.0"), map[string]lifecycle.Run{
		"Testos": {State: "superseded", Job: verify.Job{Provider: "fake", ID: "fake-superseded", Started: goldenStart},
			Detail: "canceled: the branch moved to " + git.Abbrev(base)},
	})
	// A verdict set: passed and tested on one platform, declined on
	// another — the multi-line rendering.
	multi := mintBranch(t, repo, "multi", "2.1")
	writeRuns(t, repo, multi, map[string]lifecycle.Run{
		"Testos": {State: "passed", Tested: true, Linted: true, Lint: "clean"},
		"Oldos":  {State: "unsupported", Detail: "declares known_fail on Oldos"},
	})

	// Unnoted tips: never verified; verified a commit behind; and the
	// same tree as a verified commit under a reworded message.
	mintBranch(t, repo, "unnoted", "3.1")
	behind := mintBranch(t, repo, "behind", "2.2")
	writeRuns(t, repo, behind, map[string]lifecycle.Run{"Testos": {State: "passed", Linted: true, Lint: "clean"}})
	growBranch(t, repo, "dockhand/jq-behind", "version 2.2\nrevision 1\n", "jq: rebuild against the new libjq")
	_, err = repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-amended", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 2.1\n"), Message: "jq: update to 2.1 (reworded)",
	})
	require.NoError(t, err)

	fake := &verifytest.Fake{
		States: map[string]verify.Status{
			"fake-passed":      {State: verify.Passed, Handle: "dockhand-worker-passed"},
			"fake-failed":      {State: verify.Failed, Handle: "dockhand-worker-failed"},
			"fake-unsupported": {State: verify.Failed, Handle: "dockhand-worker-unsupported"},
			"fake-blocked":     {State: verify.Failed, Handle: "dockhand-worker-blocked"},
			"fake-errored":     {State: verify.Errored, Detail: "guest agent unreachable after boot"},
		},
		Vanished: map[string]bool{"fake-vanished": true},
		Logs: map[string]string{
			"fake-passed": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n",
			"fake-failed": "--->  Building jq\n" +
				"Error: Failed to build jq: command execution failed\n" +
				"Error: See /opt/local/var/macports/logs/x/main.log for details.\n",
			"fake-unsupported": "Error: jq is known to fail on this platform\n",
			"fake-blocked": "--->  Building olm\n" +
				"Error: Failed to build olm: command execution failed\n" +
				"Error: See /opt/local/var/macports/logs/x/main.log for details.\n",
		},
	}
	return repo, fake
}

// goldenPromotedRepo is a checkout with an upstream remote (URL only,
// never contacted), a pushable bare fork owned by the gh login, and
// one branch per PR standing status and clean distinguish: open,
// closed, merged, merged with the bytes already on the primary branch,
// promoted with no PR, promoted with the lookup failing — and one
// never promoted at all.
func goldenPromotedRepo(t *testing.T) (*git.Repo, *goldenGh) {
	t.Helper()
	repo := goldenRepo(t)
	ctx := context.Background()
	gittest.BareFork(t, repo, "herbygillot", "herby")

	passed := map[string]lifecycle.Run{"Testos": {State: "passed", Linted: true, Lint: "clean"}}
	for _, c := range []struct {
		suffix, version string
		noted, pushed   bool
	}{
		{"closed", "2.0", true, true},
		{"local", "2.2", true, false},
		{"merged", "2.3", true, true},
		{"nopr", "2.4", false, true},
		{"open", "2.5", true, true},
		{"outage", "2.6", false, true},
	} {
		sha := mintBranch(t, repo, c.suffix, c.version)
		if c.noted {
			writeRuns(t, repo, sha, passed)
		}
		if c.pushed {
			require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-"+c.suffix))
		}
	}
	// Landed: the branch's bytes are already what the primary branch
	// carries, which is the confirmation half of clean's merged verdict.
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	_, err = repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-landed", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 1.7\n"), Message: "jq: update to 1.7",
	})
	require.NoError(t, err)
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-landed"))

	gh := &goldenGh{login: "herbygillot", prs: map[string]string{
		"dockhand/jq-open":   `[{"number":77,"title":"jq: update to 2.5","state":"open","html_url":"https://x/77"}]`,
		"dockhand/jq-closed": `[{"number":78,"title":"jq: update to 2.0","state":"closed","html_url":"https://x/78"}]`,
		"dockhand/jq-merged": `[{"number":79,"title":"jq: update to 2.3","state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/79"}]`,
		"dockhand/jq-landed": `[{"number":80,"title":"jq: update to 1.7","state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/80"}]`,
	}, broken: map[string]bool{"dockhand/jq-outage": true}}
	return repo, gh
}

// goldenGh scripts GitHub per branch: the head-ref lookup answers from
// prs (a branch it does not know has no PR), and a branch in broken
// fails the lookup itself. ghFake answers every branch alike, which
// the single-branch promote tests want and a multi-branch sweep cannot
// use.
type goldenGh struct {
	login  string
	prs    map[string]string
	broken map[string]bool
	calls  [][]string
}

func (g *goldenGh) run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	switch {
	case len(args) >= 2 && args[0] == "api" && args[1] == "user":
		return g.login + "\n", nil
	case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "/pulls?head="):
		_, rest, _ := strings.Cut(args[1], "/pulls?head=")
		ref, _, _ := strings.Cut(rest, "&")
		_, branch, _ := strings.Cut(ref, ":")
		if g.broken[branch] {
			return "", errors.New("gh api: HTTP 502 from api.github.com (scripted outage)")
		}
		if js, ok := g.prs[branch]; ok {
			return js, nil
		}
		return "[]", nil
	}
	return "", fmt.Errorf("goldenGh: unscripted call %v", args)
}

// testTools is the finder a test's run resolves tools through, when a
// test has stated one: tartAbsent and tartOnPath install a finder that
// answers for tart by name and falls through to the real PATH search
// for everything else (git is genuinely needed). It is state of the
// test binary, not of the product — the product has no package-level
// finder — and it is safe because the tests in this package are serial
// by design (none calls t.Parallel). testFinder reads it, so the
// fixtures and states built after the helper ran pick it up and the
// helpers' call sites stay as they are.
var testTools *tool.Finder

// stubTart installs a finder for the rest of the test that answers the
// tart lookup with path and err, and every other lookup for real.
func stubTart(t *testing.T, path string, err error) {
	t.Helper()
	prev := testTools
	testTools = tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return path, err
		}
		return exec.LookPath(name)
	})
	t.Cleanup(func() { testTools = prev })
}

// testFinder is the finder a fixture or a run state should carry: the
// stated one when a test stated one, else the real PATH search — the
// composition root's own answer, for the verbs whose transcript does
// not depend on tart.
func testFinder() *tool.Finder {
	if testTools != nil {
		return testTools
	}
	return tool.NewFinder(nil)
}

// tartAbsent stubs the tool lookup so TartPresent answers no: no
// deferred pump, no orphan sweep, whatever the machine has installed.
// tartOnPath (deferred_pump_test.go) is its counterpart.
func tartAbsent(t *testing.T) {
	t.Helper()
	stubTart(t, "", errors.New("tart stubbed absent"))
}

// goldenState is a run against the repository with the fake provider
// wired and the streams captured separately. A nil fake leaves the
// provider unwired, which is its own transcript.
func goldenState(repo *git.Repo, fake *verifytest.Fake) (*runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb}
	if fake != nil {
		rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }
	}
	return rs, &out, &errb
}

// goldenPortdir copies the bump fixture — a synthetic port whose
// upstream releases sit beside it under files/upstream, fetched over
// file:// — into a fresh <category>/<port> layout and returns the
// portdir. The copy is what a realization may write to; the fixture
// stays pristine. The path is resolved so the transcript's portdir is
// exactly the string the verb prints.
func goldenPortdir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return copyBumpee(t, root)
}

// goldenPortRepo is goldenPortdir inside a git repository with the
// fixture committed on the primary branch — what --diff plans against.
func goldenPortRepo(t *testing.T) string {
	t.Helper()
	pinGitDates(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	portdir := copyBumpee(t, root)
	gittest.Init(t, testFinder(), root, nil)
	return portdir
}

func copyBumpee(t *testing.T, root string) string {
	t.Helper()
	portdir := filepath.Join(root, "devel", "bumpee")
	src := filepath.Join("testdata", "golden", "ports", "devel", "bumpee")
	require.NoError(t, os.CopyFS(portdir, os.DirFS(src)))
	return portdir
}

// transcript is one invocation as the user saw it: the process exit
// code, both streams, and any further product a case pins under its
// own heading.
type transcript struct {
	exit     int
	stdout   string
	stderr   string
	sections []section
}

type section struct{ name, body string }

// capture executes an action against rs the way the command tree
// would, and returns what the user would have seen. A failing action
// is rendered as cobra renders it — usage silenced, the "dockhand:"
// prefix, the message, one line — and classified through the same
// ExitCode table main exits with.
func capture(t *testing.T, rs *runstate.Context, out, errb *bytes.Buffer, a Action) transcript {
	t.Helper()
	t.Cleanup(rs.Close)
	err := a.Execute(context.Background(), rs)
	if err != nil {
		fmt.Fprintln(errb, "dockhand:", err.Error())
	}
	return transcript{exit: ExitCode(err), stdout: out.String(), stderr: errb.String()}
}

// captureExecute runs the whole command tree, flags and all, the way
// main does — for the verbs whose seams need no fakes.
func captureExecute(t *testing.T, args ...string) transcript {
	t.Helper()
	t.Setenv("DOCKHAND_TREE", "")
	t.Setenv("DOCKHAND_PREFIX", "")
	var out, errb bytes.Buffer
	code := execute(context.Background(), "test", args, &out, &errb)
	return transcript{exit: code, stdout: out.String(), stderr: errb.String()}
}

// render writes the transcript in the golden's form: the exit code,
// then each section's bytes verbatim under its heading, every line
// indented two spaces so headings cannot be mistaken for content. A
// section whose bytes do not end in a newline says so, the way diff
// does, so the rendering loses nothing.
func (tr transcript) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit: %d\n", tr.exit)
	renderSection(&b, "stdout", tr.stdout)
	renderSection(&b, "stderr", tr.stderr)
	for _, s := range tr.sections {
		renderSection(&b, s.name, s.body)
	}
	return b.String()
}

func renderSection(b *strings.Builder, name, body string) {
	fmt.Fprintf(b, "--- %s\n", name)
	if body == "" {
		return
	}
	lines := strings.Split(body, "\n")
	trailing := lines[len(lines)-1] == ""
	if trailing {
		lines = lines[:len(lines)-1]
	}
	for _, l := range lines {
		b.WriteString("  ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	if !trailing {
		b.WriteString("\\ no newline at end of section\n")
	}
}

// rewrite is one exact replacement a case declares for a path only it
// knows: the repository root, the portdir under test.
type rewrite struct{ from, to string }

// elapsedRE matches the age of a running job as status and verify
// print it — "verifying (37h12m5s)", "already verifying on Testos
// (37h12m5s)" — the one value in a transcript that time itself moves.
// The age is time.Since the pinned goldenStart, so on a clock set
// before that instant it prints negative ("-2h0m0s"); the sign is
// matched so a skewed clock reads as the same transcript, not as a
// rendering change.
var elapsedRE = regexp.MustCompile(`(verifying (?:on \S+ )?\()-?(?:\d+h)?(?:\d+m)?\d+(?:\.\d+)?s\)`)

// normalize applies the case's replacements — temporary paths, which
// differ per run — and then the elapsed-time rewrite. Nothing else is
// touched: shas, job IDs, hashes and every message are pinned raw.
func normalize(s string, rw []rewrite) string {
	for _, r := range rw {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return elapsedRE.ReplaceAllString(s, "${1}<elapsed>)")
}

// ghSections renders the pr create and pr edit calls a promote made:
// the arguments one per line, and the body — the last argument —
// verbatim under its own heading, so the PR template is pinned too.
func ghSections(gh *ghFake) []section {
	var out []section
	for _, verb := range []string{"create", "edit"} {
		for _, call := range gh.called(verb) {
			args, body := call[:len(call)-1], call[len(call)-1]
			out = append(out,
				section{"gh " + strings.Join(call[:2], " ") + " (arguments)", strings.Join(args, "\n") + "\n"},
				section{"gh " + strings.Join(call[:2], " ") + " (body)", body})
		}
	}
	return out
}

// fileSection reads a file the verb wrote, for the products the
// streams do not show.
func fileSection(t *testing.T, name, path string) section {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return section{name, string(b)}
}
