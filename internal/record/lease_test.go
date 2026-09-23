package record

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseEligibilityAndRelease(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	var lease Lease
	require.True(t, lease.Eligible(now), "no claim and no retry is eligible")
	later := now.Add(time.Minute)
	lease.RetryAt = &later
	require.False(t, lease.Eligible(now), "a future retry defers work")
	require.True(t, lease.Eligible(later), "a due retry is eligible")
	lease.Claim = &Claim{Owner: "driver", Generation: 3, ExpiresAt: now.Add(time.Hour)}
	require.False(t, lease.Eligible(later), "a live claim excludes other drivers")
	require.True(t, lease.Eligible(now.Add(2*time.Hour)), "an expired claim no longer excludes")
	lease.ClaimGeneration = 3
	lease.ConsecutiveFailures = 2
	lease.Release()
	require.Nil(t, lease.Claim)
	require.Nil(t, lease.RetryAt)
	require.Equal(t, uint64(3), lease.ClaimGeneration, "the generation outlives the claim")
	require.Equal(t, uint32(2), lease.ConsecutiveFailures, "failures are reset only by expected progress")
}

func TestEffectiveSourceAndPreparingActions(t *testing.T) {
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
