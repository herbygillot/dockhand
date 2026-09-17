package publish

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestSharedReleaseSummaryNamesWhatWasNotBuiltLocally(t *testing.T) {
	t.Parallel()
	newest := record.Target{Name: "py314-requests", Portfile: "python/py-requests/Portfile", Subport: "py314-requests"}
	older := record.Target{Name: "py313-requests", Portfile: newest.Portfile, Subport: "py313-requests"}
	stub := record.Target{Name: "py-requests", Portfile: newest.Portfile}
	scope := &record.ReleaseScope{Affected: []record.ReleaseMember{{Target: stub, MetadataOnly: true}, {Target: older}, {Target: newest}}}
	plan := record.VerificationPlan{Targets: []record.VerificationTarget{{ID: "t1", Port: newest, Root: true}}}
	attempts := []record.Attempt{{TargetID: "t1", Spec: record.BuildSpec{Target: newest, Config: record.BuildConfig{Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}
	summary := SharedReleaseSummary(plan, attempts, scope)
	require.Contains(t, summary, "The initiating subport of this shared release passed verification locally.")
	require.Contains(t, summary, "- py314-requests: passed")
	require.Contains(t, summary, "Not built locally: py313-requests. The pull request workflow builds every subport.")
	require.NotContains(t, summary, "py-requests", "a metadata-only stub is not something to build")
	plan.Targets = append(plan.Targets, record.VerificationTarget{ID: "t2", Port: older})
	attempts = append(attempts, record.Attempt{TargetID: "t2", Spec: record.BuildSpec{Target: older, Config: record.BuildConfig{Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed}})
	summary = SharedReleaseSummary(plan, attempts, scope)
	require.Contains(t, summary, "All buildable subports in this shared release passed verification.")
	require.NotContains(t, summary, "Not built locally")
}
