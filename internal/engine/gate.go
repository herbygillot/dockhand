package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// VerifyPlan proves a plan's port builds before anything real is
// written. The edited port is rematerialized from the plan itself —
// read the Portfile, hold it to the plan's precondition hash, apply the
// edits, shadow the result — so the port under test is exactly what
// apply would write.
func (e *Engine) VerifyPlan(ctx context.Context, p *plan.Plan, release platform.Release, test bool) (string, error) {
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
	root, err := e.Temp()
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

	lint, err := e.RunVerification(ctx, p.Port, shadow.Target.Portdir, release, test)
	if err != nil {
		return "", err
	}
	return lint, nil
}

// RunVerification submits one portdir to the VM provider and reports
// the verdict. Both verification modes arrive here: a plan's shadowed
// portdir, and a portdir as it sits in the tree. The returned lint is
// the run's evidence on a pass — the same summary a settled background
// run records — so a gate-verified tip's note says exactly what a
// background-verified one would.
func (e *Engine) RunVerification(ctx context.Context, portName, portdir string, release platform.Release, test bool) (string, error) {
	prov, err := e.Verifier(ctx)
	if err != nil {
		return "", err
	}
	job, err := prov.Submit(ctx, verify.Request{
		Port:     portName,
		Portdirs: []string{portdir},
		Platform: release,
		Owner:    e.TreeRoot,
		Test:     test,
	})
	if err != nil {
		// Someone is standing here. The provider counted slots and has
		// no idea who asked, so the fact that nothing is being queued —
		// the --verify gate and `verify <portdir>` both wait for their
		// answer and then leave — is stamped by the caller that knows
		// it. It changes the band, not the sentence.
		var full *verify.CapacityError
		if errors.As(err, &full) {
			full.Synchronous = true
		}
		return "", err
	}
	caps := prov.Capabilities()
	on := release
	if on.IsZero() && len(caps.Platforms) > 0 {
		on = caps.Platforms[0]
	}
	fmt.Fprintf(e.Err, "verifying %s on %s… ", portName, on)
	st, log, err := awaitVerdict(ctx, prov, job)
	if err != nil {
		fmt.Fprintln(e.Err)
		_ = prov.Release(context.WithoutCancel(ctx), job)
		return "", err
	}
	switch st.State {
	case verify.Passed:
		fmt.Fprintln(e.Err, "passed")
		return verdict.LintSummary(log), prov.Release(ctx, job)
	case verify.Failed:
		fmt.Fprintln(e.Err, "FAILED")
		tail := log
		if len(tail) > render.FailureTail {
			tail = tail[len(tail)-render.FailureTail:]
		}
		fmt.Fprintln(e.Err, tail)
		// The environment is kept on purpose: it is the debug handle.
		return "", &VerifyFailedError{Port: portName, Handle: st.Handle}
	case verify.Errored:
		_ = prov.Release(context.WithoutCancel(ctx), job)
		// The environment could not answer. It reported the machine's
		// band until now, which is the same confusion the background
		// follow had: a crashed guest agent is not a missing base, and
		// telling the user to provision one wastes the evening.
		return "", &verdict.ErroredError{Port: portName, Platform: on.Name, Detail: st.Detail}
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
