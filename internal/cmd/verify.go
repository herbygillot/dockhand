package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
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
// edits, shadow the result — so a saved plan and a just-produced one
// verify identically, and the port under test is exactly what apply
// would write.
func verifyPlan(ctx context.Context, rc *RunContext, p *plan.Plan, release platform.Release) error {
	src, err := os.ReadFile(filepath.Join(p.Portdir, macports.PortfileName))
	if err != nil {
		return err
	}
	if plan.FileSHA256(src) != p.PortfileSHA256 {
		return fmt.Errorf("%w: %s", plan.ErrDrift, p.Portdir)
	}
	edited, err := plan.ApplyEdits(src, p.Edits)
	if err != nil {
		return err
	}
	root, err := rc.TempDir()
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

	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	portName := p.Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(p.Portdir))
	}
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{shadow.Target.Portdir},
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
	fmt.Fprintf(rc.Err, "verifying %s on %s… ", portName, on)
	st, err := verify.Await(ctx, prov, job, 3*time.Second)
	if err != nil {
		fmt.Fprintln(rc.Err)
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return err
	}
	switch st.State {
	case verify.Passed:
		fmt.Fprintln(rc.Err, "passed")
		return prov.Release(ctx, job)
	case verify.Failed:
		fmt.Fprintln(rc.Err, "FAILED")
		tail := st.Log
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		fmt.Fprintln(rc.Err, tail)
		// The environment is kept on purpose: it is the debug handle.
		return &VerifyFailedError{Port: portName, Handle: st.Handle}
	case verify.Errored:
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return fmt.Errorf("%w: %s", verify.ErrNoEnvironment, st.Detail)
	case verify.Running:
	}
	return fmt.Errorf("verify: job ended in state %s", st.State)
}

// Verify builds the verify subcommand: prove a saved plan's port builds,
// in a pristine environment, without writing anything.
func Verify(rc *RunContext) *cobra.Command {
	var on string
	c := &cobra.Command{
		Use:   "verify <plan.json|->",
		Short: "Build a plan's port in a pristine VM, changing nothing",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := cmd.InOrStdin()
			if args[0] != "-" {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close() //nolint:errcheck // read-path close
				r = f
			}
			p, err := plan.Decode(r)
			if err != nil {
				return err
			}
			release, err := releaseFlag(on)
			if err != nil {
				return err
			}
			return verifyPlan(cmd.Context(), rc, p, release)
		},
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
