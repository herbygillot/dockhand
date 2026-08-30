package bump

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

func TestSuppliedDistfilesNamesWhatTheBlockContributes(t *testing.T) {
	got, err := suppliedDistfiles(info.Vendored{CargoCrates: "libc 0.2.156 a5f43f1 bitflags 2.6.0 b048fb6"})
	require.NoError(t, err)
	assert.Equal(t, []string{"libc-0.2.156.crate", "bitflags-2.6.0.crate"}, got)
}

func TestSuppliedDistfilesEmptyWithoutABlock(t *testing.T) {
	got, err := suppliedDistfiles(info.Vendored{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A block appends one checksum record per distfile it supplies. Those
// literals live inside the block, which is replaced wholesale, so they
// must not be looked for among the checksums command's words.
func TestOwnRecordsDropsBlockSuppliedEntries(t *testing.T) {
	recorded := []checksums.Recorded{
		{File: "tokei-13.0.0.tar.gz", Type: "sha256", Value: "aaa"},
		{File: "libc-0.2.156.crate", Type: "sha256", Value: "bbb"},
		{File: "bitflags-2.6.0.crate", Type: "sha256", Value: "ccc"},
	}
	got := ownRecords(recorded, []string{"tokei-13.0.0.tar.gz"})
	require.Len(t, got, 1)
	assert.Equal(t, "tokei-13.0.0.tar.gz", got[0].File)
}

// The single-distfile form carries no name, and only the port itself
// writes it.
func TestOwnRecordsKeepsTheUnnamedForm(t *testing.T) {
	got := ownRecords([]checksums.Recorded{{Type: "sha256", Value: "aaa"}}, []string{"foo-1.0.tar.gz"})
	require.Len(t, got, 1)
}

func valsWithPatch(t *testing.T, body string) info.Values {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "patch-x.diff"), []byte(body), 0o644))
	return info.Values{Filespath: dir, Patchfiles: []string{"patch-x.diff"}}
}

// The patch's own diff headers are read rather than its name guessed at:
// a patch named for something else can still rewrite the lockfile.
func TestPatchesLockfileReadsDiffHeadersNotNames(t *testing.T) {
	vals := valsWithPatch(t, `--- a/Cargo.lock
+++ b/Cargo.lock
@@ -1 +1 @@
-version = 3
+version = 4
`)
	pf, ok := patchesLockfile(vals)
	assert.True(t, ok)
	assert.Equal(t, "patch-x.diff", pf)
}

func TestPatchesLockfileIgnoresPatchesElsewhere(t *testing.T) {
	vals := valsWithPatch(t, `--- a/src/main.rs
+++ b/src/main.rs
@@ -1 +1 @@
-fn main() {}
+fn main() { println!("hi") }
`)
	_, ok := patchesLockfile(vals)
	assert.False(t, ok)
}

// A patchfile that cannot be read proves nothing, and the point is to
// prove the lockfile is untouched.
func TestPatchesLockfileTreatsUnreadableAsTouching(t *testing.T) {
	vals := info.Values{Filespath: t.TempDir(), Patchfiles: []string{"absent.diff"}}
	pf, ok := patchesLockfile(vals)
	assert.True(t, ok)
	assert.Equal(t, "absent.diff", pf)
}

func TestPatchesLockfileNoPatches(t *testing.T) {
	_, ok := patchesLockfile(info.Values{})
	assert.False(t, ok)
}
