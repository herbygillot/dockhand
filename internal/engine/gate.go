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

// Proof is what a synchronous verification proved: the job it ran in,
// what lint said in its log, and the provider's own phrase for what a
// pass in that environment is worth.
//
// All three, rather than the lint summary alone, because the pre-mint
// gate carries them onto the branch's record: the job is the
// environment the verdict was earned in, already handed back, and the
// evidence is the claim that belongs to it. A record naming no
// environment at all would say a verdict had been reached nowhere, and
// a gate-verified tip is supposed to read exactly like a
// background-verified one.
type Proof struct {
	Job      verify.Job
	Lint     string
	Evidence string
}

// VerifyPlan proves a plan's port builds before anything real is
// written. The edited port is rematerialized from the plan itself —
// read the Portfile, hold it to the plan's precondition hash, apply the
// edits, shadow the result — so the port under test is exactly what
// apply would write.
func (e *Engine) VerifyPlan(ctx context.Context, p *plan.Plan, o Policy) (Proof, error) {
	src, err := os.ReadFile(filepath.Join(p.Portdir, macports.PortfileName))
	if err != nil {
		return Proof{}, err
	}
	edited, err := p.Materialize(src)
	if errors.Is(err, plan.ErrDrift) {
		return Proof{}, fmt.Errorf("%w: %s", plan.ErrDrift, p.Portdir)
	}
	if err != nil {
		return Proof{}, err
	}
	root, err := e.Temp()
	if err != nil {
		return Proof{}, err
	}
	// Shadow needs no evaluator: it is a copy, and the guest does the
	// evaluating from here on.
	h := port.New(tree.Target{Portdir: p.Portdir}, nil).WithTempDir(root)
	shadow, cleanup, err := h.Shadow(edited)
	if err != nil {
		return Proof{}, err
	}
	defer cleanup()
	// The plan's whole files go into the shadow too, over the copies
	// Shadow took from the tree. The gate's verdict is carried onto the
	// minted commit by content identity — markVerified records the tip
	// as passed on the strength of the gate having built the same bytes
	// — and a shadow still carrying the old patch would earn that
	// verdict for a port the branch does not contain.
	for _, f := range p.Files {
		if err := os.WriteFile(filepath.Join(shadow.Target.Portdir, filepath.FromSlash(f.Path)), []byte(f.Content), 0o644); err != nil {
			return Proof{}, err
		}
	}

	// The gate builds what the branch will carry, so it builds it under
	// the same terms — which is why it takes the whole policy and not
	// three fields of it: a change that leaves the port's archive
	// matching bytes it replaced is verified from source or verified
	// against the archive it was supposed to be testing, and the run's
	// own --recheck is part of that answer.
	return e.RunVerification(ctx, p.Port, shadow.Target.Portdir, o.On, o.Test, o.fromSource(p.Intent))
}

// RunVerification submits one portdir to the VM provider and reports
// the verdict. Both verification modes arrive here: a plan's shadowed
// portdir, and a portdir as it sits in the tree.
func (e *Engine) RunVerification(ctx context.Context, portName, portdir string, release platform.Release, test, source bool) (Proof, error) {
	prov, err := e.Verifier(ctx)
	if err != nil {
		return Proof{}, err
	}
	var ignore []string
	if source {
		ignore = []string{portName}
	}
	job, err := prov.Submit(ctx, verify.Request{
		Ports:      []string{portName},
		Portdirs:   []string{portdir},
		Platform:   release,
		Owner:      e.TreeRoot,
		Test:       test,
		FromSource: ignore,
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
		return Proof{}, err
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
		return Proof{}, err
	}
	switch st.State {
	case verify.Passed:
		fmt.Fprintln(e.Err, "passed")
		return Proof{Job: job, Lint: verdict.LintSummary(log), Evidence: caps.Evidence}, prov.Release(ctx, job)
	case verify.Failed:
		fmt.Fprintln(e.Err, "FAILED")
		tail := log
		if len(tail) > render.FailureTail {
			tail = tail[len(tail)-render.FailureTail:]
		}
		fmt.Fprintln(e.Err, tail)
		// The environment is kept on purpose: it is the debug handle.
		return Proof{}, &VerifyFailedError{Port: portName, Handle: st.Handle}
	case verify.Errored:
		_ = prov.Release(context.WithoutCancel(ctx), job)
		// The environment could not answer. It reported the machine's
		// band until now, which is the same confusion the background
		// follow had: a crashed guest agent is not a missing base, and
		// telling the user to provision one wastes the evening.
		return Proof{}, &verdict.ErroredError{Port: portName, Platform: on.Name, Detail: st.Detail}
	case verify.Running:
	}
	return Proof{}, fmt.Errorf("verify: job ended in state %s", st.State)
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
