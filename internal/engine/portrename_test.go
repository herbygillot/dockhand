package engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A rename writes the Portfile and its files at the new name's directory,
// the name set where the Portfile sets it, and the old Portfile as an
// obsolete stub replaced_by the new name, at its version with its revision
// raised: one edit dockhand records, so tidy takes it as dockhand's. A
// move keeps the name and changes the directory (the person's "I agree we
// add a `rename` action to Dockhand.", 2026-10-29).
func TestRenamingAPort(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-rename", Ports: []string{"jq"}})
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq", "devel/libharbor")
	write(t, branch.Worktree, map[string]string{"textproc/jq/files/patch-a.diff": "--- a\n+++ b\n", "textproc/jq/Portfile": "name jq\nversion 1.7.1\ncategories textproc\nlivecheck.url https://jq.example/jq\n"})
	testsupport.Git(t, branch.Worktree, "add", "-A")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "jq: a patch")
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {{Name: "jq", Version: "1.7.1", Options: map[string]string{"categories": "textproc"}}}, "devel/libharbor": {port("libharbor")},
	}})

	planned, err := e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "jq", Name: "jq2", Plan: true})
	require.NoError(t, err)
	require.Equal(t, &portfile.Stub{Name: "jq", ReplacedBy: "jq2", Version: "1.7.1", Revision: 1, Categories: []string{"textproc"}}, planned.Stub)
	require.Equal(t, []string{"line 4: livecheck.url https://jq.example/jq"}, planned.Left, "what the new name implies is the person's")
	require.False(t, planned.Applied)
	require.NoFileExists(t, filepath.Join(branch.Worktree, "textproc/jq2/Portfile"), "a plan writes nothing")

	_, err = e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "jq", Name: "libharbor"})
	require.ErrorContains(t, err, "there is already a port libharbor, at devel/libharbor")

	renamed, err := e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "jq", Name: "jq2"})
	require.NoError(t, err)
	require.True(t, renamed.Applied)
	require.Equal(t, "name jq2\nversion 1.7.1\ncategories textproc\nlivecheck.url https://jq.example/jq\n", read(t, filepath.Join(branch.Worktree, "textproc/jq2/Portfile")))
	require.Equal(t, "--- a\n+++ b\n", read(t, filepath.Join(branch.Worktree, "textproc/jq2/files/patch-a.diff")))
	require.Equal(t, string(renamed.Stub.Text()), read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))
	require.NoFileExists(t, filepath.Join(branch.Worktree, "textproc/jq/files/patch-a.diff"))
	var edits []model.Edit
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Len(t, edits, 1)
	require.Equal(t, model.EditRename, edits[0].Kind)
	require.Equal(t, "jq2: rename from jq", edits[0].Subject)
	require.Equal(t, "textproc/jq2", edits[0].Directory)
	require.Len(t, edits[0].Files, 4, "both Portfiles, and the patch at each place")

	// A move keeps the name, and names the category first where the old
	// was.
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\ncategories devel net\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "libharbor: categories")
	_, err = e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "libharbor", Category: "devel"})
	require.ErrorContains(t, err, "libharbor is in devel already")
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "devel/libharbor/Portfile"), []byte("name libharbor\nversion 3\ncategories devel net\n"), 0o644))
	_, err = e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "libharbor", Category: "net"})
	require.ErrorContains(t, err, "devel/libharbor has changes not committed")
	testsupport.Git(t, branch.Worktree, "checkout", "--", "devel/libharbor/Portfile")
	moved, err := e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "libharbor", Category: "net"})
	require.NoError(t, err)
	require.Nil(t, moved.Stub)
	require.True(t, moved.Category)
	require.Equal(t, "name libharbor\nversion 2\ncategories net net\n", read(t, filepath.Join(branch.Worktree, "net/libharbor/Portfile")))
	require.NoFileExists(t, filepath.Join(branch.Worktree, "devel/libharbor/Portfile"))
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Equal(t, model.EditMove, edits[1].Kind)
	require.Equal(t, "libharbor: move to net", edits[1].Subject)
}

// A rename is one change: the new port planned at its directory, read
// against the old Portfile and not linted as new, the stub excluded as
// MacPorts' CI excludes a port replaced_by another; tidy makes it one
// commit, "jq2: rename from jq", with an update after it "…, update to
// 1.8.1", and submit takes its title from it and calls it no new port
// (dockhand rename, 2026-10-29).
func TestARenameIsCheckedTidiedAndSubmittedAsOne(t *testing.T) {
	t.Parallel()
	for _, update := range []bool{false, true} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		f.withFork(t, e)
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-rename"})
		require.NoError(t, err)
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "textproc/jq")
		usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
			"textproc/jq":  {{Name: "jq", Version: "1.7.1", Options: map[string]string{"categories": "textproc", "replaced_by": "jq2"}}},
			"textproc/jq2": {{Name: "jq2", Version: "1.7.1", Options: map[string]string{"categories": "textproc"}}},
		}})
		e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
		_, err = e.RenamePort(t.Context(), PortRenameRequest{Branch: branch, Port: "jq", Name: "jq2"})
		require.NoError(t, err)
		subject := "jq2: rename from jq"
		if update {
			write(t, branch.Worktree, map[string]string{"textproc/jq2/Portfile": "name jq2\nversion 1.8.1\n"})
			testsupport.Git(t, branch.Worktree, "add", "textproc/jq2/Portfile")
			subject = "jq2: rename from jq, update to 1.8.1"
		}

		proposal, err := e.ProposeCheck(t.Context(), CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureAuto}, Where: Where{Environments: []model.Environment{tahoeArm}}})
		require.NoError(t, err)
		require.Empty(t, proposal.Plan.Unresolved)
		i := slices.IndexFunc(proposal.Plan.Targets, func(target model.PlanTarget) bool { return target.Target.Name == "jq2" })
		require.GreaterOrEqual(t, i, 0, "%+v", proposal.Plan.Targets)
		jq2 := proposal.Plan.Targets[i]
		require.Equal(t, "textproc/jq", jq2.MovedFrom)
		require.False(t, jq2.Added, "a renamed port isn't linted as a new one")
		require.False(t, slices.ContainsFunc(proposal.Plan.Targets, func(target model.PlanTarget) bool { return target.Directory == "textproc/jq" }), "the stub is the rename's")

		plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
		require.NoError(t, err)
		require.Len(t, plan.Groups, 1, "the rename is one commit")
		require.Equal(t, "textproc/jq2", plan.Groups[0].Directory)
		require.Equal(t, "textproc/jq", plan.Groups[0].MovedFrom)
		require.Equal(t, subject, plan.Groups[0].Subject())
		_, err = e.ApplyTidy(t.Context(), plan)
		require.NoError(t, err)

		submit, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
		require.NoError(t, err)
		require.Equal(t, subject, submit.Title)
		require.Equal(t, []string{"jq2"}, submit.Ports)
		require.NotContains(t, submit.Body, "New port", "a renamed port is no new one")
	}
}
