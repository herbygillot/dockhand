package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/report"
	"github.com/herbygillot/dockhand/internal/tool"
)

// Build step 10's done-criteria, held mechanically rather than by
// review. Every one of these is checkable on every build forever, which
// is the point: the defect this restructure removes is one that grew
// from "just one more import" and "just one more step", and a criterion
// nobody can run is a criterion that comes back.

// ENGINE IS GONE, AND SO ARE ITS TWO NEIGHBOURS. The census is over the
// import graph rather than over the filesystem, because a directory
// that still existed and that nothing imported would be dead code and a
// package that was deleted and re-created under another name would pass
// a directory check.
func TestTheDissolvedPackagesAreGone(t *testing.T) {
	for _, gone := range []string{
		"github.com/herbygillot/dockhand/internal/engine",
		"github.com/herbygillot/dockhand/internal/runstate",
		"github.com/herbygillot/dockhand/internal/render",
		"github.com/herbygillot/dockhand/internal/cmd",
		"github.com/herbygillot/dockhand/internal/verdict",
	} {
		out, err := exec.Command("go", "list", gone).CombinedOutput() //nolint:gosec // the argument is a constant of this test
		assert.Error(t, err, "%s still exists: %s", gone, out)
	}
}

// NOTHING OUTSIDE report IMPORTS report. This is the half of the
// criterion the depguard rule cannot state, because depguard denies
// edges FROM a package and this denies edges INTO one.
func TestNothingOutsideCliImportsReport(t *testing.T) {
	// The whole module, not "./..." — a test runs in its own package's
	// directory, where "./..." would list exactly one package and the
	// census would pass by seeing nothing.
	for _, pkg := range deps(t, "github.com/herbygillot/dockhand/...") {
		if pkg == "github.com/herbygillot/dockhand/internal/report" {
			continue
		}
		for _, imp := range imports(t, pkg) {
			if imp != "github.com/herbygillot/dockhand/internal/report" {
				continue
			}
			assert.Equal(t, "github.com/herbygillot/dockhand/internal/cli", pkg,
				"%s imports report; only the adapter that renders may", pkg)
		}
	}
}

// cli IMPORTS app AND report AND NOTHING ELSE OF THE DOMAIN, where "the
// domain" is the seven packages that own a lifecycle or a judgment.
//
// THE COMPOSITION ROOT NAMES THE TYPES IT CONSTRUCTS, and that is why
// this list is what it is rather than empty. app declares its
// dependencies BY TYPE — app.Change carries a *git.Repo, a
// *statestore.Store, a *ledger.Ledger, a run.Stager, a run.Local and a
// change.Prepared; app.Cycle carries a publish.Env, a publish.Pace and a
// change.Evaluator — so an adapter that could not name git, statestore,
// ledger, run, change and publish could not build an operation at all.
// What the criterion actually forbids is REACHING PAST app: calling a
// lifecycle mutator, opening an Amend, assembling a road out of stages.
// That is what the next test holds.
func TestCliNamesOnlyWhatItMustConstruct(t *testing.T) {
	// The domain packages a composition root has no business naming: each
	// of these is reached through a value app hands out or holds, and
	// naming one here would mean this package had acquired a judgment.
	forbidden := map[string]string{
		"github.com/herbygillot/dockhand/internal/darwin/abi": "an ABI measurement is a verification's evidence",
		"github.com/herbygillot/dockhand/internal/artifact":   "what a guest reported is the provider's and the judge's",
	}
	for _, imp := range imports(t, "github.com/herbygillot/dockhand/internal/cli") {
		if why, bad := forbidden[imp]; bad {
			t.Errorf("cli imports %s: %s", imp, why)
		}
	}
	// And it does import the two it is supposed to.
	all := strings.Join(imports(t, "github.com/herbygillot/dockhand/internal/cli"), " ")
	assert.Contains(t, all, "github.com/herbygillot/dockhand/internal/app")
	assert.Contains(t, all, "github.com/herbygillot/dockhand/internal/report")
}

// NO LIFECYCLE MUTATOR IS CALLED FROM cli. The mutators are the
// functions whose names end in `In` and which take a *statestore.Txn —
// change.MintIn, run.EnqueueIn, publish.RetireIn, and their kin — and
// the whole of R22's ruling is that cli reaches them through an
// operation or an entry point and never directly. A grep is the honest
// check here: the packages are legitimately imported, so an import
// census cannot see the difference.
func TestCliOpensNoTransactionAndCallsNoMutator(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f) //nolint:gosec // the path came from a glob of this directory
		require.NoError(t, err)
		body := string(src)
		assert.NotContains(t, body, ".Amend(",
			"%s opens a statestore transaction; a cross-lifecycle write is an operation's, and hold/unhold/dismiss are app entry points for exactly this reason", f)
		for _, mutator := range []string{"change.MintIn(", "change.CloseIn(", "change.ExtendIn(",
			"change.HoldIn(", "change.UnholdIn(", "change.AnswerIn(", "change.DemolishIn(",
			"run.EnqueueIn(", "run.WithdrawIn(", "publish.RetireIn(", "publish.DeleteForkIn("} {
			assert.NotContains(t, body, mutator,
				"%s calls %s directly; that is what app is for", f, mutator)
		}
	}
}

// A SECOND DISPATCHER ON A CHECKOUT EXITS 0 NAMING THE ONE THAT HOLDS
// THE LOCK. It does not wait and it does not run a degraded pass: the
// queue is the state ref and the resident process is already draining
// it, so nothing was refused and there is nothing for this one to do.
func TestASecondDispatcherExitsZeroNamingTheHolder(t *testing.T) {
	repo := emptyRepo(t)
	ctx := context.Background()
	path, err := lockPath(ctx, repo, dispatchLock)
	require.NoError(t, err)

	// The stamp names a LIVE process, because lockfile.Probe repeats a
	// stamp only when this host can still see the process that wrote it
	// — a losing caller must not name a pid that is not running.
	release, err := lockfile.Hold(ctx, path, liveHolder(t, "dispatch"), 0)
	require.NoError(t, err)
	t.Cleanup(release)

	var out bytes.Buffer
	s := &Services{Out: &out, Err: &bytes.Buffer{}, Tools: testFinder(), Now: time.Now, TreeRoot: repo.Root}
	err = dispatchLoop(ctx, s, repo, app.CycleRequest{}, publish.Pace{Set: true, Max: 1, Window: time.Hour}, loop{every: time.Minute})

	require.NoError(t, err, "a second dispatcher exits 0")
	assert.Equal(t, exitcode.OK, ExitCode(err))
	assert.Contains(t, out.String(), "already resident")
	assert.Contains(t, out.String(), strconv.Itoa(os.Getpid()), "it names the holder")
}

// THE RESIDENCY PROBE TAKES NOTHING, so two verbs probing at once
// cannot each read the other as the resident — the defect an
// adversarial pass found in the draft that said "try-lock the lockfile".
func TestProbingResidencyNeitherTakesTheLockNorSeesAProber(t *testing.T) {
	repo := emptyRepo(t)
	ctx := context.Background()

	assert.Equal(t, app.NoDispatcher, probeResidency(ctx, repo).State,
		"a checkout nobody is dispatching on has no dispatcher")
	// Two probes in a row, which under a try-lock would have had the
	// second see the first.
	assert.Equal(t, app.NoDispatcher, probeResidency(ctx, repo).State)

	path, err := lockPath(ctx, repo, dispatchLock)
	require.NoError(t, err)
	release, err := lockfile.Hold(ctx, path, liveHolder(t, "dispatch"), 0)
	require.NoError(t, err)
	r := probeResidency(ctx, repo)
	assert.Equal(t, app.DispatcherResident, r.State)
	assert.Equal(t, os.Getpid(), r.Holder.PID)
	release()
	assert.Equal(t, app.NoDispatcher, probeResidency(ctx, repo).State)
}

// THE REMEDY LINE IS DERIVED AND NOT WRITTEN, which makes a cycle report
// telling its reader to run cycle UNREPRESENTABLE rather than merely
// fixed. There are exactly two instructions and one admission.
func TestTheRemedyLineCannotTellYouToRunTheVerbYouRan(t *testing.T) {
	resident := report.Remedy(app.Residency{State: app.DispatcherResident, Holder: record.OwnerID{PID: 7}})
	assert.Contains(t, resident, "will start it")
	assert.NotContains(t, resident, "dockhand cycle")
	assert.NotContains(t, resident, "dockhand dispatch")

	none := report.Remedy(app.Residency{State: app.NoDispatcher})
	assert.Contains(t, none, "dockhand dispatch")
	assert.Contains(t, none, "dockhand cycle")

	unknown := report.Remedy(app.Residency{State: app.ResidencyUnknown})
	assert.NotContains(t, unknown, "dockhand dispatch",
		"an unreadable lock instructs nothing: rule 7")
}

// THE PERSISTENT FLAGS ARE EXACTLY THREE. --auto and DOCKHAND_AUTO are
// retired: the invoker is which process acted, so `dockhand dispatch
// --auto` is a usage error and not a redundancy.
func TestThePersistentFlagsAreExactlyThree(t *testing.T) {
	root := Root("test")
	var got []string
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
	assert.ElementsMatch(t, []string{"prefix", "tree", "debug"}, got)

	err := runCLI(t, "dispatch", "--auto")
	require.Error(t, err)
	assert.Equal(t, exitcode.Usage, ExitCode(err), "an unknown flag is the invocation's problem")
}

// THE FLAGS THAT MOVED, held one refusal at a time. Each of these is a
// spelling the dispatch ruling changed, and a test that only checked the
// new name would pass on a build that still accepted the old one.
func TestTheRetiredSpellingsAreRefused(t *testing.T) {
	requireNoTree(t)
	for _, args := range [][]string{
		{"bump", "--recalc", "jq"},                // deleted into refresh-checksums
		{"bump", "--recheck", "jq"},               // its earlier name, deleted with it
		{"bump", "--trace", "jq"},                 // moved to `dockhand log --trace`
		{"promote", "--no-verify", "dockhand/jq"}, // renamed --ignore
		{"promote", "--closes", "1", "dockhand/jq"},
		{"provision", "tart", "--recheck"}, // renamed --validate
	} {
		err := runCLI(t, args...)
		require.Error(t, err, "%v", args)
		assert.Equal(t, exitcode.Usage, ExitCode(err), "%v", args)
	}
}

// `provision tart xcode` NESTS under the tart provisioner: baking a
// toolchain into a golden image is a TART act, and the path should say
// which provider it belongs to before a second one exists.
func TestProvisionXcodeNestsUnderTart(t *testing.T) {
	root := Root("test")
	c, _, err := root.Find([]string{"provision", "tart", "xcode"})
	require.NoError(t, err)
	assert.Equal(t, "xcode", c.Name())
	assert.Equal(t, "tart", c.Parent().Name())
	assert.Equal(t, "provision", c.Parent().Parent().Name())

	// And the old flat spelling is gone.
	_, _, err = root.Find([]string{"provision", "xcode"})
	if err == nil {
		flat, _, _ := root.Find([]string{"provision", "xcode"})
		assert.NotEqual(t, "xcode", flat.Name(), "provision xcode still resolves flat")
	}
}

// THE SETUP GROUP holds provision and doctor. doctor moved here from
// Reports because it reports on the MACHINE and not on the ports, and
// the grouping is the only place that distinction is said to a reader.
func TestTheSetupGroupHoldsProvisionAndDoctor(t *testing.T) {
	root := Root("test")
	byGroup := map[string][]string{}
	for _, c := range root.Commands() {
		byGroup[c.GroupID] = append(byGroup[c.GroupID], c.Name())
	}
	assert.ElementsMatch(t, []string{"provision", "doctor"}, byGroup["setup"])
	assert.Contains(t, byGroup["branch"], "dispatch", "the pass verbs are filed together")
	assert.Contains(t, byGroup["branch"], "cycle")
}

// --publish-max 0 IS A USAGE ERROR and not a way to disable publication:
// a cap of zero and an unconfigured cap are the same value, and
// --no-publish already says the thing plainly.
func TestDispatchRefusesTheFlagsARunawayLoopWouldNeed(t *testing.T) {
	requireNoTree(t)
	for _, args := range [][]string{
		{"dispatch", "--publish-max", "0"},
		{"dispatch", "--superseded"},           // an inference, unattended, forever
		{"dispatch", "--dry-run"},              // a loop that previews forever
		{"dispatch", "--every", "10s"},         // below the floor
		{"dispatch", "--publish-every", "48h"}, // past the compaction floor
	} {
		err := runCLI(t, args...)
		require.Error(t, err, "%v", args)
		assert.Equal(t, exitcode.Usage, ExitCode(err), "%v", args)
	}
	// Both refusals lift under --once, which is the whole distinction:
	// what a resident loop may not do, one supervised pass may.
	for _, args := range [][]string{
		{"dispatch", "--once", "--superseded"},
		{"dispatch", "--once", "--dry-run"},
	} {
		err := runCLI(t, args...)
		if err != nil {
			assert.NotEqual(t, exitcode.Usage, ExitCode(err),
				"%v is accepted with --once; it failed for another reason: %v", args, err)
		}
	}
}

// NO DEPENDENCY IS RESOLVED THAT THE INVOCATION WILL NOT USE, held on
// the value that decides it. This is the property runstate's lazy memo
// could not have: there, which services an invocation acquired was
// decided deep inside whatever code path happened to ask.
func TestNeedsIsWhatIsAcquiredAndNothingElse(t *testing.T) {
	// --plan resolves the target, asks the port what it evaluates to and
	// reads the base commit's Portfile — so a repository, a tree and an
	// evaluator — and it starts no build and opens no pull request.
	document := app.ChangeRequest{Delivery: app.Document}.Needs()
	assert.False(t, document.Verifier, "a --plan must never boot a provider")
	assert.False(t, document.Forge, "a --plan must never reach the forge")
	assert.True(t, document.Repo && document.Tree && document.Evaluator)

	// A revision bump downloads nothing, and that is a fact about the
	// INTENT rather than about the delivery.
	assert.False(t, app.ChangeRequest{Delivery: app.Enqueue, Fetches: false}.Needs().Fetcher)
	assert.True(t, app.ChangeRequest{Delivery: app.Enqueue, Fetches: true}.Needs().Fetcher)

	// A verifier exactly where an attempt may be started; a forge exactly
	// where a publication may be opened.
	assert.False(t, app.ChangeRequest{Delivery: app.Branch}.Needs().Verifier, "--no-verify starts nothing")
	assert.True(t, app.ChangeRequest{Delivery: app.Enqueue}.Needs().Verifier)
	assert.False(t, app.ChangeRequest{Delivery: app.Enqueue}.Needs().Forge)
	assert.True(t, app.ChangeRequest{Delivery: app.PullRequest}.Needs().Forge)
}

// A SERVICE A ROAD DID NOT DECLARE IS A WIRING GAP AND NOT A FACT ABOUT
// THE MACHINE. Rule 7 in the other direction: a nil repository must
// never be readable as "no repository here".
func TestAnUndeclaredServiceRefusesRatherThanReadingAsAbsent(t *testing.T) {
	s := &Services{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Tools: testFinder(), Now: time.Now}
	require.NoError(t, s.Acquire(context.Background(), app.Needs{}))
	t.Cleanup(s.Close)

	_, err := s.Repo()
	require.ErrorIs(t, err, ErrNotAcquired)
	_, err = s.State()
	require.ErrorIs(t, err, ErrNotAcquired)
	_, err = s.Tree()
	require.ErrorIs(t, err, ErrNotAcquired)
	assert.Nil(t, s.VerifyProvider(), "a road that declared no verifier is handed none, which app reads as ErrNoProvider")
	assert.Zero(t, s.PublishEnv().Repo, "a road that declared no forge is handed a zero Env")
}

// CLOSE RUNS IN REVERSE ORDER OF ACQUISITION, whether the operation
// succeeded or not. The failures are what used to leave directories
// behind.
func TestCloseRunsInReverseOrder(t *testing.T) {
	var order []int
	s := &Services{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	for i := range 3 {
		s.closers = append(s.closers, func() { order = append(order, i) })
	}
	s.Close()
	assert.Equal(t, []int{2, 1, 0}, order)
	s.Close()
	assert.Equal(t, []int{2, 1, 0}, order, "a second Close is a no-op, not a double release")
}

// A TOOL THE MACHINE DOES NOT HAVE IS THE MACHINE'S BAND, and the two
// ways of having no environment are told apart because their remedies
// are.
func TestNoTartIsTheMachineBandOnTheRoadsThatAskedToVerify(t *testing.T) {
	stubTool(t, tool.Tart, "", errors.New("stubbed absent"))
	_, err := realVerifier(testFinder())(context.Background())
	require.Error(t, err)
	assert.Equal(t, exitcode.ToolMissing, ExitCode(err))
}

// emptyRepo is a git checkout with nothing in it, for the tests that
// need a common git dir to put a lockfile in.
func emptyRepo(t *testing.T) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // the arguments are constants of this test
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	repo, err := git.Open(context.Background(), tool.NewFinder(nil), dir)
	require.NoError(t, err)
	return repo
}

// deps and imports read the import graph through `go list`, which is
// the same view the compiler has and the only one that cannot be fooled
// by a directory somebody forgot to delete.
func deps(t *testing.T, pattern string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", pattern).Output() //nolint:gosec // the pattern is a constant of this test
	require.NoError(t, err)
	return strings.Fields(string(out))
}

func imports(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{range .Imports}}{{.}} {{end}}", pkg).Output() //nolint:gosec // the package is this repository's own
	require.NoError(t, err)
	return strings.Fields(string(out))
}

// THE PROCESS EXIT IS WHAT A SHELL READS, and it is built from the same
// classifier the document's twin is, so the two cannot disagree. This
// exercises the whole of execute() — the Find pre-flight, the tree, the
// classifier — rather than ExitCode alone.
func TestTheProcessExitIsTheClassifiersAnswer(t *testing.T) {
	requireNoTree(t)
	assert.Equal(t, exitcode.Usage, code(t, "no-such-verb"),
		"an unknown subcommand returns Usage from execute directly, so the classification stays identity-based")
	assert.Equal(t, exitcode.OK, code(t, "version"))
	assert.Equal(t, exitcode.Usage, code(t, "doctor", "extra"))
}

// EXIT 62 REACHES A SHELL THROUGH `dispatch --once` AND NOWHERE ELSE.
// Publication is the dispatcher's stage: a person's `cycle` publishes
// nothing and can never emit it, and a resident dispatcher never exits.
// This holds the classification rather than a live pass, because what
// moved is which road can produce the code and not what the code means.
func TestTheSpentAllowanceLandsInThePendingBand(t *testing.T) {
	assert.Equal(t, exitcode.PromotionPending, ExitCode(publish.ErrPaceSpent))
	twin := TwinOf(publish.ErrPaceSpent)
	assert.Equal(t, "pending", twin.Family)
	assert.Equal(t, "pace-spent", twin.Reason)
}

// A PASS'S OWN CODE IS 84 AND NOTHING ELSE PRODUCES IT. It reaches a
// process status in exactly two places — `dispatch --once` and a
// person's `cycle` — and Family(84) already answers "partial" with no
// change to exitcode.Family, because the decade IS the family.
func TestThePassBandIsClassifiedWithoutAnEdit(t *testing.T) {
	assert.Equal(t, "partial", exitcode.Family(exitcode.PassNeedsAttention))
	assert.Equal(t, "tree", exitcode.Family(exitcode.BranchMoved))
	assert.Equal(t, "tree", exitcode.Family(exitcode.BranchCheckedOut))
	assert.Equal(t, exitcode.PassNeedsAttention, ExitCode(exitWith(exitcode.PassNeedsAttention)))
	assert.NoError(t, exitWith(exitcode.OK), "a road that computed 0 hands back no error at all")
}
