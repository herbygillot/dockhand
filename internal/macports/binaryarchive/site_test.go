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

	directory := t.TempDir()
	entry, err := Sign(t.Context(), keys, archive, directory)
	require.NoError(t, err)
	require.Equal(t, "libharbor", entry.Port)
	require.Equal(t, map[string]string{name: kept, name + ".sig": filepath.Join(directory, name+".sig"), name + ".rmd160": filepath.Join(directory, name+".rmd160")}, entry.Files)
	signature, err := os.ReadFile(entry.Files[name+".sig"])
	require.NoError(t, err)
	require.Equal(t, keys.Signify.Sign(data, "verify with dockhand.pub"), signature)
	public := filepath.Join(t.TempDir(), RSAPublicKey)
	require.NoError(t, os.WriteFile(public, keys.PublicKeys()[RSAPublicKey], 0o644))
	out, err := exec.CommandContext(t.Context(), "/usr/bin/openssl", "dgst", "-ripemd160", "-verify", public, "-signature", entry.Files[name+".rmd160"], kept).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Equal(t, "/var/tmp/dockhand-archives/libharbor/"+name, EntryPath("/var/tmp/dockhand-archives", "libharbor", name))
	require.Equal(t, keys.Signify.PublicKey("dockhand archives"), keys.PublicKeys()[SignifyPublicKey])

	changed := archive
	changed.Digest = "sha256:" + hex.EncodeToString(make([]byte, 32))
	_, err = Sign(t.Context(), keys, changed, t.TempDir())
	require.ErrorContains(t, err, "isn't the "+changed.Digest+" it was kept as")
	for _, bad := range []Archive{{Port: "../etc", Name: name}, {Port: "lib#harbor", Name: name}, {Port: "libharbor", Name: "../../etc/passwd"}} {
		_, err = Sign(t.Context(), keys, Archive{Port: bad.Port, Name: bad.Name, Digest: archive.Digest, Path: kept}, t.TempDir())
		require.ErrorContains(t, err, "can't be a site's entry", "%+v", bad)
		require.False(t, Installable(bad.Port, bad.Name))
	}
	require.True(t, Installable("libharbor", name))
}
