package binaryarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// An archive becomes a site's entry once it is the archive it was kept
// as: its signify signature, and an RSA one that verifies as MacPorts
// verifies one with openssl, named as MacPorts fetches them beside it.
// One that isn't what it was kept as, or can't be named in a site, is
// refused.
func TestAnArchiveIsSignedAsASitesEntry(t *testing.T) {
	keys, err := LoadKeys(t.TempDir())
	require.NoError(t, err)
	data := []byte("libharbor's archive")
	kept := filepath.Join(t.TempDir(), "kept")
	require.NoError(t, os.WriteFile(kept, data, 0o644))
	sum := sha256.Sum256(data)
	name := "libharbor-4_0.darwin_25.arm64.tbz2"
	archive := Archive{Port: "libharbor", Name: name, Digest: "sha256:" + hex.EncodeToString(sum[:]), Path: kept}

	entry, err := Sign(t.Context(), keys, archive)
	require.NoError(t, err)
	require.Equal(t, "libharbor", entry.Port)
	id := keys.id()
	require.Equal(t, map[string]string{name: kept, name + ".sig": kept + "." + id + ".sig", name + ".rmd160": kept + "." + id + ".rmd160"}, entry.Files, "beside the archive, named by the keys")
	signature, err := os.ReadFile(entry.Files[name+".sig"])
	require.NoError(t, err)
	require.Equal(t, keys.Signify.Sign(data, "verify with dockhand.pub"), signature)
	public := filepath.Join(t.TempDir(), RSAPublicKey)
	require.NoError(t, os.WriteFile(public, keys.PublicKeys()[RSAPublicKey], 0o644))
	out, err := exec.CommandContext(t.Context(), "/usr/bin/openssl", "dgst", "-ripemd160", "-verify", public, "-signature", entry.Files[name+".rmd160"], kept).CombinedOutput()
	require.NoError(t, err, "%s", out)
	// The very signature openssl makes, which PKCS #1 v1.5 makes the same
	// every time: Go's own RIPEMD-160 DigestInfo would fail the verify.
	theirs := filepath.Join(t.TempDir(), "theirs.rmd160")
	out, err = exec.CommandContext(t.Context(), "/usr/bin/openssl", "dgst", "-ripemd160", "-sign", keys.RSA, "-out", theirs, kept).CombinedOutput()
	require.NoError(t, err, "%s", out)
	ours, err := os.ReadFile(entry.Files[name+".rmd160"])
	require.NoError(t, err)
	want, err := os.ReadFile(theirs)
	require.NoError(t, err)
	require.Equal(t, want, ours)

	// Signed once: a second entry is the same files, not made again.
	before, err := os.Stat(entry.Files[name+".sig"])
	require.NoError(t, err)
	again, err := Sign(t.Context(), keys, archive)
	require.NoError(t, err)
	require.Equal(t, entry, again)
	after, err := os.Stat(entry.Files[name+".sig"])
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime())

	// An empty archive, which can't be mapped, is signed all the same.
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	none := sha256.Sum256(nil)
	_, err = Sign(t.Context(), keys, Archive{Port: "libharbor", Name: name, Digest: "sha256:" + hex.EncodeToString(none[:]), Path: empty})
	require.NoError(t, err)
	require.Equal(t, "/var/tmp/dockhand-archives/libharbor/"+name, EntryPath("/var/tmp/dockhand-archives", "libharbor", name))
	require.Equal(t, keys.Signify.PublicKey("dockhand archives"), keys.PublicKeys()[SignifyPublicKey])

	changed := archive
	changed.Digest = "sha256:" + hex.EncodeToString(make([]byte, 32))
	_, err = Sign(t.Context(), keys, changed)
	require.ErrorContains(t, err, "isn't the "+changed.Digest+" it was kept as")
	for _, bad := range []Archive{{Port: "../etc", Name: name}, {Port: "lib#harbor", Name: name}, {Port: "libharbor", Name: "../../etc/passwd"}} {
		_, err = Sign(t.Context(), keys, Archive{Port: bad.Port, Name: bad.Name, Digest: archive.Digest, Path: kept})
		require.ErrorContains(t, err, "can't be a site's entry", "%+v", bad)
		require.False(t, Installable(bad.Port, bad.Name))
	}
	require.True(t, Installable("libharbor", name))
}
