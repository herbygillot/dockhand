package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// verifyPlan proves a plan's port builds before anything real is
// written. The edited port is rematerialized from the plan itself —
// read the Portfile, hold it to the plan's precondition hash, apply the
// edits, shadow the result — so the port under test is exactly what
// apply would write.
func verifyPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan, release platform.Release, test bool) (string, error) {
	src, err := os.ReadFile(filepath.Join(p.Portdir, macports.PortfileName))
	if err != nil {
		return "", err
	}
	edited, err := p.Materialize(src)
	if errors.Is(err, plan.ErrDrift) {
		return "", fmt.Errorf("%w: %s", plan.ErrDrift, p.Portdir)
	}
	if err != nil {
		return "", err
	}
	root, err := rs.TempDir()
	if err != nil {
		return "", err
	}
	// Shadow needs no evaluator: it is a copy, and the guest does the
	// evaluating from here on.
	h := port.New(tree.Target{Portdir: p.Portdir}, nil).WithTempDir(root)
	shadow, cleanup, err := h.Shadow(edited)
	if err != nil {
		return "", err
	}
	defer cleanup()

	lint, err := runVerification(ctx, rs, p.Port, shadow.Target.Portdir, release, test)
	if err != nil {
		return "", err
	}
	return lint, nil
}

// runVerification submits one portdir to the VM provider and reports
// the verdict. Both verification modes arrive here: a plan's shadowed
// portdir, and a portdir as it sits in the tree. The returned lint is
// the run's evidence on a pass — the same summary a settled background
// run records — so a gate-verified tip's note says exactly what a
// background-verified one would.
func runVerification(ctx context.Context, rs *runstate.Context, portName, portdir string, release platform.Release, test bool) (string, error) {
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return "", err
	}
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{portdir},
		Platform: release,
		Owner:    rs.TreeRoot,
		Test:     test,
	})
	if err != nil {
		return "", err
	}
	caps := prov.Capabilities()
	on := release
	if on.IsZero() && len(caps.Platforms) > 0 {
		on = caps.Platforms[0]
	}
	fmt.Fprintf(rs.Err, "verifying %s on %s… ", portName, on)
	st, log, err := awaitVerdict(ctx, prov, job)
	if err != nil {
		fmt.Fprintln(rs.Err)
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return "", err
	}
	switch st.State {
	case verify.Passed:
		fmt.Fprintln(rs.Err, "passed")
		return verdict.LintSummary(log), prov.Release(ctx, job)
	case verify.Failed:
		fmt.Fprintln(rs.Err, "FAILED")
		tail := log
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		fmt.Fprintln(rs.Err, tail)
		// The environment is kept on purpose: it is the debug handle.
		return "", &lifecycle.VerifyFailedError{Port: portName, Handle: st.Handle}
	case verify.Errored:
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return "", fmt.Errorf("%w: %s", verify.ErrNoEnvironment, st.Detail)
	case verify.Running:
	}
	return "", fmt.Errorf("verify: job ended in state %s", st.State)
}

// awaitVerdict polls to a terminal state, then fetches the log once —
// the one read every evidence extraction shares.
func awaitVerdict(ctx context.Context, prov verify.Verifier, job verify.Job) (verify.Status, string, error) {
	st, err := verify.Await(ctx, prov, job, 3*time.Second)
	if err != nil {
		return verify.Status{}, "", err
	}
	log, lerr := prov.Log(ctx, job)
	if lerr != nil {
		log = ""
	}
	return st, log, nil
}

// verifyAction proves a port builds as it sits, in a pristine
// environment, without writing anything. This is state verification —
// it tests the portdir's current content, whoever produced it, which
// is what makes human edits after a dockhand change verifiable at all.
type verifyAction struct {
	target string
	on     []string
	test   bool
	trace  bool
}

var _ Action = verifyAction{}

func (a verifyAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// The in-flight branch wins, resolved the way every sibling verb
	// resolves it — a branch name outright, or a port name that names
	// exactly one dockhand branch. The branch is the unit (D21), and
	// verifying one means verifying its tip sha, whoever made it. A
	// target with no in-flight branch falls through to state
	// verification of the working tree, which a portdir path always
	// reaches directly.
	if repo, err := rs.Repo(ctx); err == nil {
		branch, rerr := lifecycle.ResolveBranch(ctx, repo, a.target)
		switch {
		case rerr == nil:
			return verifyBranch(ctx, rs, repo, a.target, branch, a.on, a.test, a.trace)
		case errors.Is(rerr, lifecycle.ErrAmbiguousTarget):
			return rerr
		}
	}
	var single string
	if len(a.on) > 1 {
		return usagef("state verification of a portdir takes one release; a branch takes lists and \"all\"")
	}
	if len(a.on) == 1 {
		single = a.on[0]
	}
	release, err := releaseFlag(single)
	if err != nil {
		return err
	}
	targets, err := resolveTargets(rs, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("verify takes exactly one port; %q names %d", a.target, len(targets))
	}
	portName := targets[0].Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(targets[0].Portdir))
	}
	_, err = runVerification(ctx, rs, portName, targets[0].Portdir, release, a.test)
	return err
}

// verifyBranch submits a branch's tip for verification in the
// background, exactly as the mint that created it would have: the
// changed portdir is derived from git — diff against the merge base
// with the primary branch, so a human commit's changes count too — and
// materialized from the object database. A job already running for the
// tip is left alone; a running job the branch has moved past is
// canceled first, its worker released and its note marked superseded,
// because a verdict about an abandoned sha is a slot spent on nothing.
// Each release's submit is a claim under the repository's submit lock,
// the same claim status's pump makes: a verify racing a status over
// one deferred run used to start it twice.
func verifyBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, target, branch string, on []string, test, trace bool) error {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return err
	}
	releases, err := resolveReleaseSet(on, prov.Capabilities().Platforms, true)
	if err != nil {
		return err
	}
	if trace && len(releases) > 1 {
		return usagef("--trace follows one build; name one release with --on")
	}
	if err := lifecycle.CancelStale(ctx, rs, repo, branch, tip); err != nil {
		return err
	}

	rel, err := branchPortdir(ctx, repo, branch, tip)
	if err != nil {
		return err
	}
	portName, err := branchPortName(ctx, rs, repo, target, branch, tip, rel)
	if err != nil {
		return err
	}

	var deferred int
	for _, r := range releases {
		started, err := submitRelease(ctx, rs, repo, branch, tip, rel, portName, r, test)
		var vde *lifecycle.VerifyDeferredError
		if errors.As(err, &vde) {
			// No slot for this platform right now: recorded, reported,
			// and the remaining releases still get their chance.
			deferred++
			continue
		}
		if err != nil {
			return err
		}
		if trace && started {
			if err := followStarted(ctx, rs, repo, tip, portName, r.Name, prov); err != nil {
				return err
			}
		}
	}
	if deferred > 0 {
		return &lifecycle.VerifyDeferredError{Branch: branch,
			// Deferrals have different remedies — a full machine frees on
			// its own, a missing capability needs provisioning — and the
			// per-release lines above name each one. A field run caught
			// this summary promising "when a slot frees" to a deferral
			// no freed slot would ever help.
			Reason: fmt.Sprintf("%d release(s) deferred — each line above names its remedy; `dockhand status` retries them as remedies are met", deferred)}
	}
	return nil
}

// submitRelease claims one release's run for the branch and submits it,
// under the repository's submit lock from the re-read through the
// record — the claim pumpRun makes, made the same way, so a verify and
// a status over one deferred run cannot both start it. The note is
// read under the lock, because what it says outside is what a peer
// may already have changed: a run already running is left alone, and
// a deferral is re-recorded with its reason before the lock goes, so
// the record can never land on top of a peer's start. started reports
// a submit that went through, for --trace to follow once the claim is
// released.
func submitRelease(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch, tip, rel, portName string, r platform.Release, test bool) (started bool, err error) {
	unlock, err := repo.LockSubmit(ctx, submitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention — a peer's pump booting a guest — is
		// not a hung process, so the lock's own advice would mislead.
		return false, fmt.Errorf("%w: a verification is being submitted in this repository; `dockhand status` shows what it started, then `dockhand verify %s` again", lockfile.ErrHeld, branch)
	}
	if err != nil {
		return false, err
	}
	defer unlock()
	if n, nerr := ledger.Open(repo).Read(ctx, tip); nerr == nil {
		if run, ok := n.Runs[r.Name]; ok && run.State == record.Running {
			fmt.Fprintf(rs.Err, "already verifying on %s (%s); `dockhand status` follows it\n",
				r.Name, time.Since(run.Job.Started).Round(time.Second))
			return false, nil
		}
	}
	err = lifecycle.SubmitVerification(ctx, rs, &lifecycle.Minted{
		Repo: repo, Branch: branch, Sha: tip, RelPort: rel,
	}, portName, r, false, test)
	var vde *lifecycle.VerifyDeferredError
	if errors.As(err, &vde) {
		if rerr := lifecycle.RecordRun(ctx, rs, repo, tip, portName, r.Name, record.Run{
			State: record.Deferred, Detail: vde.Reason,
		}, fmt.Sprintf("deferred %s: %s", r.Name, vde.Reason)); rerr != nil {
			return false, rerr
		}
		return false, err
	}
	return err == nil, err
}

// followStarted streams the run submitRelease just started — after the
// claim is released, because a build's forty minutes must hold no lock
// a peer's status pump waits on. The job is read back from the note
// the submit recorded; a submit the pre-flight settled without a build
// (known_fail, recorded unsupported) leaves nothing running to follow.
func followStarted(ctx context.Context, rs *runstate.Context, repo *git.Repo, tip, portName, plat string, prov verify.Verifier) error {
	n, err := ledger.Open(repo).Read(ctx, tip)
	if err != nil {
		return err
	}
	run, ok := n.Runs[plat]
	if !ok || run.State != record.Running {
		return nil
	}
	return lifecycle.FollowRun(ctx, rs, repo, tip, portName, plat, prov, run.Job)
}

// branchPortName names what a branch verification builds. The
// portdir's base name is NOT the answer — devel/pcre's branch may
// change pcre2, and building the parent verifies nothing about the
// change (field-caught: the VM built the untouched pcre 8.45 and
// would have called the pcre2 branch verified). Resolution, most
// direct authority first: the port the user themselves named (a
// target that matched the branch as dockhand/<target>-*, the mint's
// own naming); the tip note's recorded port (written from the plan's
// subport at bump time); and for a hand-made branch with neither, the
// context the branch's own diff moves under evaluation.
func branchPortName(ctx context.Context, rs *runstate.Context, repo *git.Repo, target, branch, tip, rel string) (string, error) {
	if target != branch {
		return target, nil
	}
	if n, err := ledger.Open(repo).Read(ctx, tip); err == nil && n.Port != "" {
		return n.Port, nil
	}
	return lifecycle.ChangedPort(ctx, rs, repo, tip, rel)
}

// branchPortdir derives the one portdir a branch changes against its
// merge base with the primary branch — from git alone, so a human
// commit's changes count the same as a minted one's.
func branchPortdir(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return "", err
	}
	paths, err := repo.DiffNames(ctx, base, tip)
	if err != nil {
		return "", err
	}
	portdirs := map[string]bool{}
	for _, p := range paths {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) >= 3 {
			portdirs[parts[0]+"/"+parts[1]] = true
		}
	}
	if len(portdirs) != 1 {
		return "", fmt.Errorf("verify: %s changes %d portdirs against %s; one at a time for now", branch, len(portdirs), git.Abbrev(base))
	}
	for d := range portdirs {
		return d, nil
	}
	return "", nil // unreachable
}

// Verify builds the verify subcommand.
func Verify() *cobra.Command {
	var on []string
	var test, trace bool
	c := &cobra.Command{
		Use:   "verify <branch|port|subport|portdir>",
		Short: "Verify a branch's tip in a pristine VM — or a port as it sits",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return verifyAction{target: args[0], on: on, test: test, trace: trace}, nil
		}),
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to verify on, or "all" (default: the newest provisioned base)`)
	c.Flags().BoolVar(&test, "test", false,
		"also run the port's test suite (`port test`) after the install")
	c.Flags().BoolVar(&trace, "trace", false,
		"stay attached after submitting: stream the build log until it finishes")
	return c
}
