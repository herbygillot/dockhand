package signify

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A key's files are signify's: two lines each, the key's number tying a
// signature to its public key. A key read back is the same key.
func TestAKeysFilesAreSignifys(t *testing.T) {
	key, err := Generate()
	require.NoError(t, err)
	public := key.PublicKey("dockhand archives")
	signature := key.Sign([]byte("an archive"), "verify with dockhand archives")
	for _, f := range []struct {
		data []byte
		size int
	}{{public, 2 + 8 + 32}, {signature, 2 + 8 + 64}} {
		lines := strings.Split(strings.TrimSuffix(string(f.data), "\n"), "\n")
		require.Len(t, lines, 2)
		require.True(t, strings.HasPrefix(lines[0], "untrusted comment: "))
		decoded, err := base64.StdEncoding.DecodeString(lines[1])
		require.NoError(t, err)
		require.Len(t, decoded, f.size)
		require.Equal(t, "Ed", string(decoded[:2]))
		require.Equal(t, key.Number[:], decoded[2:10])
	}
	data, err := key.MarshalBinary()
	require.NoError(t, err)
	var read Key
	require.NoError(t, read.UnmarshalBinary(data))
	require.Equal(t, key, read)
	require.Error(t, read.UnmarshalBinary(data[1:]))
}

// MacPorts' own signify accepts what the key signs, as it checks an
// archive fetched from an archive site, and refuses a file changed after.
func TestMacPortsSignifyVerifiesWhatAKeySigns(t *testing.T) {
	tclsh := testsupport.MacPortsTclsh(t)
	signify := filepath.Join(filepath.Dir(filepath.Dir(tclsh)), "libexec", "macports", "bin", "signify")
	if _, err := os.Stat(signify); err != nil {
		t.Skipf("MacPorts' signify is not beside DOCKHAND_TEST_MACPORTS_TCLSH: %v", err)
	}
	key, err := Generate()
	require.NoError(t, err)
	root := t.TempDir()
	archive := filepath.Join(root, "libharbor-4_0.darwin_25.arm64.tbz2")
	require.NoError(t, os.WriteFile(archive, []byte("libharbor's archive"), 0o644))
	require.NoError(t, os.WriteFile(archive+".sig", key.Sign([]byte("libharbor's archive"), "verify with dockhand.pub"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dockhand.pub"), key.PublicKey("dockhand archives"), 0o644))
	verify := func() error {
		return exec.CommandContext(t.Context(), signify, "-V", "-q", "-p", filepath.Join(root, "dockhand.pub"), "-x", archive+".sig", "-m", archive).Run()
	}
	require.NoError(t, verify())
	require.NoError(t, os.WriteFile(archive, []byte("libharbor's archive, changed"), 0o644))
	require.Error(t, verify(), "a changed archive fails")
}
