package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestARenamedBranchKeepsItsRecordAndItsPullRequest(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number

	run(t, branch.Worktree, "branch", "-m", "jq-with-docs")
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, status.Missing, "the old name is gone")

	adoption, err := e.Adopt(t.Context(), AdoptRequest{Branch: "jq-with-docs"})
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", adoption.Renamed)
	require.Equal(t, branch.ID, adoption.Branch.ID, "the same record")
	require.Equal(t, "jq-with-docs", adoption.Branch.Name)
	require.Equal(t, number, adoption.Branch.PullRequest.Number)
	again, err := e.Adopt(t.Context(), AdoptRequest{Branch: "jq-with-docs"})
	require.NoError(t, err)
	require.True(t, again.Already)

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\nchecksums sha256 0000\n# docs\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: add a note")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: adoption.Branch, NoCheck: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Equal(t, "ada/macports-ports:dockhand/jq-update", plan.Head(), "a pull request's head can't move, so pushes go where it was opened from")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, run(t, branch.Worktree, "rev-parse", "HEAD"), string(fake.head("dockhand/jq-update")))
	require.Empty(t, fake.head("jq-with-docs"), "no new branch on the fork")
}

func TestAnUnrelatedBranchIsNotTakenForARename(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	first, err := e.Start(t.Context(), StartRequest{Name: "one"})
	require.NoError(t, err)
	run(t, first.Worktree, "branch", "-m", "renamed")
	run(t, f.clone, "branch", "unrelated", "master")

	adoption, err := e.Adopt(t.Context(), AdoptRequest{Branch: "unrelated"})
	require.NoError(t, err)
	require.Empty(t, adoption.Renamed, "its checkout is not the missing branch's, and there was no push")
	require.NotEqual(t, first.ID, adoption.Branch.ID)

	renamed, err := e.Adopt(t.Context(), AdoptRequest{Branch: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "dockhand/one", renamed.Renamed, "the missing branch's worktree has it checked out")
	require.Equal(t, first.ID, renamed.Branch.ID)
}
