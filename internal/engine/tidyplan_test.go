package engine

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/model"
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
	require.Contains(t, string(data), "version = 2")
	require.Contains(t, string(data), `working_tree = "`+plan.Final+`"`)

	var saved savedTidyPlan
	_, err = toml.Decode(string(data), &saved)
	require.NoError(t, err)
	saved.Commits[0].Message = "libharbor: update to 3, with the github PortGroup it needs\n"
	encode := func(plan savedTidyPlan) []byte {
		var b bytes.Buffer
		require.NoError(t, toml.NewEncoder(&b).Encode(plan))
		return b.Bytes()
	}
	edited := encode(saved)

	loaded, err := e.LoadTidyPlan(t.Context(), edited)
	require.NoError(t, err)
	require.Equal(t, "libharbor: update to 3, with the github PortGroup it needs", loaded.Groups[0].Subject())
	require.NotEmpty(t, regrouped.Groups[1].Notes)
	for _, group := range loaded.Groups {
		require.Empty(t, group.Notes, "the notes said how the proposal was made; the plan as saved is what applies")
	}
	require.Equal(t, []string{"libharbor"}, loaded.Groups[0].Ports)
	require.Len(t, loaded.Groups[1].Combines, 1)
	require.Empty(t, loaded.Blocking())

	// A plan saved as JSON, before, is still read.
	legacy := saved
	legacy.Version = 1
	old, err := json.Marshal(legacy)
	require.NoError(t, err)
	loaded, err = e.LoadTidyPlan(t.Context(), old)
	require.NoError(t, err)
	require.Equal(t, "libharbor: update to 3, with the github PortGroup it needs", loaded.Groups[0].Subject())

	short := saved
	short.Commits = short.Commits[:1]
	_, err = e.LoadTidyPlan(t.Context(), encode(short))
	require.ErrorContains(t, err, "don't cover exactly the branch's changes: missing textproc/jq/Portfile")

	_, err = e.LoadTidyPlan(t.Context(), []byte(`{"version": 1, "extra": true}`))
	require.ErrorContains(t, err, "not a saved tidy plan")
	_, err = e.LoadTidyPlan(t.Context(), []byte("version = 2\nextra = true\n"))
	require.ErrorContains(t, err, "not a saved tidy plan: a plan has no extra")
	_, err = e.LoadTidyPlan(t.Context(), []byte(`{"version": 2}`))
	require.ErrorContains(t, err, "not a saved tidy plan: a JSON plan is version 1")
	_, err = e.LoadTidyPlan(t.Context(), []byte("version = 3\n"))
	require.ErrorContains(t, err, "version 3; this dockhand reads version 2")

	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "edited after the plan\n"})
	_, err = e.LoadTidyPlan(t.Context(), edited)
	require.ErrorIs(t, err, ErrStalePlan)
	require.ErrorContains(t, err, "its files were edited")
	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# harbor 3\n"})

	// Staging something the plan never saw makes it stale too, since
	// applying would reset the index.
	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "staged after the plan\n"})
	run(t, plan.Worktree, "add", "textproc/jq/Portfile")
	write(t, plan.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# harbor 3\n"})
	_, err = e.LoadTidyPlan(t.Context(), edited)
	require.ErrorIs(t, err, ErrStalePlan)
	require.ErrorContains(t, err, "something was staged since it was made")
	run(t, plan.Worktree, "read-tree", plan.Index)

	loaded, err = e.LoadTidyPlan(t.Context(), edited)
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), loaded)
	require.NoError(t, err)
	require.Equal(t, []string{"libharbor: update to 3, with the github PortGroup it needs", "jq: build against libharbor 3"}, log(t, plan.Worktree, plan.Branch.Base))
	_, err = e.LoadTidyPlan(t.Context(), edited)
	require.ErrorContains(t, err, "it has new commits")
}

// A saved plan's messages read as the commits will say them, and read back
// as they were: a multi-line literal string where TOML can hold the
// message, escaped where it can't (the hugo exercise's re-submitting
// sshuttle, finding 1).
func TestASavedPlansMessagesReadAsWritten(t *testing.T) {
	when := time.Date(2026, 9, 28, 12, 0, 0, 0, time.FixedZone("", -4*60*60))
	body := "sshuttle: update to 2.0.0\n\nBuild with Python 3.14, the python PortGroup's default.\n\nGenerated-By: Dockhand v3 (https://github.com/herbygillot/dockhand)\n"
	for message, literal := range map[string]bool{
		body:                                   true,
		"jq: it's \"quoted\" \\ and\ttabbed\n": true,
		"jq: holds '''three quotes'''\n":       false,
		"jq: rings a bell \a\n":                false,
		"jq: '''\"both\"''' and \\ \a\n":       false,
	} {
		plan := TidyPlan{Branch: model.Branch{Name: "dockhand/jq"}, Groups: []TidyGroup{{Message: message, Paths: []string{"textproc/jq/Portfile"}, Author: git.Signature{Name: "Ada", Email: "ada@example.org", When: when}}}}
		data, err := plan.Save()
		require.NoError(t, err)
		require.Equal(t, literal, bytes.Contains(data, []byte("message = '''\n"+message+"'''")), "%s", data)
		saved, err := decodeSavedPlan(data)
		require.NoError(t, err, "%s", data)
		require.Equal(t, message, string(saved.Commits[0].Message))
		require.True(t, when.Equal(saved.Commits[0].Author.When))
	}
}

// A subject taken from a commit says whose commit it was: dockhand's, by
// its attribution line, or the person's (the hugo exercise's re-submitting
// sshuttle, finding 2).
func TestASubjectSaysWhoseCommitItCameFrom(t *testing.T) {
	_, plan := threeChanges(t)
	at := slices.IndexFunc(plan.Groups, func(g TidyGroup) bool { return g.Directory == "textproc/jq" })
	require.GreaterOrEqual(t, at, 0)
	jq := plan.Groups[at]
	require.Contains(t, jq.Notes, "subject from your commit "+short(model.ObjectID(jq.Combines[0].ID)))

	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "jq: update to 1.8.1\n\n"+commitmsg.GeneratedBy())
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# built with the new oniguruma\n"})
	proposal, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, proposal.Groups, 1)
	require.Contains(t, proposal.Groups[0].Notes, "subject from dockhand's commit "+short(model.ObjectID(proposal.Groups[0].Combines[0].ID)))
}
