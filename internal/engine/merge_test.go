package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A pull request's merge fitness is in its status: its ports' maintainers
// as their change records keep them, who its description mentions, who
// approved it, and when MacPorts' maintainer timeout ends, 72 hours from
// its opening, by the engine's clock; a maintainer's approval ends the
// wait. The reading is the one status already makes of it.
func TestAPullRequestsMergeFitnessIsInItsStatus(t *testing.T) {
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
	opened := at.Add(-31 * time.Hour)
	fake.PRs[number].CreatedAt, fake.PRs[number].Author, fake.PRs[number].Mentions = opened, "herbygillot", []string{"ryandesign"}

	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, statuses[0].Merge, "nothing read of the pull request yet")

	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, statuses[0].Merge)
	require.Equal(t, []string{"jq"}, statuses[0].MaintainersUnread, "no change record keeps its maintainers")

	tree := model.ObjectID(strings.TrimSpace(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		return tx.RecordChange(model.ChangeRecord{Branch: branch.ID, Tree: tree, Base: branch.Base, Directory: "textproc/jq", Policy: fidelity.ChangePolicy, At: at,
			Ports: []model.SubportChange{{Port: "jq", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "version", From: "1.7.1", To: "1.8.1"}}, Maintainers: "{@ryandesign ryandesign} openmaintainer"}}})
	}))
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	merge := statuses[0].Merge
	require.Empty(t, statuses[0].MaintainersUnread)
	require.Equal(t, []string{"jq"}, merge.Waiting())
	require.True(t, merge.Ports[0].OpenMaintainer)
	require.Equal(t, opened, merge.Opened, "the timeout counts from the opening")
	require.Equal(t, opened.Add(72*time.Hour), merge.Until)
	require.Equal(t, 41*time.Hour, merge.Left, "by the engine's clock")
	require.Equal(t, []string{"ryandesign"}, merge.Notified)

	later := opened.Add(73 * time.Hour)
	e.options.Now = func() time.Time { return later }
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.True(t, statuses[0].Merge.Passed())

	fake.Statuses = map[int]forge.PullRequestStatus{number: {Review: "approved", Approvals: 1, ApprovedBy: []string{"ryandesign"}}}
	e.options.Now = func() time.Time { return at }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	merge = statuses[0].Merge
	require.Equal(t, []string{"ryandesign"}, merge.Approvers)
	require.Equal(t, []string{"@ryandesign"}, merge.Ports[0].ApprovedBy)
	require.Empty(t, merge.Waiting(), "its maintainer's approval ends the wait")
	require.True(t, merge.Until.IsZero())
}
