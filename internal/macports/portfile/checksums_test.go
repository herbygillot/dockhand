package portfile

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestChecksumEditingPreservesFormattingAndRefusesAmbiguousSources(t *testing.T) {
	input := []byte("# preserved\nchecksums   sha256 old \\\n    size 1\n# trailing\n")
	output, values, err := ReplaceChecksums(input, "sha256 old size 1", Checksum{SHA256: "new", Size: 42})
	require.NoError(t, err)
	require.Equal(t, "# preserved\nchecksums   sha256 new \\\n    size 42\n# trailing\n", string(output))
	require.Equal(t, "sha256 new size 42", values)
	for _, test := range []struct{ source, evaluated string }{
		{"checksums source.tar.gz sha256 old\n", "source.tar.gz sha256 old"},
		{"checksums sha256 $checksum\n", "sha256 old"},
		{"checksums sha256 old\nchecksums sha256 other\n", "sha256 other"},
		{"checksums sha256 old sha256 other\n", "sha256 old sha256 other"},
		{"checksums sha256 old\n", "sha256 overridden"},
	} {
		_, _, err := ReplaceChecksums([]byte(test.source), test.evaluated, Checksum{SHA256: "new"})
		require.ErrorIs(t, err, ErrUnsupported, test.source)
	}
}

func TestNamedChecksumGroupsPreserveExpressionsAndAssociateByName(t *testing.T) {
	src := []byte("checksums ${distname}.tar.gz sha256 a size 1 \\n  extra.tar.gz sha256 b size 2\n")
	// Use an actual Tcl line continuation.
	src = []byte(strings.ReplaceAll(string(src), "\\n", "\\\n"))
	out, values, err := ReplaceChecksums(src, "app-2.tar.gz sha256 a size 1 extra.tar.gz sha256 b size 2", Checksum{Name: "extra.tar.gz", SHA256: "extra", Size: 20}, Checksum{Name: "app-2.tar.gz", SHA256: "app", Size: 10})
	require.NoError(t, err)
	require.Contains(t, string(out), "${distname}.tar.gz sha256 app size 10")
	require.Contains(t, string(out), "extra.tar.gz sha256 extra size 20")
	require.Equal(t, "app-2.tar.gz sha256 app size 10 extra.tar.gz sha256 extra size 20", values)
	for _, downloads := range [][]Checksum{{{Name: "wrong"}}, {{Name: "app-2.tar.gz"}, {Name: "wrong"}}, {{Name: "app-2.tar.gz"}, {Name: "app-2.tar.gz"}}} {
		_, _, err = ReplaceChecksums(src, "app-2.tar.gz sha256 a size 1 extra.tar.gz sha256 b size 2", downloads...)
		require.ErrorIs(t, err, ErrUnsupported)
	}
}

func TestLegacyChecksumGroupsAreRewrittenInTheirOwnLayout(t *testing.T) {
	sums := Checksum{SHA256: "S", RMD160: "R", Size: 7}
	for _, test := range []struct{ name, source, evaluated, want string }{
		{"aligned md5 sha1 rmd160", "checksums           md5     aaaa \\\n                    sha1    bbbb \\\n                    rmd160  cccc\n", "md5 aaaa sha1 bbbb rmd160 cccc", "checksums           rmd160  R \\\n                    sha256  S \\\n                    size    7\n"},
		{"md5 beside sha256", "checksums md5 aaaa \\\n    rmd160 cccc \\\n    sha256 dddd\n", "md5 aaaa rmd160 cccc sha256 dddd", "checksums rmd160 R \\\n    sha256 S \\\n    size 7\n"},
		{"single legacy pair continues under itself", "checksums\tmd5 aaaa\n", "md5 aaaa", "checksums\trmd160 R \\\n         \tsha256 S \\\n         \tsize 7\n"},
		{"single-line legacy stays on one line", "checksums sha1 aaaa rmd160 cccc\n", "sha1 aaaa rmd160 cccc", "checksums rmd160 R sha256 S size 7\n"},
		{"rmd160 alone gains sha256", "checksums rmd160 cccc\n", "rmd160 cccc", "checksums rmd160 R \\\n          sha256 S \\\n          size 7\n"},
		{"named legacy group", "checksums a.zip md5 aaaa rmd160 cccc\n", "a.zip md5 aaaa rmd160 cccc", "checksums a.zip rmd160 R sha256 S size 7\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sums.Name = ""
			if strings.HasPrefix(test.evaluated, "a.zip") {
				sums.Name = "a.zip"
			}
			out, values, err := ReplaceChecksums([]byte(test.source), test.evaluated, sums)
			require.NoError(t, err)
			require.Equal(t, test.want, string(out))
			want := "rmd160 R sha256 S size 7"
			if sums.Name != "" {
				want = "a.zip " + want
			}
			require.Equal(t, want, values)
		})
	}
	// A current group keeps its own order and algorithms.
	out, values, err := ReplaceChecksums([]byte("checksums sha256 old rmd160 r\n"), "sha256 old rmd160 r", sums)
	require.NoError(t, err)
	require.Equal(t, "checksums sha256 S rmd160 R\n", string(out))
	require.Equal(t, "sha256 S rmd160 R", values)
	require.False(t, LegacyChecksums([]string{"sha256", "size"}))
	require.True(t, LegacyChecksums([]string{"sha1", "sha256", "size"}))
}

func TestKeepingLegacyChecksumsRefreshesEveryWrittenValue(t *testing.T) {
	src := []byte("checksums           md5     aaaa \\\n                    sha1    bbbb \\\n                    rmd160  cccc\n")
	sums := Checksum{SHA256: "S", RMD160: "R", MD5: "M", SHA1: "H", Size: 7}
	out, values, err := ReplaceChecksumsKeeping(src, "md5 aaaa sha1 bbbb rmd160 cccc", true, sums)
	require.NoError(t, err)
	require.Equal(t, "checksums           md5     M \\\n                    sha1    H \\\n                    rmd160  R\n", string(out))
	require.Equal(t, "md5 M sha1 H rmd160 R", values)
	out, _, err = ReplaceChecksumsKeeping(src, "md5 aaaa sha1 bbbb rmd160 cccc", false, sums)
	require.NoError(t, err)
	require.NotContains(t, string(out), "md5")
}
