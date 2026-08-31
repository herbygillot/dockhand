package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
func submitVerification(ctx context.Context, rs *runstate.Context, m *minted, portName string, release platform.Release) error {
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
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{filepath.Join(stage, filepath.FromSlash(m.RelPort))},
		Platform: release,
	})
	if err != nil {
		// A full provider (two-slot cap) or a mid-submit failure: the
		// branch is minted and the tip is simply unverified.
		return later(err.Error())
	}
	tree, err := m.Repo.RevParse(ctx, m.Sha+"^{tree}")
	if err != nil {
		return err
	}
	n := verifyNote{
		Schema: noteSchema, Sha: m.Sha, Tree: tree, Port: portName,
		Platform: release.Name, State: "running", Job: job,
	}
	if err := writeNote(ctx, m.Repo, n); err != nil {
		return err
	}
	fmt.Fprintf(rs.Err, "verify: submitted %s (job %s); `dockhand status` follows it\n", portName, job.ID)
	return nil
}

// realizeOpts is one invocation's choice of realization, shared by
// every intent that writes: print the plan, print the diff, edit in
// place, or — the default — mint the branch and submit verification.
type realizeOpts struct {
	planOnly bool
	diff     bool
	inPlace  bool
	noVerify bool
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
		return markVerified(ctx, rs, m, p, o.on)
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
	return submitVerification(ctx, rs, m, p.Port, release)
}

// markVerified writes the minted commit's note as passed, on the
// strength of the pre-mint gate having built identical content.
func markVerified(ctx context.Context, rs *runstate.Context, m *minted, p *plan.Plan, on string) error {
	release, err := releaseFlag(on)
	if err != nil {
		return err
	}
	tree, err := m.Repo.RevParse(ctx, m.Sha+"^{tree}")
	if err != nil {
		return err
	}
	n := verifyNote{
		Schema: noteSchema, Sha: m.Sha, Tree: tree, Port: p.Port,
		Platform: release.Name, State: "passed",
	}
	if err := writeNote(ctx, m.Repo, n); err != nil {
		return err
	}
	fmt.Fprintln(rs.Err, "verified before minting; the tip is recorded as passed")
	return nil
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
