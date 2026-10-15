package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The archive names a checksums declaration lists literally move with the
// version, wherever the declaration is; the algorithms and values don't.
func TestChecksumFileNamesMoveWithTheVersion(t *testing.T) {
	src := "go_toolchain.setup  1.26.9\n\nchecksums           \\\n                    go1.26.9.src.tar.gz \\\n                    rmd160  aaa \\\n                    sha256  1.26.9 \\\n                    size    1269\n" +
		"platform darwin {\n    checksums-append go1.26.9.darwin-arm64.tar.gz sha256 bbb size 2\n}\n"
	out, err := RenameChecksumFiles([]byte(src), "1.26.9", "1.27.0")
	require.NoError(t, err)
	require.Contains(t, string(out), "go1.27.0.src.tar.gz")
	require.Contains(t, string(out), "go1.27.0.darwin-arm64.tar.gz")
	require.Contains(t, string(out), "sha256  1.26.9", "a value is left as it is, whatever it spells")
	require.Contains(t, string(out), "go_toolchain.setup  1.26.9", "the version itself is the version edit's")
	same, err := RenameChecksumFiles([]byte(src), "1.26.9", "1.26.9")
	require.NoError(t, err)
	require.Equal(t, src, string(same))
}
