package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// garageLog is the end of garage's failed macos-15 job's log, as the
// forge gives it (field testing, 2026-10-03).
const garageLog = `DEBUG: Fetching https://mirror.sjtu.edu.cn/macports/distfiles/garage/jni-sys-macros-0.4.1.crate failed: The requested URL returned error: 403
Error: Failed to fetch jni-sys-macros-0.4.1.crate: The requested URL returned error: 403
Error: Failed to fetch garage: Failed to fetch distfiles
`

// A pull request whose CI fails has each failed job's log read once, and
// classified: a fetch a mirror refused is said as one, and its failed jobs
// run again through the person's login; a log GitHub no longer keeps is
// unclassified, once. Without write access, running them again is refused
// with what moves it on (garage #35174, field testing, 2026-10-03).
func TestAFailedCIJobIsClassifiedOnceAndRunAgain(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number

	fake.Statuses = map[int]forge.PullRequestStatus{number: {Review: "none", Checks: forge.CheckSummary{Total: 3, Passed: 1, Failed: 2, Failing: []string{"macos-14", "macos-15"}}}}
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	head := string(statuses[0].Branch.PullRequest.Observed.Head)
	require.NotEmpty(t, head)

	fake.Failed = map[string][]forge.CIJob{head: {{ID: 11, Run: 7, Attempt: 1, Name: "macos-15"}, {ID: 12, Run: 7, Attempt: 1, Name: "macos-14"}}}
	fake.Logs = map[int64]string{11: garageLog}
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	ci := statuses[0].CI
	require.Len(t, ci, 2)
	require.Equal(t, model.CIUnclassified, ci[0].Cause, "macos-14's log was gone")
	require.Equal(t, "macos-15", ci[1].Check)
	require.Equal(t, model.CIFetch, ci[1].Cause)
	require.Equal(t, "fetching jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn answered 403) on macos-15", CIFailureWords(ci[1]))
	require.Equal(t, 2, fake.LogsRead)

	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, fake.LogsRead, "a job's attempt is read once, its log gone or not")

	runs, err := e.RerunCI(t.Context(), statuses[0].Branch)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, runs)
	require.Equal(t, []int64{7}, fake.Rerun)

	fake.RerunErr = fmt.Errorf("%w: 403", forge.ErrRejected)
	_, err = e.RerunCI(t.Context(), statuses[0].Branch)
	require.Equal(t, refusal.Auth, refusal.KindOf(err))
	require.EqualError(t, err, fmt.Sprintf("running #%d's failed CI again needs write access to macports/macports-ports, which a maintainer has", number))
	require.Equal(t, []refusal.Next{{External: fmt.Sprintf("ask a maintainer to run it again, or push to #%d, which runs it anew", number)}}, refusal.Of(err).Next)

	fake.Failed = nil
	_, err = e.RerunCI(t.Context(), statuses[0].Branch)
	require.ErrorContains(t, err, fmt.Sprintf("nothing of #%d's CI failed at its head", number))
}
