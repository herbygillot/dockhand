package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/edit"
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

// vmProvider assembles the tart provider from the base images actually
// on this machine. No bases is ErrNoEnvironment with the remedy named.
func vmProvider(ctx context.Context) (tart.Provider, error) {
	releases, err := (provision.Tart{}).Provisioned(ctx)
	if err != nil {
		return tart.Provider{}, err
	}
	if len(releases) == 0 {
		return tart.Provider{}, fmt.Errorf(
			"%w: no base images; run `dockhand provision tart --macos <release>` first",
			verify.ErrNoEnvironment)
	}
	bases := make([]tart.Base, 0, len(releases))
	for _, r := range releases {
		bases = append(bases, tart.Base{VM: tart.BaseName(r), Release: r})
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

	portName := p.Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(p.Portdir))
	}
	return runVerification(ctx, rs, portName, shadow.Target.Portdir, release)
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
		tail := st.Log
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		fmt.Fprintln(rs.Err, tail)
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
	on     string
}

var _ Action = verifyAction{}

func (a verifyAction) Execute(ctx context.Context, rs *runstate.Context) error {
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
	release, err := releaseFlag(a.on)
	if err != nil {
		return err
	}
	return runVerification(ctx, rs, portName, targets[0].Portdir, release)
}

// Verify builds the verify subcommand.
func Verify() *cobra.Command {
	var on string
	c := &cobra.Command{
		Use:   "verify <port|subport|portdir>",
		Short: "Build a port as it sits, in a pristine VM, changing nothing",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return verifyAction{target: args[0], on: on}, nil
		}),
	}
	c.Flags().StringVar(&on, "on", "", "macOS release to verify on (name or version; default: the first provisioned base)")
	return c
}

// releaseFlag parses --on, the empty flag meaning the provider default.
func releaseFlag(on string) (platform.Release, error) {
	if on == "" {
		return platform.Release{}, nil
	}
	r, err := platform.Parse(on)
	if err != nil {
		return platform.Release{}, &UsageError{Err: err}
	}
	return r, nil
}
