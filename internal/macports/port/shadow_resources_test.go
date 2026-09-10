package port

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// handleAt is this package's own binding, since porttest imports it.
func handleAt(t *testing.T, portdir string) Handle {
	t.Helper()
	return New(tree.Target{Portdir: portdir}, evaluator(t))
}

// treeWithGroup builds a ports tree carrying a port group of its own,
// which is the case the installation's default tree cannot answer for.
func treeWithGroup(t *testing.T, group, portfile string) string {
	t.Helper()
	root := t.TempDir()
	groups := filepath.Join(root, macports.ResourcesDir, "port1.0", "group")
	require.NoError(t, os.MkdirAll(groups, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(groups, "shadowprobe-1.0.tcl"), []byte(group), 0o644))
	dir := filepath.Join(root, "devel", "groupprobe")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

// THE BUG, AS A TEST. A shadow of unchanged bytes must evaluate to what
// the original does. Before the tree's _resources reached the shadow,
// this failed with "PortGroup not found": getportresourcepath derives
// the tree two levels up from the portdir and falls back to the
// INSTALLATION'S DEFAULT TREE when it finds no _resources there, so the
// port group the tree carries was invisible to every prediction made
// about the port that uses it.
func TestAnUnchangedShadowEvaluatesTheSame(t *testing.T) {
	const group = `options shadowprobe.mark
default shadowprobe.mark {set-by-this-tree}
version 4.5.6
`
	portdir := treeWithGroup(t, group, `PortSystem 1.0
PortGroup shadowprobe 1.0
name groupprobe
checksums rmd160 0 sha256 0 size 0
`)
	h := handleAt(t, portdir)
	ctx := context.Background()

	real, err := h.Values(ctx)
	require.NoError(t, err)
	require.Equal(t, "4.5.6", real.Version, "the tree's own port group sets it")

	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	require.NoError(t, err)
	shadow, cleanup, err := h.Shadow(src)
	require.NoError(t, err)
	defer cleanup()

	got, err := shadow.Values(ctx)
	require.NoError(t, err, "a shadow of unchanged bytes must evaluate, not fail to find the port group")
	assert.Equal(t, real.Semantic, got.Semantic,
		"same bytes, same tree: every semantic field must agree")
}

// The quiet half of the same defect, and the one that would corrupt a
// prediction rather than stop it: where the default tree HAS a group of
// the same name, a shadow missing _resources answers from that one. The
// difference then looks like something the edit did.
func TestAShadowReadsThisTreesGroupAndNotAnother(t *testing.T) {
	portdir := treeWithGroup(t, "version 1.1.1\n", `PortSystem 1.0
PortGroup shadowprobe 1.0
name groupprobe
checksums rmd160 0 sha256 0 size 0
`)
	h := handleAt(t, portdir)

	// A second tree with the SAME group name and a different answer.
	other := treeWithGroup(t, "version 9.9.9\n", `PortSystem 1.0
PortGroup shadowprobe 1.0
name groupprobe
checksums rmd160 0 sha256 0 size 0
`)
	otherVals, err := handleAt(t, other).Values(context.Background())
	require.NoError(t, err)
	require.Equal(t, "9.9.9", otherVals.Version, "the fixture's two trees really do disagree")

	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	require.NoError(t, err)
	shadow, cleanup, err := h.Shadow(src)
	require.NoError(t, err)
	defer cleanup()

	got, err := shadow.Values(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.1.1", got.Version, "the shadow belongs to the tree it was made from")
}

// A bare portdir in a temporary directory has no tree around it, which
// is what every other fixture in this suite is. Shadowing one must not
// become an error just because there are no resources to carry.
func TestShadowOfATreelessPortdirStillWorks(t *testing.T) {
	dir := t.TempDir()
	src := []byte("PortSystem 1.0\nname bare\nversion 1.0\nchecksums rmd160 0 sha256 0 size 0\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), src, 0o644))
	h := handleAt(t, dir)

	shadow, cleanup, err := h.Shadow(src)
	require.NoError(t, err)
	defer cleanup()
	got, err := shadow.Values(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.0", got.Version)
}

// THE SHADOW'S RESOURCES ARE ITS OWN. This is what a link would have
// cost, and it is the property the rust case needs: a port group edited
// in the shadow must be the one the shadow evaluates, and must not
// reach the checkout it was copied from. The same test covers the
// cleanup, which under a link would have deleted every port group in
// the user's tree for a prediction nobody asked to be destructive.
func TestShadowResourcesAreItsOwn(t *testing.T) {
	portdir := treeWithGroup(t, "version 1.0\n", `PortSystem 1.0
PortGroup shadowprobe 1.0
name groupprobe
checksums rmd160 0 sha256 0 size 0
`)
	treeRoot := filepath.Dir(filepath.Dir(portdir))
	realGroup := filepath.Join(treeRoot, macports.ResourcesDir, "port1.0", "group", "shadowprobe-1.0.tcl")

	h := handleAt(t, portdir)
	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	require.NoError(t, err)
	shadow, cleanup, err := h.Shadow(src)
	require.NoError(t, err)

	shadowRoot := filepath.Dir(filepath.Dir(shadow.Target.Portdir))
	shadowGroup := filepath.Join(shadowRoot, macports.ResourcesDir, "port1.0", "group", "shadowprobe-1.0.tcl")
	fi, err := os.Lstat(filepath.Join(shadowRoot, macports.ResourcesDir))
	require.NoError(t, err)
	require.Zero(t, fi.Mode()&os.ModeSymlink, "the shadow owns its resources; it does not borrow them")

	// EDIT THE SHADOW'S PORT GROUP. This is the rust shape: the change
	// is in the resource, not in the Portfile.
	require.NoError(t, os.WriteFile(shadowGroup, []byte("version 7.7.7\n"), 0o644))
	got, err := shadow.Values(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "7.7.7", got.Version, "the shadow evaluates the port group as the shadow has it")

	// And the tree it was copied from is untouched, before and after
	// the shadow is dropped.
	original, err := os.ReadFile(realGroup)
	require.NoError(t, err)
	assert.Equal(t, "version 1.0\n", string(original), "the checkout's own port group is unchanged")
	cleanup()
	assert.FileExists(t, realGroup, "and it survives the shadow's removal")
}
