package binaryarchive

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The archive keys are made once, readable by their owner alone, and
// makers racing for them all get the first one's: the signify key and the
// RSA key, whose public half is PEM as openssl writes one.
func TestTheArchiveKeysAreMadeOnce(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "ssh")
	found := make([]Keys, 8)
	var makers sync.WaitGroup
	for i := range found {
		makers.Go(func() {
			made, err := LoadKeys(directory)
			require.NoError(t, err)
			found[i] = made
		})
	}
	makers.Wait()
	for _, made := range found[1:] {
		require.Equal(t, found[0], made)
	}
	for _, name := range []string{"archives.key", "archives-rsa.pem"} {
		info, err := os.Stat(filepath.Join(directory, name))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
	}
	require.True(t, strings.HasPrefix(string(found[0].RSAPublic), "-----BEGIN PUBLIC KEY-----\n"))
	again, err := LoadKeys(directory)
	require.NoError(t, err)
	require.Equal(t, found[0], again)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 2, "no maker's temporary file is left")
}
