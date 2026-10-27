package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func commitAs(t *testing.T, dir, author, message string) {
	t.Helper()
	name, email, _ := strings.Cut(author, " ")
	testsupport.Git(t, dir, "-c", "user.name="+name, "-c", "user.email="+email, "commit", "-q", "-am", message)
}

func log(t *testing.T, dir string, base model.ObjectID) []string {
	t.Helper()
	out := testsupport.Git(t, dir, "log", "--reverse", "--format=%s", string(base)+"..HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestTidyCommitsAnUpdateUnambiguouslyAndRestoreUndoesIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: "jq"})
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
	require.Empty(t, testsupport.Git(t, branch.Worktree, "status", "--porcelain"), "the files are the commit, and the index agrees")
	require.Equal(t, modelineHead+"name jq\nversion 1.8.1\nchecksums sha256 0000\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))
	require.Equal(t, string(branch.Base), testsupport.Git(t, branch.Worktree, "rev-parse", "refs/dockhand/checkpoints/tidy-1"))

	again, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, again.Keep, "a tidy branch stays as it is")

	_, _, err = e.Restore(t.Context(), "tidy-1")
	require.NoError(t, err)
	require.Empty(t, log(t, branch.Worktree, branch.Base))
	require.Equal(t, "M textproc/jq/Portfile", testsupport.Git(t, branch.Worktree, "status", "--porcelain"), "the edit reads as uncommitted again")
	_, _, err = e.Restore(t.Context(), "tidy-1")
	require.ErrorContains(t, err, "already restored")
}

func TestTidySquashesCorrectionsAndKeepsTheirTrailers(t *testing.T) {
	t.Parallel()
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
	require.Equal(t, "Ada <ada@example.org>", testsupport.Git(t, dir, "log", "-1", "--format=%an <%ae>"))
}

func TestTidyKeepsAGoodHistoryAndOrdersSeveralPorts(t *testing.T) {
	t.Parallel()
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

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
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
	t.Parallel()
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
	testsupport.Git(t, dir, "checkout", "--", ".")

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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "merged", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	testsupport.Git(t, dir, "switch", "-q", "-c", "side")
	write(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 2\n"})
	commitAs(t, dir, "Ada ada@example.org", "jq: update to 2")
	testsupport.Git(t, dir, "switch", "-q", branch.Name)
	write(t, dir, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, dir, "Ada ada@example.org", "libharbor: update to 3")
	testsupport.Git(t, dir, "-c", "user.name=Ada", "-c", "user.email=ada@example.org", "merge", "-q", "--no-edit", "side")
	_, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.ErrorContains(t, err, "never flattens a merge")
}

// A tidy checkpoint keeps the index it replaced, which can hold a staged
// version neither the old head nor the working files have, and restore
// puts it back, unless something was staged since (Design v3 §8). From
// the 2026-09-25 implementation review.
func TestRestorePutsTheStagedVersionBack(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# staged version\n"})
	testsupport.Git(t, branch.Worktree, "add", "textproc/jq/Portfile")
	staged := testsupport.Git(t, branch.Worktree, "show", ":textproc/jq/Portfile")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# working version\n"})

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch, Squash: true, Message: "jq: update to 1.8.1"})
	require.NoError(t, err)
	tidied, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.NotEmpty(t, tidied.Checkpoint.Index)
	require.NotEqual(t, staged, testsupport.Git(t, branch.Worktree, "show", ":textproc/jq/Portfile"), "tidy leaves the index at its new head")
	require.Equal(t, string(tidied.Checkpoint.Index), testsupport.Git(t, branch.Worktree, "rev-parse", tidied.Checkpoint.IndexRef()+"^{tree}"), "a ref keeps it reachable")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# staged after tidy\n"})
	testsupport.Git(t, branch.Worktree, "add", "textproc/jq/Portfile")
	_, _, err = e.Restore(t.Context(), tidied.Checkpoint.Name())
	require.ErrorContains(t, err, "dockhand/jq-update has changes staged after "+tidied.Checkpoint.Name())
	testsupport.Git(t, branch.Worktree, "reset", "-q")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# working version\n"})

	_, _, err = e.Restore(t.Context(), tidied.Checkpoint.Name())
	require.NoError(t, err)
	require.Equal(t, staged, testsupport.Git(t, branch.Worktree, "show", ":textproc/jq/Portfile"))
	require.Equal(t, "name jq\nversion 1.8.1\n# working version\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")), "the working files are untouched")
}

// A person's edit beside dockhand's uncommitted edits keeps their subject,
// the update's though a revision bump followed it, while every line they
// wrote still stands. Lines they didn't write are the person's to change.
// Once one they wrote is gone, as when the version is taken back, the
// subject is the person's to give (the hugo exercise's certigo run,
// finding 2).
func TestAPersonsEditBesideAnUpdateKeepsItsSubject(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: "jq", Subject: "rebuild against the new oniguruma"})
	require.NoError(t, err)
	require.Equal(t, modelineHead+"name jq\nversion 1.8.1\nrevision 1\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": modelineHead + "name jq\nversion 1.8.1\nrevision 1\nbuild.args-append VERSION=1.8.1\n"})

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 1)
	group := plan.Groups[0]
	require.False(t, group.FromEdits)
	require.Equal(t, "jq: update to 1.8.1", group.Subject(), "the update's, not the revision bump's")
	require.Contains(t, group.Notes, "subject from dockhand's edits, which the other changes leave standing")
	require.Contains(t, group.Notes, "has changes dockhand's commands did not make; review them")
	require.NotContains(t, group.Message, "Generated-By", "the commit isn't dockhand's edits alone")
	require.Empty(t, group.Blocking)
	require.False(t, plan.Unambiguous(), "a person's changes are reviewed")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": modelineHead + "version 1.8.1\nrevision 1\n"})
	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1", plan.Groups[0].Subject())
	require.Contains(t, plan.Groups[0].Notes, "subject from dockhand's edits, which the other changes leave standing", "a line dockhand didn't write is the person's")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\ngo.setup github.com/jqlang/jq 1.8.2 v\nrevision 1\n"})
	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.NotContains(t, plan.Groups[0].Notes, "subject from dockhand's edits, which the other changes leave standing", "the version dockhand wrote is gone")
	require.NotEqual(t, "jq: update to 1.8.1", plan.Groups[0].Subject())
}

// Rearranging tidy's commits so a port comes before one it depends on is
// noted on its commit, as the check of the files orders them; the order
// is still the person's (the libuv run's finding 6).
func TestARegroupPuttingADependentFirstIsNoted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {port("jq", "libharbor")}, "devel/libharbor": {port("libharbor")},
	}})
	// Edits to both, uncommitted, checked as they are: tidy proposes a
	// commit for each.
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3.1\n", "textproc/jq/Portfile": "name jq\nversion 1.8.2\n"})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	checkPlan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, checkPlan, model.OriginPerson)
	require.NoError(t, err)
	check, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, check.State)

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	order := map[string]int{}
	for i, group := range plan.Groups {
		order[group.Directory] = i + 1
	}
	require.Contains(t, order, "textproc/jq")
	require.Contains(t, order, "devel/libharbor")
	dependentFirst := fmt.Sprintf("%d %d", order["textproc/jq"], order["devel/libharbor"])
	regrouped, err := e.Regroup(t.Context(), plan, dependentFirst, "")
	require.NoError(t, err)
	require.Equal(t, "textproc/jq", regrouped.Groups[0].Directory)
	require.Contains(t, regrouped.Groups[0].Notes, "comes before commit 2, which changes libharbor, a port jq depends on as "+check.Name()+" orders them")

	dependencyFirst := fmt.Sprintf("%d %d", order["devel/libharbor"], order["textproc/jq"])
	regrouped, err = e.Regroup(t.Context(), plan, dependencyFirst, "")
	require.NoError(t, err)
	for _, group := range regrouped.Groups {
		for _, note := range group.Notes {
			require.NotContains(t, note, "depends on", "the dependency first needs no note")
		}
	}
}

// A version bump made by hand is named by the version the port itself
// declares, not another subport's setup line: git's own version line was
// never read, since git-devel's github.setup came first (the git run's
// finding 5).
func TestTidyNamesAHandMadeBumpByThePortsOwnVersion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-hand", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.2\nsubport jq-devel {\n    github.setup jqlang jq 1.9.0 jq-\n}\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "wip")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 1)
	require.Equal(t, "jq: update to 1.8.2", plan.Groups[0].Subject())
}

// A re-tidy keeps the commits it wouldn't change, as they are: #35044's
// first two commits were written again with the same trees and messages,
// another committer time, so submitting replaced the pull request's
// history rather than adding a commit to it (adding py-flatbuffers,
// finding 3). Once a commit is made anew, those after it are too.
func TestTidyKeepsTheCommitsItWouldntChange(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/yq/Portfile": "name yq\nversion 4.54.1\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "yq: new port")
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	first, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Zero(t, first.Kept)
	jq := first.Commits[0]

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: "yq", Subject: "rebuild for jq 1.8.1"})
	require.NoError(t, err)
	plan, err = e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 2, "%+v", plan.Groups)
	second, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 1, second.Kept)
	require.Equal(t, jq, second.Commits[0], "jq's commit is the one it was")
	require.NotEqual(t, jq, second.Commits[1])
	require.Equal(t, []string{"jq: update to 1.8.1", "yq: rebuild for jq 1.8.1"}, log(t, branch.Worktree, branch.Base))
}

// Once a branch's work is committed, a worktree dockhand made is narrowed
// back to _resources and the ports the branch changes: edit jq widened the
// ov branch's worktree for good (the ov run's finding 6).
func TestTidyNarrowsAWorktreeBackToTheBranchsPorts(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	_, err = e.Edit(t.Context(), branch, "libharbor")
	require.NoError(t, err)
	worktree, err := e.openWorktree(t.Context(), branch)
	require.NoError(t, err)
	cone, err := worktree.SparseCone(t.Context())
	require.NoError(t, err)
	require.Contains(t, cone, "devel/libharbor", "edit widened it")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	result, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, []string{"devel/libharbor"}, result.Narrowed)
	cone, err = worktree.SparseCone(t.Context())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"_resources", "textproc/jq"}, cone)
	require.NoDirExists(t, filepath.Join(branch.Worktree, "devel/libharbor"))
}

// A patch the Portfile names, and Git doesn't track, is the port's: tidy
// commits it with the Portfile, where it committed the patchfiles line
// alone and the check 404'd fetching the patch (field testing, net-snmp,
// check-469). Any other untracked file there is left out and said, and a
// named one Git ignores is left out as git add leaves it.
func TestTidyTakesInThePatchesThePortfileNames(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-patch", Here: true})
	require.NoError(t, err)
	dir := branch.Worktree
	common := testsupport.Git(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	write(t, common, map[string]string{"info/exclude": "ignored.diff\n"})
	write(t, dir, map[string]string{
		"textproc/jq/Portfile":           "name jq\nversion 1.7.1\npatchfiles fix.diff ignored.diff\n",
		"textproc/jq/files/fix.diff":     "fix\n",
		"textproc/jq/files/ignored.diff": "ignored\n",
		"textproc/jq/files/notes.txt":    "notes\n",
	})

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/jq/files/fix.diff"}, plan.Taken)
	require.Equal(t, []string{"textproc/jq/files/notes.txt"}, plan.LeftOut)
	require.Equal(t, []string{"textproc/jq/files/ignored.diff"}, plan.Ignored)
	require.Len(t, plan.Groups, 1)
	require.ElementsMatch(t, []string{"textproc/jq/Portfile", "textproc/jq/files/fix.diff"}, plan.Groups[0].Paths)
	plan.Groups[0].SetSubject("jq: fix the build")

	write(t, dir, map[string]string{"textproc/jq/files/fix.diff": "fix, again\n"})
	_, err = e.ApplyTidy(t.Context(), plan)
	require.ErrorIs(t, err, ErrStalePlan, "the patch changed after the plan")
	write(t, dir, map[string]string{"textproc/jq/files/fix.diff": "fix\n"})

	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, "textproc/jq/Portfile\ntextproc/jq/files/fix.diff", testsupport.Git(t, dir, "show", "--format=", "--name-only", "HEAD"))
	require.Equal(t, "?? textproc/jq/files/notes.txt", testsupport.Git(t, dir, "status", "--porcelain"))

	again, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, again.Keep)
	require.Equal(t, []string{"textproc/jq/files/notes.txt"}, again.LeftOut, "still uncommitted, and said")
}

// definedAt names the ports textproc/terraform defines at each commit, as
// each one's index would: the branch's head adds terraform-1.17.
type definedAt map[model.ObjectID][]string

func (definedAt) Dependents(context.Context, model.Source, []string) ([]treequery.Dependent, error) {
	return nil, nil
}

func (d definedAt) PortsDefined(_ context.Context, source model.Source, _ []string) (map[string][]string, error) {
	return map[string][]string{"sysutils/terraform": d[source.Commit]}, nil
}

// subject-port reads the ports a directory defines at the branch's head
// too, so a commit naming the subport it adds names its port; one naming
// no port of the directory is still flagged (the docs dig's I21).
func TestASubjectMayNameTheSubportTheBranchAdds(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	useDependents(e, definedAt{"base": {"terraform", "terraform-1.16"}, "head": {"terraform", "terraform-1.16", "terraform-1.17"}})
	history := []git.HistoryCommit{
		{ID: "1111111", Message: "terraform-1.17: new subport\n", Paths: []string{"sysutils/terraform/Portfile"}},
		{ID: "2222222", Message: "terraform-2.0: new subport\n", Paths: []string{"sysutils/terraform/Portfile"}},
	}
	findings := commitrules.CheckCommits(e.ruleCommits(t.Context(), model.Source{Commit: "base"}, model.Source{Commit: "head"}, history))
	var flagged []string
	for _, finding := range findings {
		if finding.Code == "subject-port" {
			flagged = append(flagged, finding.Commit)
		}
	}
	require.Equal(t, []string{"2222222"}, flagged, "terraform-1.17 is the head's, and terraform-2.0 is nobody's")
}

// A port can satisfy a dependency on another where MacPorts evaluates its
// conflicts as naming it: a -devel sibling, or one the survey names, as
// pkgconf for pkgconfig (the review survey's §5); with the path-style
// spec the tree writes most for it, and nothing where it writes none.
func TestAProviderIsAPortWhoseConflictsNameIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	conflicts := func(name, of string) macports.PortInfo {
		info := port(name)
		info.Options["conflicts"] = of
		return info
	}
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
		"devel/pkgconf": {conflicts("pkgconf", "pkgconfig")},
		"devel/libfoo":  {port("libfoo"), conflicts("libfoo-devel", "libfoo")},
		"devel/libbar":  {port("libbar"), port("libbar-devel")},
		"devel/gettext": {port("gettext")},
	}})
	write(t, f.clone, map[string]string{
		"devel/wharf/Portfile": "name wharf\ndepends_build path:bin/pkg-config:pkgconfig\ndepends_lib path:lib/libfoo.dylib:libfoo path:lib/libbar.dylib:libbar\n",
		"devel/dock/Portfile":  "name dock\ndepends_build path:bin/pkg-config:pkgconfig\n",
		"devel/pier/Portfile":  "name pier\ndepends_build path:lib/pkgconfig/x.pc:pkgconfig\n",
	})
	testsupport.Git(t, f.clone, "add", "-A")
	testsupport.Git(t, f.clone, "commit", "-q", "-m", "ports")
	tree := testsupport.Git(t, f.clone, "rev-parse", "HEAD^{tree}")
	providers := e.providers(t.Context(), tree)
	others, spec := providers("pkgconfig")
	require.Equal(t, []string{"pkgconf"}, others)
	require.Equal(t, "path:bin/pkg-config:pkgconfig", spec, "the one the tree writes most")
	others, spec = providers("libfoo")
	require.Equal(t, []string{"libfoo-devel"}, others)
	require.Equal(t, "path:lib/libfoo.dylib:libfoo", spec)
	others, _ = providers("libbar")
	require.Empty(t, others, "a sibling that doesn't conflict provides nothing it says")
	_, spec = providers("gettext")
	require.Empty(t, spec, "the tree writes it path-style nowhere")
}

// A hand edit's trailing spaces and swallowing \ are committed fixed by
// tidy, as dockhand's own edits, and the working files with them; its
// untouched neighbours stay as they were (the person's "we should have
// Dockhand do those", 2026-10-27).
func TestTidyFixesTheLintFindingsOnTheLinesABranchTouches(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/yq/Portfile": "name yq \nversion 4.54.1\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "yq: new port")
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "yq-hand"})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/yq/Portfile": "name yq \nversion 4.54.2  \ndepends_lib port:jq \\\nuse_configure no\n"})

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/yq/Portfile: removed trailing whitespace on line 2",
		`textproc/yq/Portfile: dropped the \ ending line 3, which carried the next line's option into its command`}, plan.Fixed)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	want := "name yq \nversion 4.54.2\ndepends_lib port:jq\nuse_configure no\n"
	require.Equal(t, strings.TrimSuffix(want, "\n"), testsupport.Git(t, branch.Worktree, "show", "HEAD:textproc/yq/Portfile"))
	written, err := os.ReadFile(filepath.Join(branch.Worktree, "textproc/yq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, want, string(written))
	require.Empty(t, testsupport.Git(t, branch.Worktree, "status", "--porcelain"))
}
