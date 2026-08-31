package checksums

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	// Single-distfile form.
	rs, err := Parse([]string{"rmd160", "aa", "sha256", "bb", "size", "9"})
	require.NoError(t, err)
	assert.Equal(t, []Recorded{
		{"", "rmd160", "aa"}, {"", "sha256", "bb"}, {"", "size", "9"},
	}, rs)

	// Multi-distfile form with filenames.
	rs, err = Parse([]string{
		"foo-1.0.tar.gz", "sha256", "bb", "size", "9",
		"bar-1.0.tar.gz", "sha256", "cc", "size", "8",
	})
	require.NoError(t, err)
	assert.Equal(t, []Recorded{
		{"foo-1.0.tar.gz", "sha256", "bb"}, {"foo-1.0.tar.gz", "size", "9"},
		{"bar-1.0.tar.gz", "sha256", "cc"}, {"bar-1.0.tar.gz", "size", "8"},
	}, rs)

	_, err = Parse([]string{"sha256"})
	require.ErrorIs(t, err, ErrMalformed)
}

func TestSumsValue(t *testing.T) {
	s := Sums{Rmd160: "aa", Sha256: "bb", Size: 9}
	for typ, want := range map[string]string{"rmd160": "aa", "sha256": "bb", "size": "9"} {
		got, ok := s.Value(typ)
		require.True(t, ok, typ)
		assert.Equal(t, want, got, typ)
	}
	_, ok := s.Value("md5")
	assert.False(t, ok)
}

func TestReplacements(t *testing.T) {
	fresh := Sums{Rmd160: "aa", Sha256: "bb", Size: 12}

	// The unnamed single-distfile form takes the sole entry.
	reps, err := Replacements(
		[]Recorded{{Type: "rmd160", Value: "old-a"}, {Type: "sha256", Value: "old-b"}, {Type: "size", Value: "9"}},
		map[string]Sums{"foo-1.0.tar.gz": fresh})
	require.NoError(t, err)
	assert.Equal(t, []Replacement{
		{Old: "old-a", New: "aa", Reason: "checksum rmd160"},
		{Old: "old-b", New: "bb", Reason: "checksum sha256"},
		{Old: "9", New: "12", Reason: "checksum size"},
	}, reps)

	// The named form resolves per file.
	reps, err = Replacements(
		[]Recorded{{File: "a.tar.gz", Type: "sha256", Value: "x"}, {File: "b.tar.gz", Type: "sha256", Value: "y"}},
		map[string]Sums{"a.tar.gz": {Sha256: "AA"}, "b.tar.gz": {Sha256: "BB"}})
	require.NoError(t, err)
	assert.Equal(t, "AA", reps[0].New)
	assert.Equal(t, "BB", reps[1].New)
}

func TestReplacementsUnresolvable(t *testing.T) {
	one := map[string]Sums{"f.tar.gz": {Sha256: "x"}}
	cases := map[string]struct {
		recorded []Recorded
		sums     map[string]Sums
	}{
		"legacy type cannot be recomputed": {[]Recorded{{Type: "md5", Value: "e"}}, one},
		"unknown type":                     {[]Recorded{{Type: "sha512", Value: "e"}}, one},
		"named file with no sums":          {[]Recorded{{File: "other.tar.gz", Type: "sha256", Value: "e"}}, one},
		"unnamed with several distfiles": {[]Recorded{{Type: "sha256", Value: "e"}},
			map[string]Sums{"a": {}, "b": {}}},
	}
	for name, c := range cases {
		_, err := Replacements(c.recorded, c.sums)
		require.ErrorIs(t, err, ErrUnresolved, name)
	}
}

// A block appends one checksum record per distfile it supplies. Those
// literals live inside the block, which is replaced wholesale, so they
// must not be looked for among the checksums command's words.
func TestOwnRecordsDropsBlockSuppliedEntries(t *testing.T) {
	recorded := []Recorded{
		{File: "tokei-13.0.0.tar.gz", Type: "sha256", Value: "aaa"},
		{File: "libc-0.2.156.crate", Type: "sha256", Value: "bbb"},
		{File: "bitflags-2.6.0.crate", Type: "sha256", Value: "ccc"},
	}
	got := ForFiles(recorded, []string{"tokei-13.0.0.tar.gz"})
	require.Len(t, got, 1)
	assert.Equal(t, "tokei-13.0.0.tar.gz", got[0].File)
}

// The single-distfile form carries no name, and only the port itself
// writes it.
func TestOwnRecordsKeepsTheUnnamedForm(t *testing.T) {
	got := ForFiles([]Recorded{{Type: "sha256", Value: "aaa"}}, []string{"foo-1.0.tar.gz"})
	require.Len(t, got, 1)
}
