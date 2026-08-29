package checksums

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o644))
	sums, err := HashFile(path)
	require.NoError(t, err)
	assert.Equal(t, "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03", sums.Sha256)
	assert.Equal(t, int64(6), sums.Size)
	assert.Len(t, sums.Rmd160, 40)

	_, err = HashFile(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
}
