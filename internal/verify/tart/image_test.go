package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cacheAt points both sidecar roots at directories the test owns.
func cacheAt(t *testing.T) string {
	t.Helper()
	home, cache := t.TempDir(), t.TempDir()
	oh, oc := tartHome, cacheDir
	tartHome = func() (string, error) { return home, nil }
	cacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { tartHome, cacheDir = oh, oc })
	return home
}

// link stands in for what `tart pull` leaves behind: a tag symlinked to
// the digest directory it resolved to.
func link(t *testing.T, home, repo, tag, digest string) {
	t.Helper()
	dir := filepath.Join(home, "cache", "OCIs", filepath.FromSlash(repo))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, digest), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, digest), filepath.Join(dir, tag)))
}

// THE TAG IS NOT THE IMAGE. Bases are built from `:latest`, which moves,
// so a verdict that said "built on Tahoe" named a release and not an
// image — and two passes months apart could disagree about what that
// meant with nothing in either record to show it.
func TestPinnedNamesTheImageBehindTheTag(t *testing.T) {
	const repo, digest = "ghcr.io/cirruslabs/macos-tahoe-vanilla", "sha256:eeec54bf"
	home := cacheAt(t)
	link(t, home, repo, "latest", digest)

	assert.Equal(t, repo+"@"+digest, Pinned(repo+":latest"))
	assert.Equal(t, digest, ImageDigest(repo+":latest"))
}

// "I COULD NOT FIND OUT" IS ANSWERED AS NOTHING AT ALL, never as a
// digest somebody made up. tart's cache layout is its layout and not its
// contract, so every way of failing to read it answers the same way.
func TestAnUnreadableImageAnswersNothing(t *testing.T) {
	cacheAt(t)
	assert.Empty(t, Pinned("ghcr.io/cirruslabs/macos-nosuch-vanilla:latest"), "never pulled")
	assert.Empty(t, Pinned("ghcr.io/cirruslabs/macos-tahoe-vanilla@sha256:abc"), "already pinned")
	assert.Empty(t, Pinned("no-tag-at-all"), "not a tagged reference")
}

// THE MOMENT MATTERS, so provenance is WRITTEN and never re-derived. A
// base outlives every change that runs on it, and `latest` will have
// moved by the time a verdict wants to name the image — so the answer
// has to be the one recorded when the base was built.
func TestABasesProvenanceIsWrittenAndReadBack(t *testing.T) {
	cacheAt(t)
	const pinned = "ghcr.io/cirruslabs/macos-tahoe-vanilla@sha256:eeec54bf"

	assert.Empty(t, BaseImage("dockhand-base-tahoe"), "a base provisioned before this says so by saying nothing")
	NoteBase("dockhand-base-tahoe", pinned)
	assert.Equal(t, pinned, BaseImage("dockhand-base-tahoe"))

	ForgetBase("dockhand-base-tahoe")
	assert.Empty(t, BaseImage("dockhand-base-tahoe"), "provenance goes when the base does")
}

// A DIGEST NOBODY COULD RESOLVE IS NOT RECORDED AS BLANK PROVENANCE:
// nothing is written, so the read is the same "unknown" an older base
// gives, and both are honest.
func TestAnUnknownImageWritesNoProvenance(t *testing.T) {
	cacheAt(t)
	NoteBase("dockhand-base-tahoe", "")
	assert.Empty(t, BaseImage("dockhand-base-tahoe"))
}
