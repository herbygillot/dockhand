package prepare

import (
	"github.com/stretchr/testify/require"
	"strings"
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

func TestNamedChecksumGroupsPreserveExpressionsAndAssociateByName(t *testing.T) {
	src := []byte("checksums ${distname}.tar.gz sha256 a size 1 \\n  extra.tar.gz sha256 b size 2\n")
	// Use an actual Tcl line continuation.
	src = []byte(strings.ReplaceAll(string(src), "\\n", "\\\n"))
	out, values, err := replaceChecksums(src, "app-2.tar.gz sha256 a size 1 extra.tar.gz sha256 b size 2", Download{Name: "extra.tar.gz", SHA256: "extra", Size: 20}, Download{Name: "app-2.tar.gz", SHA256: "app", Size: 10})
	require.NoError(t, err)
	require.Contains(t, string(out), "${distname}.tar.gz sha256 app size 10")
	require.Contains(t, string(out), "extra.tar.gz sha256 extra size 20")
	require.Equal(t, "app-2.tar.gz sha256 app size 10 extra.tar.gz sha256 extra size 20", values)
	for _, downloads := range [][]Download{{{Name: "wrong"}}, {{Name: "app-2.tar.gz"}, {Name: "wrong"}}, {{Name: "app-2.tar.gz"}, {Name: "app-2.tar.gz"}}} {
		_, _, err = replaceChecksums(src, "app-2.tar.gz sha256 a size 1 extra.tar.gz sha256 b size 2", downloads...)
		require.ErrorIs(t, err, ErrUnsupported)
	}
}
