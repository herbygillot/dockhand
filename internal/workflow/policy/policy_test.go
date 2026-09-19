package policy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/stretchr/testify/require"
)

// reader answers the few questions policy asks from records held in memory.
// Anything else it is asked panics, which names the question a test forgot.
type reader struct {
	state.Reader
	jobs       map[record.JobID]record.Job
	attempts   map[record.AttemptID]record.Attempt
	revisions  map[record.RevisionID]record.Revision
	changes    map[record.ChangeID]record.Change
	pulls      map[record.PullRequestID]record.PullRequest
	plans      map[record.JobID]record.VerificationPlan
	candidates func(state.VerificationQuery) []record.Attempt
}

func (r reader) Job(_ context.Context, id record.JobID) (record.Job, error) {
	if job, ok := r.jobs[id]; ok {
		return job, nil
	}
	return record.Job{}, state.ErrNotFound
}
func (r reader) Attempt(_ context.Context, id record.AttemptID) (record.Attempt, error) {
	if attempt, ok := r.attempts[id]; ok {
		return attempt, nil
	}
	return record.Attempt{}, state.ErrNotFound
}
func (r reader) Revision(_ context.Context, id record.RevisionID) (record.Revision, error) {
	if revision, ok := r.revisions[id]; ok {
		return revision, nil
	}
	return record.Revision{}, state.ErrNotFound
}
func (r reader) Change(_ context.Context, id record.ChangeID) (record.Change, error) {
	if change, ok := r.changes[id]; ok {
		return change, nil
	}
	return record.Change{}, state.ErrNotFound
}
func (r reader) PullRequest(_ context.Context, id record.PullRequestID) (record.PullRequest, error) {
	if pr, ok := r.pulls[id]; ok {
		return pr, nil
	}
	return record.PullRequest{}, state.ErrNotFound
}
func (r reader) Plan(_ context.Context, id record.JobID) (record.VerificationPlan, error) {
	if plan, ok := r.plans[id]; ok {
		return plan, nil
	}
	return record.VerificationPlan{}, state.ErrNotFound
}
func (r reader) AttemptsForJob(_ context.Context, id record.JobID) ([]record.Attempt, error) {
	var found []record.Attempt
	for _, attempt := range r.attempts {
		if attempt.JobID == id {
			found = append(found, attempt)
		}
	}
	return found, nil
}
func (r reader) VerificationCandidates(_ context.Context, query state.VerificationQuery) ([]record.Attempt, error) {
	if r.candidates == nil {
		return nil, nil
	}
	return r.candidates(query), nil
}

var (
	tree     = record.ObjectID(strings.Repeat("a", 40))
	commit   = record.ObjectID(strings.Repeat("b", 40))
	target   = record.Target{Name: "jq", Portfile: "sysutils/jq/Portfile"}
	platform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	config   = record.BuildConfig{Provider: "tart", Platform: platform, EnvironmentDigest: "sha256:env", VerifierDigest: "fixture:v1", Tests: record.TestDeclared}
	observed = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
)

func build() record.BuildSpec {
	return record.BuildSpec{Branch: "candidate", Source: record.Source{Tree: tree, Commit: commit, Base: commit}, Target: target, Config: config}
}

func passing(id record.AttemptID, job record.JobID) record.Attempt {
	return record.Attempt{ID: id, JobID: job, State: record.AttemptFinished, Spec: build(), Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: observed}}
}

func failing(id record.AttemptID, job record.JobID) record.Attempt {
	attempt := passing(id, job)
	attempt.Evidence.Verdict = record.VerdictFailed
	return attempt
}

func TestSelectVerificationExplainsEveryOutcome(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, detail, err := SelectVerification(ctx, reader{}, record.Job{Spec: record.JobSpec{FreshVerification: true}}, build())
	require.NoError(t, err)
	require.Equal(t, "Fresh verification requested", detail)

	byTree := func(attempts ...record.Attempt) reader {
		return reader{candidates: func(q state.VerificationQuery) []record.Attempt {
			if q.Tree == "" {
				return attempts[:min(len(attempts), q.Limit)]
			}
			var matching []record.Attempt
			for _, attempt := range attempts {
				if attempt.Spec.Source.Tree == q.Tree {
					matching = append(matching, attempt)
				}
			}
			return matching
		}}
	}
	selected, detail, err := SelectVerification(ctx, byTree(passing("attempt_1", "job_1")), record.Job{}, build())
	require.NoError(t, err)
	require.Equal(t, record.AttemptID("attempt_1"), selected.ID)
	require.Equal(t, "Reused passing verification from attempt attempt_1 (job job_1), observed 2026-09-19T10:00:00Z", detail)

	selected, detail, err = SelectVerification(ctx, byTree(failing("attempt_2", "job_2"), passing("attempt_1", "job_1")), record.Job{}, build())
	require.NoError(t, err)
	require.Empty(t, selected.ID, "the newest matching result decides; an older pass does not outrank a newer failure")
	require.Equal(t, "Latest matching attempt attempt_2 is not passing; running a new build", detail)

	other := passing("attempt_3", "job_3")
	other.Spec.Source.Tree = record.ObjectID(strings.Repeat("c", 40))
	selected, detail, err = SelectVerification(ctx, byTree(other), record.Job{}, build())
	require.NoError(t, err)
	require.Empty(t, selected.ID)
	require.Equal(t, "No applicable result among the latest 32 terminal attempts for this tree and target; compared with attempt_3: source tree differs", detail)

	differentPolicy := passing("attempt_4", "job_4")
	differentPolicy.Spec.Config.Tests = record.TestRequired
	selected, detail, err = SelectVerification(ctx, byTree(differentPolicy), record.Job{}, build())
	require.NoError(t, err)
	require.Empty(t, selected.ID, "a matching tree with different inputs is skipped, not reused")
	require.Contains(t, detail, "test policy differs")

	_, detail, err = SelectVerification(ctx, byTree(), record.Job{}, build())
	require.NoError(t, err)
	require.Equal(t, "No recorded terminal verification for this target; running a new build", detail)
}

func TestPublicationCoverageNeedsAPlanOnlyBeyondOneTarget(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := passing("attempt_1", "job_1")
	owner := record.Job{ID: "job_1", Spec: record.JobSpec{Targets: []record.Target{target}}}
	r := reader{jobs: map[record.JobID]record.Job{"job_1": owner}, attempts: map[record.AttemptID]record.Attempt{"attempt_1": root}}
	require.NoError(t, PublicationCoverage(ctx, r, root), "one target, no scope: the root proves everything")

	single := &record.ReleaseScope{Affected: []record.ReleaseMember{{Target: target, Before: record.ReleaseState{Version: "1.7"}, After: record.ReleaseState{Version: "1.8"}}}}
	require.NoError(t, PublicationCoverage(ctx, r, root, single), "a scope naming only the root target needs no plan")

	sibling := record.Target{Name: "jq-devel", Portfile: "sysutils/jq/Portfile", Subport: "jq-devel"}
	shared := &record.ReleaseScope{Affected: []record.ReleaseMember{{Target: target, Before: record.ReleaseState{Version: "1.7"}, After: record.ReleaseState{Version: "1.8"}}, {Target: sibling, Before: record.ReleaseState{Version: "1.7"}, After: record.ReleaseState{Version: "1.8"}}}}
	require.NoError(t, PublicationCoverage(ctx, r, root, shared), "by default a shared release is proven by its initiating target")
	all := owner
	all.Spec.AllSubports = true
	r.jobs["job_1"] = all
	err := PublicationCoverage(ctx, r, root, shared)
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, "required coverage plan is missing")
	r.jobs["job_1"] = owner

	dependents := owner
	dependents.Spec.IncludeDependents = true
	r.jobs["job_1"] = dependents
	err = PublicationCoverage(ctx, r, root)
	require.ErrorIs(t, err, publish.ErrPrecondition, "dependents always need the recorded plan")
}

func TestValidatePublicationActionBindsTheActionToItsJob(t *testing.T) {
	t.Parallel()
	spec := record.PublicationSpec{LocalBranch: "candidate", HeadBranch: "candidate", Forge: "github", Repository: "macports/macports-ports", HeadRepository: "author/ports", BaseBranch: "master", PushURL: "u", BaseURL: "b", LockDirectory: "/locks", EvidenceAttempt: "attempt_1", Desired: record.PublicationContent{Head: commit, Title: "jq: update"}}
	standalone := record.Job{ID: "job_1", ChangeID: "change_1", Phase: record.PhasePublication, Spec: record.JobSpec{Action: record.Publish, InputRevision: "revision_1", Publication: &spec}}
	action := record.PublicationAction{JobID: "job_1", ChangeID: "change_1", RevisionID: "revision_1", Spec: spec}
	require.NoError(t, ValidatePublicationAction(standalone, action))

	altered := action
	altered.Spec.BaseBranch = "elsewhere"
	err := ValidatePublicationAction(standalone, altered)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorContains(t, err, "does not match the accepted publication intent")

	wrongPhase := standalone
	wrongPhase.Phase = record.PhaseVerification
	require.ErrorContains(t, ValidatePublicationAction(wrongPhase, action), "does not belong to the job's publication phase")

	destination := spec.Destination()
	combined := record.Job{ID: "job_2", ChangeID: "change_2", Phase: record.PhasePublication, ResultRevision: "revision_2",
		Prepared: &record.PreparedChange{Branch: "candidate", Source: record.Source{Tree: tree, Commit: commit}},
		Spec:     record.JobSpec{Action: record.BumpRevision, Destination: record.Published, PublishTo: &destination}}
	combinedAction := record.PublicationAction{JobID: "job_2", ChangeID: "change_2", RevisionID: "revision_2", Spec: spec}
	require.NoError(t, ValidatePublicationAction(combined, combinedAction))
	moved := combined
	moved.Prepared = &record.PreparedChange{Branch: "candidate", Source: record.Source{Tree: tree, Commit: record.ObjectID(strings.Repeat("d", 40))}}
	require.ErrorContains(t, ValidatePublicationAction(moved, combinedAction), "does not match the accepted prepared destination")
}

func TestPublicationEvidenceChecksTheCitedAttemptOrItsAbsence(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	change := record.Change{ID: "change_1", Branch: "candidate", Targets: []record.Target{target}, Disposition: record.ChangeOpen}
	cited := passing("attempt_1", "job_0")
	owner := record.Job{ID: "job_0", Spec: record.JobSpec{Targets: []record.Target{target}}}
	revision := record.Revision{ID: "revision_1", ChangeID: "change_1", Source: record.Source{Tree: tree, Commit: commit, Base: commit}}
	job := record.Job{ID: "job_1", ChangeID: "change_1", Phase: record.PhasePublication,
		Spec: record.JobSpec{Action: record.Publish, InputRevision: "revision_1", Source: revision.Source, Targets: []record.Target{target}, Build: &config, Verification: record.VerificationRequired}}
	r := reader{
		jobs: map[record.JobID]record.Job{"job_0": owner}, attempts: map[record.AttemptID]record.Attempt{"attempt_1": cited},
		revisions: map[record.RevisionID]record.Revision{"revision_1": revision}, changes: map[record.ChangeID]record.Change{"change_1": change},
		candidates: func(state.VerificationQuery) []record.Attempt { return []record.Attempt{cited} },
	}
	require.NoError(t, PublicationEvidence(ctx, r, job, record.PublicationSpec{EvidenceAttempt: "attempt_1"}))

	newer := failing("attempt_2", "job_9")
	r.candidates = func(state.VerificationQuery) []record.Attempt { return []record.Attempt{newer, cited} }
	err := PublicationEvidence(ctx, r, job, record.PublicationSpec{EvidenceAttempt: "attempt_1"})
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, "no longer applicable")

	otherTarget := change
	otherTarget.Targets = []record.Target{{Name: "deno", Portfile: "devel/deno/Portfile"}}
	r.changes["change_1"] = otherTarget
	err = PublicationEvidence(ctx, r, job, record.PublicationSpec{EvidenceAttempt: "attempt_1"})
	require.ErrorContains(t, err, "must cover the tracked contribution target")
	r.changes["change_1"] = change

	unverified := job
	unverified.Spec.Build, unverified.Spec.Verification = nil, record.VerificationSkipped
	require.NoError(t, PublicationEvidence(ctx, r, unverified, record.PublicationSpec{Unverified: true}), "an explicitly unverified publication cites nothing")
	err = PublicationEvidence(ctx, r, job, record.PublicationSpec{Unverified: true})
	require.True(t, errors.Is(err, ErrInvalidRequest), "%v", err)
	require.ErrorContains(t, err, "explicitly skipped verification")

	wrongPhase := job
	wrongPhase.Phase = record.PhaseVerification
	require.ErrorContains(t, PublicationEvidence(ctx, r, wrongPhase, record.PublicationSpec{EvidenceAttempt: "attempt_1"}), "requires publication phase")
}
