package workflow_test

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/require"
)

type secondProvider struct {
	verify.Provider
	f     *fixture
	calls map[string]int
}

func (p *secondProvider) Capabilities(context.Context) (verify.Capabilities, error) {
	p.calls["capabilities"]++
	return verify.Capabilities{Name: "second"}, nil
}
func (p *secondProvider) Submit(_ context.Context, r verify.Request) (verify.Submission, error) {
	p.calls["submit"]++
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: "second", RequestID: r.ID, RunID: "second-run"}, Resources: []record.ResourceHandle{{Provider: "second", ID: "second-resource"}}}, nil
}
func (p *secondProvider) Observe(_ context.Context, r record.ProviderRun) (verify.Observation, error) {
	p.calls["observe"]++
	return verify.Observation{Run: r, State: record.AttemptFinished, Verdict: record.VerdictPassed, ObservedAt: p.f.now()}, nil
}
func (p *secondProvider) Release(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
	p.calls["release"]++
	return verify.ReleaseResult{Confirmed: true}, nil
}

func TestCycleRoutesRecordedProvidersAndResourceCleanup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.provider.observe = terminal(f, record.VerdictPassed)
	other := &secondProvider{f: f, calls: map[string]int{}}
	f.engine.Providers = map[string]verify.Provider{"scripted": f.provider, "second": other}
	first := f.submit(t, "first")
	request := f.request("second")
	request.Spec.Build.Provider = "second"
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	f.run(t, first, receipt.JobID)
	f.run(t, first, receipt.JobID)
	f.run(t, first, receipt.JobID)
	require.Equal(t, 1, other.calls["submit"])
	require.Equal(t, 1, other.calls["observe"])
	require.Equal(t, 1, other.calls["release"])
	require.Equal(t, 1, f.provider.count("submit"))
	require.Equal(t, 1, f.provider.count("observe"))
}
