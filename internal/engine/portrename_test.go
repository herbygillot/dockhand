package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

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
