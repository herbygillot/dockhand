package prepare

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChecksumEditingPreservesFormattingAndRefusesAmbiguousSources(t *testing.T) {
	input := []byte("# preserved\nchecksums   sha256 old \\\n    size 1\n# trailing\n")
	output, values, err := replaceChecksums(input, "sha256 old size 1", Download{SHA256: "new", Size: 42})
	require.NoError(t, err)
	require.Equal(t, "# preserved\nchecksums   sha256 new \\\n    size 42\n# trailing\n", string(output))
	require.Equal(t, "sha256 new size 42", values)
	for _, test := range []struct{ source, evaluated string }{
		{"checksums source.tar.gz sha256 old\n", "source.tar.gz sha256 old"},
		{"checksums sha256 $checksum\n", "sha256 old"},
		{"checksums sha256 old\nchecksums sha256 other\n", "sha256 other"},
		{"checksums sha256 old sha256 other\n", "sha256 old sha256 other"},
		{"checksums rmd160 old\n", "rmd160 old"},
		{"checksums sha256 old\n", "sha256 overridden"},
	} {
		_, _, err := replaceChecksums([]byte(test.source), test.evaluated, Download{SHA256: "new"})
		require.ErrorIs(t, err, ErrUnsupported, test.source)
	}
}
