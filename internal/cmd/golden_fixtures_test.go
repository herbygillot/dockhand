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
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
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

// platRun is one platform's row in a fixture note: the guest, and the
// verdict reached inside it.
//
// Two values because the record keeps two. A run says what was
// concluded about one subject; a job says what the environment was,
// whether the test suite was asked of it and whether it has been given
// back — facts about a guest that every subject in a change shares. A
// fixture that spelled them as one value would be pinning a note shape
// nothing writes.
//
// A zero Job means no guest at all, which is what a queued run has: it
// was never submitted, so there is no environment to describe.
type platRun struct {
	Job record.JobRecord
	Run record.Run
}

// writeRuns records runs on a commit's note for port jq, keeping
// anything the note already holds.
func writeRuns(t *testing.T, repo *git.Repo, sha string, rows map[string]platRun) {
	t.Helper()
	writeSubjectRuns(t, repo, sha, "jq", rows)
}

// writeSubjectRuns is the same for a note about some other port — the
// subport shape, where what the note names is not the portdir's base.
//
// The subject is written here because these fixtures commit with
// gittest rather than through mint, so nothing bore the record: the
// port would otherwise survive in the run keys alone, where no
// projection reads it, and the note would render a verdict about
// nobody.
func writeSubjectRuns(t *testing.T, repo *git.Repo, sha, port string, rows map[string]platRun) {
	t.Helper()
	ctx := context.Background()
	l := ledger.Open(repo)
	n, err := l.LoadOrStart(ctx, sha)
	require.NoError(t, err)
	if len(n.Subjects) == 0 {
		n.Subjects = []record.Subject{{Port: port, Names: []string{port}}}
	}
	for plat, row := range rows {
		if row.Job.Job.ID != "" {
			n.Jobs[plat] = row.Job
		}
		row.Run.Platform = plat
		n.Runs[record.RunKey(port, plat)] = row.Run
	}
	require.NoError(t, l.Write(ctx, n))
}

// bearRecord opens a commit's record the way mint bears one: the
// subjects, and how far the change's contract reaches. No run, because
// a mint has submitted nothing yet — and for a change bound to the
// branch alone, there never will be one.
func bearRecord(t *testing.T, repo *git.Repo, sha string, dest record.Destination) {
	t.Helper()
	ctx := context.Background()
	l := ledger.Open(repo)
	n, err := l.LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}}}
	n.Destination = dest
	require.NoError(t, l.Write(ctx, n))
}

// liveGuest is an environment still standing on the fake provider,
// started at the pinned time. handle names it when a failure kept it as
// the debug handle, and is empty for a build still going — the note
// learns the name when the run settles, not when it starts.
func liveGuest(jobID, handle string) record.JobRecord {
	return record.JobRecord{
		Job:    verify.Job{Provider: "fake", ID: jobID, Started: goldenStart},
		Handle: handle,
	}
}

// spentGuest is an environment that was entered and given back, which
// is what a settled pass leaves behind.
func spentGuest(jobID string) record.JobRecord {
	return record.JobRecord{
		Job:      verify.Job{Provider: "fake", ID: jobID, Started: goldenStart},
		Released: true,
	}
}

// runningOn is a linted run in flight in the named guest.
func runningOn(jobID string) platRun {
	return platRun{Job: liveGuest(jobID, ""), Run: record.Run{State: record.Running, Linted: true}}
}

// keptOn is a failure whose guest is still standing as the debug
// handle. The provider names the environment after the job, which is
// what the settle stamps onto the record.
func keptOn(jobID, detail string) platRun {
	return platRun{Job: liveGuest(jobID, jobID), Run: record.Run{State: record.Failed, Detail: detail}}
}

// passedOn is a settled pass: linted clean, and its guest handed back.
func passedOn(jobID string) platRun {
	return platRun{Job: spentGuest(jobID),
		Run: record.Run{State: record.Passed, Linted: true, Lint: "clean"}}
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
		writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn(c.job)})
	}

	// Seeded settled, in the shapes cancel, deferral and supersession
	// leave behind.
	writeRuns(t, repo, mintBranch(t, repo, "canceled", "2.4"), map[string]platRun{
		"Testos": {Job: spentGuest("fake-canceled"),
			Run: record.Run{State: record.Canceled, Detail: "canceled by the user"}},
	})
	// A queued run names no job: nothing was submitted, so there is no
	// environment to describe.
	writeRuns(t, repo, mintBranch(t, repo, "queued", "2.5"), map[string]platRun{
		"Testos": {Run: record.Run{State: record.Queued,
			Detail: (&verify.CapacityError{Busy: 2, Cap: 2}).Error()}},
	})
	writeRuns(t, repo, mintBranch(t, repo, "superseded", "3.0"), map[string]platRun{
		"Testos": {Job: spentGuest("fake-superseded"),
			Run: record.Run{State: record.Superseded,
				Detail: "canceled: the branch moved to " + git.Abbrev(base)}},
	})
	// A verdict set: passed and tested on one platform, declined on
	// another — the multi-line rendering. The test was asked of the
	// guest, so it is recorded on the job.
	multi := mintBranch(t, repo, "multi", "2.1")
	writeRuns(t, repo, multi, map[string]platRun{
		"Testos": {Job: record.JobRecord{
			Job:  verify.Job{Provider: "fake", ID: "fake-multi", Started: goldenStart},
			Test: true, Released: true},
			Run: record.Run{State: record.Passed, Linted: true, Lint: "clean"}},
		// Declined before anything was submitted, so the platform names no
		// job at all — a terminal run with no guest behind it, which is
		// what the pre-flight decline actually records and the only shape
		// besides a queued run that has none.
		"Oldos": {Run: record.Run{State: record.Unsupported, Detail: "declares known_fail on Oldos"}},
	})

	// A branch minted with --no-verify. Schema 3 bears the record at
	// mint, so this branch has a note from the moment it exists and will
	// never hold a run: nobody asked for a verdict, and the drain steps
	// over a change bound to the branch alone. Under schema 2 there was
	// no note here at all and the standing read as bare drift.
	bearRecord(t, repo, mintBranch(t, repo, "unasked", "3.4"), record.ToBranch)

	// The same branch after the user grew it. The tip has no note and
	// the commit behind it has one holding nothing, which is the shape
	// the drift walk must not read as "verified at a commit the branch
	// moved past": there is no verdict back there to have been passed.
	bearRecord(t, repo, mintBranch(t, repo, "unasked-grown", "3.5"), record.ToBranch)
	growBranch(t, repo, "dockhand/jq-unasked-grown", "version 3.5\nrevision 1\n",
		"jq: fix the livecheck while I am here")

	// Unnoted tips: never verified; verified a commit behind; and the
	// same tree as a verified commit under a reworded message.
	mintBranch(t, repo, "unnoted", "3.1")
	behind := mintBranch(t, repo, "behind", "2.2")
	writeRuns(t, repo, behind, map[string]platRun{"Testos": passedOn("fake-behind")})
	growBranch(t, repo, "dockhand/jq-behind", "version 2.2\nrevision 1\n", "jq: rebuild against the new libjq")
	_, err = repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-amended", Base: primary, Commits: []git.Commit{{
			Files:   []git.File{{Path: "sysutils/jq/Portfile", Content: []byte("version 2.1\n")}},
			Message: "jq: update to 2.1 (reworded)",
		}},
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

	passed := map[string]platRun{"Testos": passedOn("fake-passed")}
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
		Branch: "dockhand/jq-landed", Base: primary, Commits: []git.Commit{{
			Files:   []git.File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.7\n")}},
			Message: "jq: update to 1.7",
		}},
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

// tartAbsent stubs the tool lookup so the machine reads as having no
// tart: no deferred pump, no orphan sweep, whatever is installed.
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
	// A version, because Root() always threads one through and the PR
	// body signs off with it. Left empty, the promote goldens pinned the
	// unversioned sign-off — a shape the real CLI cannot produce, in the
	// three files that exist to show what a real promote writes.
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb, Version: "1.2.3"}
	if fake != nil {
		// Both seams, from the one fake: a golden run stands in for a
		// machine, and a machine that can verify can also say what it is
		// running.
		rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }
		rs.Lister = rs.Verifier
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
