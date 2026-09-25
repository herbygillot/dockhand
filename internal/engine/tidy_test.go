package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

func commitAs(t *testing.T, dir, author, message string) {
	t.Helper()
	name, email, _ := strings.Cut(author, " ")
	run(t, dir, "-c", "user.name="+name, "-c", "user.email="+email, "commit", "-q", "-am", message)
}

func log(t *testing.T, dir string, base model.ObjectID) []string {
	t.Helper()
	out := run(t, dir, "log", "--reverse", "--format=%s", string(base)+"..HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestTidyCommitsAnUpdateUnambiguouslyAndRestoreUndoesIt(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.RefreshChecksums, Port: "jq"})
	require.NoError(t, err)

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.False(t, plan.Keep)
	require.Len(t, plan.Groups, 1)
	group := plan.Groups[0]
	require.True(t, group.FromEdits)
	require.True(t, group.Working)
	require.Equal(t, "jq: update to 1.8.1", group.Subject(), "the update's subject, not the checksum refresh's")
	require.Contains(t, group.Message, "\n\nGenerated-By: Dockhand ")
	require.True(t, plan.Unambiguous())
	require.Empty(t, plan.Findings)

	result, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, "tidy-1", result.Checkpoint.Name())
	require.Equal(t, []string{"jq: update to 1.8.1"}, log(t, branch.Worktree, branch.Base))
	require.Empty(t, run(t, branch.Worktree, "status", "--porcelain"), "the files are the commit, and the index agrees")
	require.Equal(t, "name jq\nversion 1.8.1\nchecksums sha256 0000\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "refs/dockhand/checkpoints/tidy-1"))

	again, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, again.Keep, "a tidy branch stays as it is")

	_, _, err = e.Restore(t.Context(), "tidy-1")
	require.NoError(t, err)
	require.Empty(t, log(t, branch.Worktree, branch.Base))
	require.Equal(t, "M textproc/jq/Portfile", run(t, branch.Worktree, "status", "--porcelain"), "the edit reads as uncommitted again")
	_, _, err = e.Restore(t.Context(), "tidy-1")
	require.ErrorContains(t, err, "already restored")
}

func TestTidySquashesCorrectionsAndKeepsTheirTrailers(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "update-jq", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	portfile := "textproc/jq/Portfile"
	write(t, dir, map[string]string{portfile: "name jq\nversion 1.8.1\nrevision 1\n"})
	commitAs(t, dir, "Ada ada@example.org", "Update jq")
	write(t, dir, map[string]string{portfile: "name jq\nversion 1.8.1\nrevision 1\nchecksums x\n"})
	commitAs(t, dir, "Ada ada@example.org", "fix checksums\n\nCloses: https://trac.macports.org/ticket/71234")
	write(t, dir, map[string]string{portfile: "name jq\nversion 1.8.1\nrevision 1\nchecksums y\n"})
	commitAs(t, dir, "Ada ada@example.org", "oops")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 1)
	group := plan.Groups[0]
	require.False(t, group.FromEdits)
	require.False(t, plan.Unambiguous(), "a person's commits are reviewed before they are rewritten")
	require.Len(t, group.Combines, 3)
	require.Equal(t, "jq: update to 1.8.1\n\nCloses: https://trac.macports.org/ticket/71234\n", group.Message, "no attribution for a person's work")
	require.Equal(t, "Ada", group.Author.Name)
	require.Len(t, plan.Findings, 1)
	require.Equal(t, "revision-after-update", plan.Findings[0].Code)

	result, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	require.Equal(t, []string{"jq: update to 1.8.1"}, log(t, dir, branch.Base))
	require.Equal(t, "Ada <ada@example.org>", run(t, dir, "log", "-1", "--format=%an <%ae>"))
}

func TestTidyKeepsAGoodHistoryAndOrdersSeveralPorts(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "two", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	write(t, dir, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "libharbor: update to 3")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# harbor 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "jq: build against libharbor 3")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, plan.Keep, "two good commits are left alone")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 2)
	require.Equal(t, "devel/libharbor", plan.Groups[0].Directory, "history's order")
	require.Equal(t, "libharbor: update to 3", plan.Groups[0].Subject())
	require.Equal(t, "textproc/jq", plan.Groups[1].Directory)
	require.False(t, plan.Groups[1].FromEdits, "the jq commit was a person's edit before dockhand's")
	require.Equal(t, "jq: build against libharbor 3", plan.Groups[1].Subject())
}

func TestTidyAsksWhatItCannotKnow(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "mixed", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# note\n"})
	commitAs(t, dir, "Ada ada@example.org", "wip")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# note 2\n"})
	commitAs(t, dir, "Bo bo@example.org", "more wip")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Blocking(), 2)
	require.Contains(t, plan.Blocking()[0], "needs a subject")
	require.Contains(t, plan.Blocking()[1], "Ada <ada@example.org> and Bo <bo@example.org>")
	_, err = e.ApplyTidy(t.Context(), plan)
	require.ErrorContains(t, err, "can't be applied yet")

	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch, Squash: true, Message: "jq: note the harbor dependency", Author: "Ada <ada@example.org>"})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking())
	write(t, dir, map[string]string{"textproc/jq/Portfile": "changed after the plan\n"})
	_, err = e.ApplyTidy(t.Context(), plan)
	require.ErrorIs(t, err, ErrStalePlan)
	run(t, dir, "checkout", "--", ".")

	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch, Squash: true, Message: "jq: note the harbor dependency", Author: "Ada <ada@example.org>"})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, []string{"jq: note the harbor dependency"}, log(t, dir, branch.Base))

	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# note 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "jq: another note")
	_, _, err = e.Restore(t.Context(), "tidy-1")
	require.ErrorContains(t, err, "has moved on since tidy-1")
	_, _, err = e.Restore(t.Context(), "tidy-9")
	require.ErrorContains(t, err, "no checkpoint tidy-9")

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		checkpoints, err := r.Checkpoints(branch.ID)
		require.NoError(t, err)
		require.Len(t, checkpoints, 1)
		return nil
	}))
}

func TestTidyRefusesAMerge(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "merged", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	run(t, dir, "switch", "-q", "-c", "side")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 2\n"})
	commitAs(t, dir, "Ada ada@example.org", "jq: update to 2")
	run(t, dir, "switch", "-q", branch.Name)
	write(t, dir, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "libharbor: update to 3")
	run(t, dir, "-c", "user.name=Ada", "-c", "user.email=ada@example.org", "merge", "-q", "--no-edit", "side")
	_, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.ErrorContains(t, err, "never flattens a merge")
}

// A tidy checkpoint keeps the index it replaced, which can hold a staged
// version neither the old head nor the working files have, and restore
// puts it back, unless something was staged since (Design v3 §8). From
// the 2026-09-25 implementation review.
func TestRestorePutsTheStagedVersionBack(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# staged version\n"})
	run(t, branch.Worktree, "add", "textproc/jq/Portfile")
	staged := run(t, branch.Worktree, "show", ":textproc/jq/Portfile")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# working version\n"})

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch, Squash: true, Message: "jq: update to 1.8.1"})
	require.NoError(t, err)
	tidied, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.NotEmpty(t, tidied.Checkpoint.Index)
	require.NotEqual(t, staged, run(t, branch.Worktree, "show", ":textproc/jq/Portfile"), "tidy leaves the index at its new head")
	require.Equal(t, string(tidied.Checkpoint.Index), run(t, branch.Worktree, "rev-parse", tidied.Checkpoint.IndexRef()+"^{tree}"), "a ref keeps it reachable")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# staged after tidy\n"})
	run(t, branch.Worktree, "add", "textproc/jq/Portfile")
	_, _, err = e.Restore(t.Context(), tidied.Checkpoint.Name())
	require.ErrorContains(t, err, "something was staged in dockhand/jq-update since "+tidied.Checkpoint.Name())
	run(t, branch.Worktree, "reset", "-q")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# working version\n"})

	_, _, err = e.Restore(t.Context(), tidied.Checkpoint.Name())
	require.NoError(t, err)
	require.Equal(t, staged, run(t, branch.Worktree, "show", ":textproc/jq/Portfile"))
	require.Equal(t, "name jq\nversion 1.8.1\n# working version\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")), "the working files are untouched")
}
