package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A port the person finished by hand after dockhand refused its update is
// concluded by update --hand-edit, in either order: given after they
// finished, it writes nothing and asks no update again; given before, it
// writes the attempt as today. Either way the target's version is done,
// by hand, so a check nobody attends goes on and ship's continuation
// skips the port; another port of the branch stays owed. The refusal
// that stopped a check nobody attended named ship -b, which asked the
// same update again and failed (field testing's item 50, #355).
func TestAHandFinishedUpdateIsConcludedByHandEdit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	p.refused = ErrUnsupported.Because("only direct HTTP(S) or FTP master sites are supported")
	release := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.8.1"}, Version: "1.8.1", Forge: "github", Tag: "jq-1.8.1"}

	t.Run("after the person finished it", func(t *testing.T) {
		t.Parallel()
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-after"})
		require.NoError(t, err)
		require.NoError(t, e.keepTarget(t.Context(), branch, "update", "jq", func(target *model.RequestTarget) { target.Release = &release }))
		require.NoError(t, e.keepTarget(t.Context(), branch, "update", "other", func(target *model.RequestTarget) {}))
		_, err = e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq", Version: "1.8.1", Release: &release})
		require.NotNil(t, refusal.Of(err), "the update is refused")
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq")
		portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
		finished := "name jq\nversion 1.8.1\n# finished by hand\n"
		require.NoError(t, os.WriteFile(portfile, []byte(finished), 0o644))

		_, err = e.UnfinishedGate(t.Context(), reread(t, e, branch.ID), true)
		require.ErrorContains(t, err, "jq has no update yet")
		require.ErrorContains(t, err, "where you finished it by hand, dockhand update jq --hand-edit concludes it, and dockhand ship -b jq-after prepares other", "ship -b would ask jq's refused update again; other is ship's to prepare")

		update, err := e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq", HandEdit: true})
		require.NoError(t, err, "the refusal it answers isn't asked again")
		require.Equal(t, "jq to 1.8.1: finished by hand", update.HandEdit)
		data, err := os.ReadFile(portfile)
		require.NoError(t, err)
		require.Equal(t, finished, string(data), "the person's files are left as they wrote them")

		open, err := e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
		require.NoError(t, err)
		require.Len(t, open, 1, "ship's continuation skips jq")
		require.Equal(t, "other", open[0].Port, "and the other port stays owed")
		_, err = e.UnfinishedGate(t.Context(), reread(t, e, branch.ID), true)
		require.ErrorContains(t, err, "other has no update yet")
		require.NotContains(t, err.Error(), "jq has no update yet")
		outcome, ok := reread(t, e, branch.ID).Request.Targets[0].Outcome("version")
		require.True(t, ok)
		require.Equal(t, model.ActionDone, outcome.Result)
		require.Equal(t, "concluded by hand at 1.8.1 (update --hand-edit)", outcome.Why)
		status, err := e.BranchStatus(t.Context(), reread(t, e, branch.ID))
		require.NoError(t, err)
		require.Len(t, status.HandEdits, 1, "the hand edit stays on the record, for status and the pull request")
		require.Equal(t, "hand edit (yours, by hand): the update was refused, jq to 1.8.1: finished by hand", status.HandEdits[0].Line(), "no attempt of dockhand's was written")

		// A hand change to the Portfile alone leaves it concluded; a newer
		// version asked is a new ask, owed again until it is done.
		require.NoError(t, os.WriteFile(portfile, []byte(finished+"# and changed again\n"), 0o644))
		open, err = e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
		require.NoError(t, err)
		require.Equal(t, []string{"other"}, portsOf(open), "a later hand change doesn't reopen jq")
		newer := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.9.0"}, Version: "1.9.0", Forge: "github", Tag: "jq-1.9.0"}
		require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "update", "jq", func(target *model.RequestTarget) { target.Release = &newer }))
		open, err = e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"jq", "other"}, portsOf(open), "update jq 1.9.0 is a new ask, and jq is owed again")
	})

	t.Run("a version asked that the Portfile doesn't name", func(t *testing.T) {
		t.Parallel()
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-patch"})
		require.NoError(t, err)
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq")
		testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/files/patch-fix.diff": "--- a\n+++ b\n"})
		update, err := e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq", Version: "1.8.1", Release: &release, HandEdit: true})
		require.NoError(t, err)
		require.True(t, update.Applied, "the person hasn't finished 1.8.1: dockhand's attempt is written for it")
		require.NotContains(t, update.HandEdit, "finished by hand")
		data, err := os.ReadFile(filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
		require.NoError(t, err)
		require.Contains(t, string(data), "1.8.1")
	})

	// attempted is an update --hand-edit with no version typed, on a branch
	// kept for jq 1.8.1 whose Portfile was edited, that wrote the attempt
	// and concluded nothing by hand.
	attempted := func(t *testing.T, name string, edit func(portfile string) string) {
		branch, err := e.Start(t.Context(), StartRequest{Name: name, Ports: []string{"jq"}})
		require.NoError(t, err)
		require.NoError(t, e.keepTarget(t.Context(), branch, "update", "jq", func(target *model.RequestTarget) { target.Release = &release }))
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq")
		path := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
		was, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte(edit(string(was))), 0o644))
		update, err := e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq", HandEdit: true})
		require.NoError(t, err)
		require.True(t, update.Applied, "dockhand's attempt is written")
		require.NotContains(t, update.HandEdit, "finished by hand")
		data, err := os.ReadFile(filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
		require.NoError(t, err)
		require.Contains(t, string(data), "1.8.1", "at the version the branch asks")
	}
	t.Run("a Portfile changed, its version the base's", func(t *testing.T) {
		t.Parallel()
		// rc3's C14: a changed file and no version typed concluded the port
		// finished at the version it already had.
		attempted(t, "jq-c14", func(portfile string) string { return portfile + "# a line changed, the version as it was\n" })
	})
	t.Run("a version that can't be read", func(t *testing.T) {
		t.Parallel()
		attempted(t, "jq-unread", func(string) string { return "name jq\nset v 1.8.1\nversion ${v}\n" })
	})
	t.Run("moved from the base's version with none asked", func(t *testing.T) {
		t.Parallel()
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-moved"})
		require.NoError(t, err)
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq")
		testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.9.0\n"})
		update, err := e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq", HandEdit: true})
		require.NoError(t, err)
		require.Equal(t, "jq to 1.9.0: finished by hand", update.HandEdit, "the person moved it, and asked nothing else")
	})

	t.Run("before the person finished it", func(t *testing.T) {
		t.Parallel()
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "jq-before"}, Action: model.EditUpdate, Port: "jq", Version: "1.8.1", Release: &release, HandEdit: true})
		require.NoError(t, err)
		require.True(t, update.Applied, "the attempt is written, as today")
		open, err := e.UnfinishedTargets(t.Context(), reread(t, e, update.Branch.ID))
		require.NoError(t, err)
		require.Empty(t, open)
		_, err = e.UnfinishedGate(t.Context(), reread(t, e, update.Branch.ID), true)
		require.NoError(t, err, "a check nobody attends goes on with what the person finishes")
	})
}

// portsOf are the targets' ports.
func portsOf(targets []model.RequestTarget) []string {
	var ports []string
	for _, target := range targets {
		ports = append(ports, target.Port)
	}
	return ports
}
