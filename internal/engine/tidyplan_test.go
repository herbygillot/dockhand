package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// threeChanges makes a branch with a PortGroup edit not yet committed,
// Ada's libharbor commit, and Bo's jq commit.
func threeChanges(t *testing.T) (*Engine, TidyPlan) {
	t.Helper()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "harbor", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	write(t, dir, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "libharbor: update to 3")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# harbor 3\n"})
	commitAs(t, dir, "Bo bo@example.org", "jq: build against libharbor 3")
	write(t, dir, map[string]string{"_resources/port1.0/group/github-1.0.tcl": "# group, for harbor\n"})
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 3)
	require.Equal(t, "", plan.Groups[0].Directory, "the PortGroup comes first")
	require.Contains(t, plan.Groups[0].Blocking[0], "needs a subject")
	return e, plan
}

func TestRegroupCombinesAndReorders(t *testing.T) {
	e, plan := threeChanges(t)

	for spec, problem := range map[string]string{
		"1 2":       "commit 3 is left out",
		"1 1+2 3":   "commit 1 is named twice",
		"1 2 4":     `"4" is not one of the commits, 1 to 3`,
		"":          "name the commits in order",
		"1+x 2 3":   `"x" is not one of the commits`,
		"1+2 3 bad": `"bad" is not one`,
	} {
		_, err := plan.Regroup(spec, "")
		require.ErrorContains(t, err, problem, spec)
	}

	regrouped, err := plan.Regroup("3, 1+2", "")
	require.NoError(t, err)
	require.Len(t, regrouped.Groups, 2)
	require.Equal(t, "jq: build against libharbor 3", regrouped.Groups[0].Subject())
	combined := regrouped.Groups[1]
	require.Equal(t, "libharbor: update to 3", combined.Subject(), "the first commit with a subject gives it")
	require.Equal(t, []string{"_resources/port1.0/group/github-1.0.tcl", "devel/libharbor/Portfile"}, combined.Paths)
	require.Equal(t, "Ada", combined.Author.Name)
	require.True(t, combined.Working)
	require.Empty(t, regrouped.Blocking())
	require.False(t, regrouped.Unambiguous(), "a combined commit is for a person to review")
	require.Len(t, plan.Groups, 3, "the original plan is unchanged")

	both, err := plan.Regroup("1 2+3", "")
	require.NoError(t, err)
	require.Contains(t, both.Blocking()[1], "combines commits by Ada <ada@example.org> and Bo <bo@example.org>; choose the attribution with --author")
	both, err = plan.Regroup("1 2+3", "Ada <ada@example.org>")
	require.NoError(t, err)
	require.Equal(t, "Ada", both.Groups[1].Author.Name)

	result, err := e.ApplyTidy(t.Context(), regrouped)
	require.NoError(t, err)
	require.Len(t, result.Commits, 2)
	require.Equal(t, []string{"jq: build against libharbor 3", "libharbor: update to 3"}, log(t, plan.Worktree, plan.Branch.Base))
	require.Empty(t, run(t, plan.Worktree, "status", "--porcelain"))
}

func TestASavedPlanAppliesUntilTheBranchMoves(t *testing.T) {
	e, plan := threeChanges(t)
	regrouped, err := plan.Regroup("1+2 3", "")
	require.NoError(t, err)
	data, err := regrouped.Save()
	require.NoError(t, err)
	require.Contains(t, string(data), `"version": 1`)
	require.Contains(t, string(data), `"working_tree": "`+plan.Final+`"`)

	var saved savedTidyPlan
	require.NoError(t, json.Unmarshal(data, &saved))
	saved.Commits[0].Message = "libharbor: update to 3, with the github PortGroup it needs\n"
	edited, err := json.Marshal(saved)
	require.NoError(t, err)

	loaded, err := e.LoadTidyPlan(t.Context(), edited)
	require.NoError(t, err)
	require.Equal(t, "libharbor: update to 3, with the github PortGroup it needs", loaded.Groups[0].Subject())
	require.Equal(t, []string{"libharbor"}, loaded.Groups[0].Ports)
	require.Len(t, loaded.Groups[1].Combines, 1)
	require.Empty(t, loaded.Blocking())

	short := saved
	short.Commits = short.Commits[:1]
	partial, err := json.Marshal(short)
	require.NoError(t, err)
	_, err = e.LoadTidyPlan(t.Context(), partial)
	require.ErrorContains(t, err, "don't cover exactly the branch's changes: missing textproc/jq/Portfile")

	_, err = e.LoadTidyPlan(t.Context(), []byte(`{"version": 1, "extra": true}`))
	require.ErrorContains(t, err, "not a saved tidy plan")
	_, err = e.LoadTidyPlan(t.Context(), []byte(`{"version": 2}`))
	require.ErrorContains(t, err, "version 2; this dockhand reads version 1")

	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "edited after the plan\n"})
	_, err = e.LoadTidyPlan(t.Context(), edited)
	require.ErrorIs(t, err, ErrStalePlan)
	require.ErrorContains(t, err, "its files were edited")
	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# harbor 3\n"})

	loaded, err = e.LoadTidyPlan(t.Context(), edited)
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), loaded)
	require.NoError(t, err)
	require.Equal(t, []string{"libharbor: update to 3, with the github PortGroup it needs", "jq: build against libharbor 3"}, log(t, plan.Worktree, plan.Branch.Base))
	_, err = e.LoadTidyPlan(t.Context(), edited)
	require.ErrorContains(t, err, "it has new commits")
}
