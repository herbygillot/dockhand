package perftest

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

// Provider performs no I/O and never probes the ledger. Finish selects whether
// observations keep a build running or complete it with successful evidence.
type Provider struct{ Finish bool }

func (p *Provider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: "perf", Platforms: []record.Platform{Platform}, Capacity: 100, Isolated: true}, nil
}
func (p *Provider) Submit(_ context.Context, r verify.Request) (verify.Submission, error) {
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: "perf", RequestID: r.ID, RunID: string(r.AttemptID)}, Resources: []record.ResourceHandle{{Provider: "perf", ID: "vm_" + string(r.AttemptID)}}}, nil
}
func (p *Provider) Reconcile(context.Context, record.RequestID) (verify.Reconciliation, error) {
	return verify.Reconciliation{}, fmt.Errorf("unexpected reconciliation in performance fixture")
}
func (p *Provider) Observe(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
	o := verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}
	if p.Finish {
		o.State, o.Verdict = record.AttemptFinished, record.VerdictPassed
		o.Steps = []record.StepResult{{Package: "fixture", Phase: "build", Verdict: record.VerdictPassed}}
	}
	return o, nil
}
func (p *Provider) Cancel(context.Context, record.ProviderRun) error { return nil }
func (p *Provider) Release(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
	return verify.ReleaseResult{Confirmed: true}, nil
}
