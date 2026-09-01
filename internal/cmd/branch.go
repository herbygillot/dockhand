package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
)

// planOnBase resolves a plan against the repository it will land in:
// the repo, its primary branch, the Portfile's repo-relative path, and
// the edited bytes — computed from the base commit's blob, never the
// working file, with the plan's precondition hash held against that
// blob. Both realizations that speak git — mint and diff — start here.
func planOnBase(ctx context.Context, p *plan.Plan) (repo *git.Repo, primary, path string, edited []byte, err error) {
	repo, err = git.Open(ctx, p.Portdir)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			// Wrapped, not swallowed: the identity is what routes this
			// to the tree exit band.
			return nil, "", "", nil, fmt.Errorf("%w — the branch workflow needs a git checkout; --in-place edits the tree directly", err)
		}
		return nil, "", "", nil, err
	}
	primary, err = repo.PrimaryBranch(ctx)
	if err != nil {
		return nil, "", "", nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, "", "", nil, err
	}
	path = rel + "/" + macports.PortfileName
	base, err := repo.BlobAt(ctx, primary, path)
	if err != nil {
		return nil, "", "", nil, err
	}
	if edit.FileSHA256(base) != p.PortfileSHA256 {
		return nil, "", "", nil, fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, primary)
	}
	edited, err = edit.Apply(base, p.Edits)
	if err != nil {
		return nil, "", "", nil, err
	}
	return repo, primary, path, edited, nil
}

// BranchInFlightError is the refusal an intent gives when its port
// already has a branch: refusal is a feature (exit 5), not a failure —
// the user asked for one thing, and the reason they did not get it is
// a judgment with a remedy, not something broken.
type BranchInFlightError struct {
	Branch string
}

func (e *BranchInFlightError) Error() string {
	return fmt.Sprintf("a change for this port is already in flight: %s — discard it or pick up where it left off", e.Branch)
}

// minted is what a realized branch hands back: enough for the caller
// to submit verification against the sha and tell the user where the
// change lives.
type minted struct {
	Repo    *git.Repo
	Branch  string
	Sha     string // full commit sha
	RelPort string // repo-relative portdir path
}

// mintFromPlan realizes a plan as a branch (D21): the edited Portfile
// is committed onto the tree's primary branch at its local position,
// under dockhand's namespace, entirely in the object database — the
// user's HEAD and working tree are never touched. A plan with no edits
// mints nothing and returns nil, nil.
func mintFromPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan) (*minted, error) {
	branch, message := "dockhand/"+p.Slug, p.Summary
	if len(p.Edits) == 0 {
		// A no-op realized as a branch would be an empty commit.
		fmt.Fprintln(rs.Out, "no edits; no branch minted")
		return nil, nil
	}
	repo, primary, path, edited, err := planOnBase(ctx, p)
	if err != nil {
		return nil, err
	}
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch:  branch,
		Base:    primary,
		Path:    path,
		Content: edited,
		Message: message,
	})
	if err != nil {
		if errors.Is(err, git.ErrBranchExists) {
			return nil, &BranchInFlightError{Branch: branch}
		}
		return nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(rs.Out, "branch: %s (%s)\n", branch, sha[:12])
	fmt.Fprintf(rs.Err, "your checkout is untouched — `git checkout %s` to add changes\n", branch)
	return &minted{Repo: repo, Branch: branch, Sha: sha, RelPort: rel}, nil
}

// VerifyDeferredError reports a verification that could not start —
// no bases, full slots, a mid-submit failure — after its branch was
// successfully minted. The branch stands (the git commit/push shape:
// nobody deletes the commit because the push failed), but the
// invocation's contract was mint AND submit, so the exit is nonzero:
// exit 3, because the obstacle is the machine's. --no-verify narrows
// the contract to mint alone.
type VerifyDeferredError struct {
	Branch string
	Reason string
}

func (e *VerifyDeferredError) Error() string {
	return fmt.Sprintf("verification not started: %s\nthe branch stands — run `dockhand verify %s` when ready", e.Reason, e.Branch)
}

// submitVerification stages the minted commit's portdir out of the
// object database — the working tree is irrelevant to what the branch
// carries — submits it to the VM provider, and records the running job
// as the commit's note. Submission not starting is not a minting
// failure — the branch stands — but it is a contract failure:
// VerifyDeferredError carries that split.
func submitVerification(ctx context.Context, rs *runstate.Context, m *minted, portName string, release platform.Release, trace, test bool) error {
	if !tartPresent() {
		// No provider, no contract: the machine cannot verify at all,
		// so this is a --no-verify bump that says so — and the branch
		// may be promoted as it is, unverified.
		fmt.Fprintln(rs.Err, "no verification possible: no local verify provider (tart)")
		fmt.Fprintf(rs.Err, "the branch is unverified; you may promote it as is, or install tart and run `dockhand verify %s`\n", m.Branch)
		return nil
	}
	later := func(why string) error {
		return &VerifyDeferredError{Branch: m.Branch, Reason: why}
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		if errors.Is(err, verify.ErrNoEnvironment) {
			return later(err.Error())
		}
		return err
	}
	// The platform resolves before anything is recorded: a run is keyed
	// by release name, and "the default" is not a key.
	if release.IsZero() {
		release = prov.Capabilities().Platforms[0]
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	stage, _, err := root.MakeDir("stage-" + portName)
	if err != nil {
		return err
	}
	if err := m.Repo.Materialize(ctx, m.Sha, m.RelPort, stage); err != nil {
		return err
	}
	staged := filepath.Join(stage, filepath.FromSlash(m.RelPort))
	// mpbb's list-time exclusion, borrowed: evaluation answers
	// known_fail in a second, before any VM boots — and it answers for
	// the branch's content, which is what was materialized.
	if declares, kerr := knownFailOn(ctx, rs, staged, release); kerr != nil {
		fmt.Fprintf(rs.Err, "warning: known_fail pre-flight: %v\n", kerr)
	} else if declares {
		return recordRun(ctx, rs, m.Repo, m.Sha, portName, release.Name, verifyRun{
			State: "unsupported", Detail: "declares known_fail on " + release.Name,
		}, fmt.Sprintf("%s declares known_fail on %s; recorded unsupported — no build attempted", portName, release.Name))
	}
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{staged},
		Platform: release,
		Test:     test,
	})
	if err != nil {
		// A full provider (two-slot cap) or a mid-submit failure: the
		// branch is minted and the tip is simply unverified.
		return later(err.Error())
	}
	if err := recordRun(ctx, rs, m.Repo, m.Sha, portName, release.Name, verifyRun{
		State: "running", Job: job, Tested: test,
	}, fmt.Sprintf("verify: submitted %s on %s (job %s); `dockhand status` follows it", portName, release.Name, job.ID)); err != nil {
		return err
	}
	if trace {
		return followRun(ctx, rs, m.Repo, m.Sha, portName, release.Name, prov, job)
	}
	return nil
}

// recordRun writes one platform's run into the commit's note — the
// read-modify-write every per-platform update goes through — and tells
// the user what was recorded.
func recordRun(ctx context.Context, rs *runstate.Context, repo *git.Repo, sha, portName, releaseName string, r verifyRun, msg string) error {
	n, err := loadOrStartNote(ctx, repo, sha, portName)
	if err != nil {
		return err
	}
	n.Runs[releaseName] = r
	if err := writeNote(ctx, repo, n); err != nil {
		return err
	}
	fmt.Fprintln(rs.Err, msg)
	return nil
}

// followRun streams a running build's log as the guest writes it,
// then settles the run through the same machinery status uses — the
// --trace contract: don't exit, watch. Ctrl-C detaches; the build
// continues without us, which is the submit-and-poll design keeping
// its promise even while we happen to be watching.
func followRun(ctx context.Context, rs *runstate.Context, repo *git.Repo, sha, portName, plat string, prov verify.Verifier, job verify.Job) error {
	fmt.Fprintf(rs.Err, "following %s on %s — Ctrl-C detaches, the build continues\n", portName, plat)
	printed := 0
	for {
		st, err := prov.Poll(ctx, job)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(rs.Err, "detached; `dockhand status` follows it from here")
				return nil
			}
			return err
		}
		if log, lerr := prov.Log(ctx, job); lerr == nil && len(log) > printed {
			fmt.Fprint(rs.Out, log[printed:])
			printed = len(log)
		}
		if st.State.Terminal() {
			break
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(rs.Err, "detached; `dockhand status` follows it from here")
			return nil
		case <-time.After(4 * time.Second):
		}
	}
	n, err := loadOrStartNote(ctx, repo, sha, portName)
	if err != nil {
		return err
	}
	if err := settleRuns(ctx, repo, &n); err != nil {
		return err
	}
	switch r := n.Runs[plat]; r.State {
	case "passed":
		fmt.Fprintf(rs.Err, "passed on %s; worker released\n", plat)
		return nil
	case "unsupported":
		fmt.Fprintf(rs.Err, "%s declines %s: %s\n", portName, plat, r.Detail)
		return nil
	case "failed":
		return &VerifyFailedError{Port: portName, Handle: r.Handle}
	default:
		return fmt.Errorf("%w: %s", verify.ErrNoEnvironment, r.Detail)
	}
}

// realizeOpts is one invocation's choice of realization, shared by
// every intent that writes: print the plan, print the diff, edit in
// place, or — the default — mint the branch and submit verification.
type realizeOpts struct {
	planOnly bool
	diff     bool
	inPlace  bool
	noVerify bool
	trace    bool
	test     bool
	on       string
	// verified says the synchronous --verify gate already ran and
	// passed on this plan's content, so realization records the verdict
	// instead of buying the same build twice.
	verified bool
}

// realizePlan carries a plan to its chosen realization. Every write
// intent arrives here, so a plan becomes a branch the same way
// whichever intent produced it (D21).
func realizePlan(ctx context.Context, rs *runstate.Context, p *plan.Plan, o realizeOpts) error {
	if o.planOnly {
		return p.Encode(rs.Out)
	}
	if o.diff {
		return diffFromPlan(ctx, rs, p)
	}
	if o.inPlace {
		// The deliberate opt-out (D21): edit where the user stands,
		// uncommitted — for the user running their own workflow, and
		// the only write mode a non-git tree has.
		return applyPlan(ctx, rs, p)
	}
	m, err := mintFromPlan(ctx, rs, p)
	if err != nil || m == nil {
		return err
	}
	if o.verified {
		// The --verify gate built exactly these bytes — the minted blob
		// and the gate's shadow are both edit.Apply over the same base —
		// so the verdict transfers to the commit by content identity.
		// Recording it beats resubmitting: the same build twice proves
		// nothing the first one did not.
		return markVerified(ctx, rs, m, p, o.on, o.test)
	}
	if o.noVerify {
		return nil
	}
	// The branch is live the moment it exists (D21): verification is
	// submitted against the tip and the guest drives its own build, so
	// this process is free to exit; status collects the verdict.
	release, err := releaseFlag(o.on)
	if err != nil {
		return err
	}
	return submitVerification(ctx, rs, m, p.Port, release, o.trace, o.test)
}

// markVerified writes the minted commit's note as passed, on the
// strength of the pre-mint gate having built identical content.
func markVerified(ctx context.Context, rs *runstate.Context, m *minted, p *plan.Plan, on string, tested bool) error {
	release, err := releaseFlag(on)
	if err != nil {
		return err
	}
	if release.IsZero() {
		// The gate ran, so a provider exists; its default names the run.
		prov, perr := vmProvider(ctx)
		if perr != nil {
			return perr
		}
		release = prov.Capabilities().Platforms[0]
	}
	return recordRun(ctx, rs, m.Repo, m.Sha, p.Port, release.Name, verifyRun{State: "passed", Tested: tested},
		fmt.Sprintf("verified before minting; the tip is recorded as passed on %s", release.Name))
}

// applyPlan carries out a plan against the working tree — the
// --in-place realization. Every intent arrives through realizePlan, so
// a plan is executed the same way whichever produced it.
func applyPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan) error {
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Apply(ctx, ev); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rs.Out, "applied: %s %s (%d edits, delta as predicted)\n",
		p.Intent, p.Portdir, len(p.Edits))
	return err
}

// diffFromPlan renders a plan as the patch its branch would carry,
// writing nothing the workspace can see: the edited blob is grafted
// into the base tree exactly as a mint would, and the two trees are
// diffed instead of committed. Repo-relative a/ and b/ paths come out
// correct because the trees carry the full structure.
func diffFromPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan) error {
	if len(p.Edits) == 0 {
		fmt.Fprintln(rs.Err, "no edits; nothing to diff")
		return nil
	}
	repo, primary, path, edited, err := planOnBase(ctx, p)
	if err != nil {
		return err
	}
	tree, err := repo.GraftTree(ctx, primary, path, edited)
	if err != nil {
		return err
	}
	patch, err := repo.DiffTrees(ctx, primary+"^{tree}", tree)
	if err != nil {
		return err
	}
	// Page the way git diff would: through the user's own pager
	// (GIT_PAGER, core.pager, PAGER — git's chain), and only when
	// stdout is a terminal. diff-tree itself runs behind a pipe, so
	// the pager decision is dockhand's to make at its own stdout.
	if f, ok := rs.Out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			if pager := repo.Pager(ctx); pager != "" && pager != "cat" {
				return git.RunPager(ctx, pager, patch, rs.Out, rs.Err)
			}
		}
	}
	_, err = rs.Out.Write(patch)
	return err
}
