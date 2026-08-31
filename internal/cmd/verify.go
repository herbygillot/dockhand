package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// VerifyFailedError reports a verification that ran to completion and
// found the port does not build. It is its own type because it is its
// own kind of outcome — not the tool failing, not the machine, not the
// invocation — and the exit table gives it its own code.
type VerifyFailedError struct {
	Port   string
	Handle string
}

func (e *VerifyFailedError) Error() string {
	msg := fmt.Sprintf("verification failed: %s does not build", e.Port)
	if e.Handle != "" {
		msg += fmt.Sprintf(" (environment kept: %s)", e.Handle)
	}
	return msg
}

// tartPresent reports whether the local verify provider exists at all.
// Its absence is a different fact from "present but unprovisioned": a
// machine without tart cannot verify, so verification quietly leaves
// the contract (bump warns and proceeds; promote warns and allows),
// where a machine with tart and no bases is asked to provision.
func tartPresent() bool {
	_, err := exec.LookPath("tart")
	return err == nil
}

// vmProvider assembles the tart provider from the base images actually
// on this machine. Both ways of having no environment — tart absent,
// tart present with no bases — are ErrNoEnvironment with the remedy
// named, which is what routes a bump to "the branch stands" rather
// than a raw exec error.
func vmProvider(ctx context.Context) (tart.Provider, error) {
	if _, err := exec.LookPath("tart"); err != nil {
		return tart.Provider{}, fmt.Errorf(
			"%w: tart is not installed (`port install tart`); --no-verify skips verification",
			verify.ErrNoEnvironment)
	}
	releases, err := (provision.Tart{}).Provisioned(ctx)
	if err != nil {
		return tart.Provider{}, err
	}
	if len(releases) == 0 {
		return tart.Provider{}, fmt.Errorf(
			"%w: no base images; run `dockhand provision tart --macos <release>` first",
			verify.ErrNoEnvironment)
	}
	// Newest first: the provider's default is its first base, and the
	// default a quick bump wants is the current OS — the mundane-build
	// check — not the oldest. Platform-floor archaeology asks for old
	// releases by name.
	bases := make([]tart.Base, 0, len(releases))
	for i := len(releases) - 1; i >= 0; i-- {
		bases = append(bases, tart.Base{VM: tart.BaseName(releases[i]), Release: releases[i]})
	}
	return tart.Provider{Bases: bases}, nil
}

// verifyPlan proves a plan's port builds before anything real is
// written. The edited port is rematerialized from the plan itself —
// read the Portfile, hold it to the plan's precondition hash, apply the
// edits, shadow the result — so the port under test is exactly what
// apply would write.
func verifyPlan(ctx context.Context, rs *runstate.Context, p *plan.Plan, release platform.Release) error {
	src, err := os.ReadFile(filepath.Join(p.Portdir, macports.PortfileName))
	if err != nil {
		return err
	}
	if edit.FileSHA256(src) != p.PortfileSHA256 {
		return fmt.Errorf("%w: %s", plan.ErrDrift, p.Portdir)
	}
	edited, err := edit.Apply(src, p.Edits)
	if err != nil {
		return err
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	// Shadow needs no evaluator: it is a copy, and the guest does the
	// evaluating from here on.
	h := port.New(tree.Target{Portdir: p.Portdir}, nil).WithTempDir(root)
	shadow, cleanup, err := h.Shadow(edited)
	if err != nil {
		return err
	}
	defer cleanup()

	return runVerification(ctx, rs, p.Port, shadow.Target.Portdir, release)
}

// runVerification submits one portdir to the VM provider and reports
// the verdict. Both verification modes arrive here: a plan's shadowed
// portdir, and a portdir as it sits in the tree.
func runVerification(ctx context.Context, rs *runstate.Context, portName, portdir string, release platform.Release) error {
	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{portdir},
		Platform: release,
	})
	if err != nil {
		return err
	}
	caps := prov.Capabilities()
	on := release
	if on.IsZero() && len(caps.Platforms) > 0 {
		on = caps.Platforms[0]
	}
	fmt.Fprintf(rs.Err, "verifying %s on %s… ", portName, on)
	st, err := verify.Await(ctx, prov, job, 3*time.Second)
	if err != nil {
		fmt.Fprintln(rs.Err)
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return err
	}
	switch st.State {
	case verify.Passed:
		fmt.Fprintln(rs.Err, "passed")
		return prov.Release(ctx, job)
	case verify.Failed:
		fmt.Fprintln(rs.Err, "FAILED")
		if log, lerr := prov.Log(ctx, job); lerr == nil {
			tail := log
			if len(tail) > 2000 {
				tail = tail[len(tail)-2000:]
			}
			fmt.Fprintln(rs.Err, tail)
		}
		// The environment is kept on purpose: it is the debug handle.
		return &VerifyFailedError{Port: portName, Handle: st.Handle}
	case verify.Errored:
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return fmt.Errorf("%w: %s", verify.ErrNoEnvironment, st.Detail)
	case verify.Running:
	}
	return fmt.Errorf("verify: job ended in state %s", st.State)
}

// verifyAction proves a port builds as it sits, in a pristine
// environment, without writing anything. This is state verification —
// it tests the portdir's current content, whoever produced it, which
// is what makes human edits after a dockhand change verifiable at all.
type verifyAction struct {
	target string
	on     []string
}

var _ Action = verifyAction{}

func (a verifyAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// A branch name wins over a port name: the branch is the unit
	// (D21), and verifying one means verifying its tip sha, whoever
	// made it. Everything else falls through to state verification of
	// the working tree.
	if repo, err := rs.Repo(ctx); err == nil && repo.HasBranch(ctx, a.target) {
		return verifyBranch(ctx, rs, repo, a.target, a.on)
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
	targets, err := resolveTargets(rs.TreeRoot, false, []string{a.target})
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
	return runVerification(ctx, rs, portName, targets[0].Portdir, release)
}

// verifyBranch submits a branch's tip for verification in the
// background, exactly as the mint that created it would have: the
// changed portdir is derived from git — diff against the merge base
// with the primary branch, so a human commit's changes count too — and
// materialized from the object database. A job already running for the
// tip is left alone; a running job the branch has moved past is
// canceled first, its worker released and its note marked superseded,
// because a verdict about an abandoned sha is a slot spent on nothing.
func verifyBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string, on []string) error {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	releases, err := verifyReleases(on, prov.Capabilities().Platforms)
	if err != nil {
		return err
	}
	if err := cancelStale(ctx, rs, repo, branch, tip); err != nil {
		return err
	}

	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return err
	}
	paths, err := repo.DiffNames(ctx, base, tip)
	if err != nil {
		return err
	}
	portdirs := map[string]bool{}
	for _, p := range paths {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) >= 3 {
			portdirs[parts[0]+"/"+parts[1]] = true
		}
	}
	if len(portdirs) != 1 {
		return fmt.Errorf("verify: %s changes %d portdirs against %s; one at a time for now", branch, len(portdirs), base[:12])
	}
	var rel string
	for d := range portdirs {
		rel = d
	}

	n, nerr := readNote(ctx, repo, tip)
	var deferred int
	for _, r := range releases {
		if nerr == nil {
			if run, ok := n.Runs[r.Name]; ok && run.State == "running" {
				fmt.Fprintf(rs.Err, "already verifying on %s (%s); `dockhand status` follows it\n",
					r.Name, time.Since(run.Job.Started).Round(time.Second))
				continue
			}
		}
		err := submitVerification(ctx, rs, &minted{
			Repo: repo, Branch: branch, Sha: tip, RelPort: rel,
		}, filepath.Base(rel), r)
		var vde *VerifyDeferredError
		if errors.As(err, &vde) {
			// No slot for this platform right now: recorded, reported,
			// and the remaining releases still get their chance.
			if rerr := recordRun(ctx, rs, repo, tip, filepath.Base(rel), r.Name, verifyRun{
				State: "deferred", Detail: vde.Reason,
			}, fmt.Sprintf("deferred %s: %s", r.Name, vde.Reason)); rerr != nil {
				return rerr
			}
			deferred++
			continue
		}
		if err != nil {
			return err
		}
	}
	if deferred > 0 {
		return &VerifyDeferredError{Branch: branch,
			Reason: fmt.Sprintf("%d release(s) deferred; re-run `dockhand verify %s --on <release>` when a slot frees", deferred, branch)}
	}
	return nil
}

// verifyReleases resolves verify's --on values against the provisioned
// bases: nothing means the newest, "all" means every base, and each
// named release must actually have a base — a verdict cannot be
// promised on an environment that does not exist.
func verifyReleases(on []string, provisioned []platform.Release) ([]platform.Release, error) {
	if len(provisioned) == 0 {
		return nil, fmt.Errorf("%w: no base images", verify.ErrNoEnvironment)
	}
	if len(on) == 0 {
		return provisioned[:1], nil
	}
	var out []platform.Release
	for _, v := range on {
		if strings.EqualFold(v, "all") {
			return provisioned, nil
		}
		r, err := platform.Parse(v)
		if err != nil {
			return nil, &UsageError{Err: err}
		}
		found := false
		for _, p := range provisioned {
			if p == r {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: no base image for %s; `dockhand provision tart --macos %s` builds one",
				verify.ErrNoEnvironment, r.Name, strings.ToLower(r.CompactName()))
		}
		out = append(out, r)
	}
	return out, nil
}

// cancelStale releases every running job recorded on a commit the
// branch once pointed at but no longer does — reachable ancestors and
// amended-away shas alike — and marks their notes superseded by the
// tip about to be submitted.
func cancelStale(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch, tip string) error {
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return err
	}
	for _, sha := range noted {
		if sha == tip {
			continue
		}
		n, err := readNote(ctx, repo, sha)
		if err != nil || !n.anyState("running") || !repo.IsAncestor(ctx, sha, branch) {
			continue
		}
		prov, err := vmProvider(ctx)
		if err != nil {
			return err
		}
		changed := false
		for plat, run := range n.Runs {
			if run.State != "running" {
				continue
			}
			if err := prov.Release(ctx, run.Job); err != nil {
				fmt.Fprintf(rs.Err, "warning: canceling %s: %v\n", run.Job.ID, err)
			}
			run.State, run.Detail = "superseded", "canceled: the branch moved to "+tip[:12]
			n.Runs[plat], changed = run, true
			fmt.Fprintf(rs.Err, "canceled stale verification of %s on %s (branch moved past it)\n", sha[:12], plat)
		}
		if changed {
			if err := writeNote(ctx, repo, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// Verify builds the verify subcommand.
func Verify() *cobra.Command {
	var on []string
	c := &cobra.Command{
		Use:   "verify <branch|port|subport|portdir>",
		Short: "Verify a branch's tip in a pristine VM — or a port as it sits",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return verifyAction{target: args[0], on: on}, nil
		}),
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to verify on, or "all" (default: the newest provisioned base)`)
	return c
}

// releaseFlag parses --on for the verbs that verify: one release, the
// empty flag meaning the provider default (the newest provisioned
// base). A matrix is refused with directions — the verdict note tracks
// one job per commit, so breadth comes from repeated runs or from
// exec, whose probes are built for it.
func releaseFlag(on string) (platform.Release, error) {
	if on == "" {
		return platform.Release{}, nil
	}
	if strings.EqualFold(on, "all") || strings.Contains(on, ",") {
		return platform.Release{}, usagef("this verb submits one platform; run the matrix afterwards with `dockhand verify <branch> --on <list|all>`")
	}
	r, err := platform.Parse(on)
	if err != nil {
		return platform.Release{}, &UsageError{Err: err}
	}
	return r, nil
}
