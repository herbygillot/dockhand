package record

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEffectiveSourcePreparingAndUpdatingActions(t *testing.T) {
	input := Source{Commit: ObjectID("a"), Tree: ObjectID("b")}
	job := Job{Spec: JobSpec{InputRevision: "revision_in", Source: input}}
	revision, source := job.EffectiveSource()
	require.Equal(t, RevisionID("revision_in"), revision)
	require.Equal(t, input, source)
	prepared := Source{Commit: ObjectID("c"), Tree: ObjectID("d")}
	job.ResultRevision, job.Prepared = "revision_out", &PreparedChange{Source: prepared}
	revision, source = job.EffectiveSource()
	require.Equal(t, RevisionID("revision_out"), revision)
	require.Equal(t, prepared, source)
	for _, action := range []Action{Bump, BumpRevision, RefreshChecksums, Amend, Rebase} {
		require.True(t, action.Prepares(), string(action))
	}
	for _, action := range []Action{Verify, Publish} {
		require.False(t, action.Prepares(), string(action))
	}
	for _, action := range []Action{Bump, BumpRevision, RefreshChecksums} {
		require.True(t, action.Updates(), string(action))
	}
	for _, action := range []Action{Amend, Rebase, Verify, Publish} {
		require.False(t, action.Updates(), string(action))
	}
}

func TestJobPhaseNextFollowsTheDestination(t *testing.T) {
	type step struct {
		from JobPhase
		spec JobSpec
		next JobPhase
		ok   bool
	}
	verified := JobSpec{Destination: Published, Verification: VerificationRequired}
	unverified := JobSpec{Destination: Published, Verification: VerificationSkipped}
	branch := JobSpec{Destination: BranchReady, Verification: VerificationSkipped}
	evidence := JobSpec{Destination: VerificationComplete, Verification: VerificationRequired}
	for _, s := range []step{
		{PhasePreparation, verified, PhaseVerification, true},
		{PhasePreparation, evidence, PhaseVerification, true},
		{PhasePreparation, unverified, PhasePublication, true},
		{PhasePreparation, branch, "", false},
		{PhaseVerification, verified, PhasePublication, true},
		{PhaseVerification, evidence, "", false},
		{PhasePublication, verified, "", false},
		{"", verified, "", false},
	} {
		next, ok := s.from.Next(s.spec)
		require.Equal(t, s.ok, ok, "%s to %s", s.from, s.spec.Destination)
		require.Equal(t, s.next, next, "%s to %s", s.from, s.spec.Destination)
	}
	for _, phase := range []JobPhase{PhasePreparation, PhaseVerification, PhasePublication} {
		require.True(t, phase.Valid(), string(phase))
	}
	require.False(t, JobPhase("").Valid())
	require.False(t, JobPhase("cleanup").Valid())
}

func TestValidCancelIsTheSharedShape(t *testing.T) {
	valid := ControlRequest{ID: "control_1", Kind: Cancel, Reason: "done"}
	require.True(t, valid.ValidCancel())
	applied := time.Now()
	for name, request := range map[string]ControlRequest{
		"no id":       {Kind: Cancel},
		"other kind":  {ID: "control_1", Kind: "pause"},
		"applied":     {ID: "control_1", Kind: Cancel, AppliedAt: &applied},
		"binary text": {ID: "control_1", Kind: Cancel, Reason: "\xff"},
	} {
		require.False(t, request.ValidCancel(), name)
	}
}
