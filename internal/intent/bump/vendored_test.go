package bump

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
)

func TestSuppliedDistfilesNamesWhatTheBlockContributes(t *testing.T) {
	got, err := suppliedDistfiles(t.Context(), port.Handle{},
		info.Vendored{CargoCrates: "libc 0.2.156 a5f43f1 bitflags 2.6.0 b048fb6"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"libc-0.2.156.crate", "bitflags-2.6.0.crate"}, got)
}

func TestSuppliedDistfilesEmptyWithoutABlock(t *testing.T) {
	got, err := suppliedDistfiles(t.Context(), port.Handle{}, info.Vendored{}, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
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
