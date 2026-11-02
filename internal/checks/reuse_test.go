package checks

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A port directory whose trees differ only in Portfile lines inside one
// subport's block names that subport, its siblings' results standing; any
// other file changed, or a change outside every block, says nothing
// (reuse.Blocks; field testing's terraform replay, 2026-10-22).
func TestADirectorysEditIsReadByTheSubportBlocksItIsIn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testsupport.Git(t, root, "init", "-q")
	checkout, err := git.Open(t.Context(), root, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	// directory commits files over what the checkout holds and reads the
	// port directory's tree.
	directory := func(files map[string]string) model.ObjectID {
		t.Helper()
		testsupport.WriteFiles(t, root, files)
		testsupport.Git(t, root, "add", "-A")
		testsupport.Git(t, root, "commit", "-q", "--allow-empty", "-m", "edit")
		return model.ObjectID(strings.TrimSpace(testsupport.Git(t, root, "rev-parse", "HEAD:sysutils/terraform")))
	}
	portfile := func(v16, top string) string {
		return "PortSystem 1.0\nname terraform\nversion " + top + "\n\nsubport terraform-1.16 {\n    version " + v16 + "\n}\n\nsubport terraform-1.0 {\n    version 1.0.11\n}\n"
	}
	d := &driver{c: &Service{Checkout: checkout}}
	then := directory(map[string]string{"sysutils/terraform/Portfile": portfile("1.16.6", "1.0"), "sysutils/terraform/files/a.patch": "a\n"})
	base := func() {
		t.Helper()
		directory(map[string]string{"sysutils/terraform/Portfile": portfile("1.16.6", "1.0"), "sysutils/terraform/files/a.patch": "a\n"})
		testsupport.Git(t, root, "rm", "-q", "--ignore-unmatch", "sysutils/terraform/files/b.patch")
		testsupport.Git(t, root, "commit", "-q", "--allow-empty", "-m", "back")
	}

	subports, ok := d.blocks(t.Context())("sysutils/terraform", then, directory(map[string]string{"sysutils/terraform/Portfile": portfile("1.16.7", "1.0")}))
	require.True(t, ok)
	require.Equal(t, []string{"terraform-1.16"}, subports)

	base()
	_, ok = d.blocks(t.Context())("sysutils/terraform", then, directory(map[string]string{"sysutils/terraform/Portfile": portfile("1.16.6", "2.0")}))
	require.False(t, ok, "outside every block")
	base()
	_, ok = d.blocks(t.Context())("sysutils/terraform", then, directory(map[string]string{"sysutils/terraform/files/a.patch": "b\n"}))
	require.False(t, ok, "another file changed")
	base()
	_, ok = d.blocks(t.Context())("sysutils/terraform", then, directory(map[string]string{"sysutils/terraform/files/b.patch": "b\n"}))
	require.False(t, ok, "a file added")

	// The reading reuse takes, the same for a pair of trees asked again.
	base()
	blocks := d.blocks(t.Context())
	now := directory(map[string]string{"sysutils/terraform/Portfile": portfile("1.16.7", "1.0")})
	for range 2 {
		subports, ok := blocks("sysutils/terraform", then, now)
		require.True(t, ok)
		require.Equal(t, []string{"terraform-1.16"}, subports)
	}
}
