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

func TestVerify(t *testing.T) {
	s := Sums{Rmd160: "aa", Sha256: "bb", Size: 9}
	require.NoError(t, Verify(s, []Recorded{
		{"", "rmd160", "aa"}, {"", "sha256", "bb"}, {"", "size", "9"},
	}))

	err := Verify(s, []Recorded{{"", "sha256", "WRONG"}})
	require.ErrorIs(t, err, ErrMismatch)
	require.ErrorContains(t, err, "recorded WRONG")

	// A legacy type cannot be verified: silence must never imply a
	// check that did not happen.
	err = Verify(s, []Recorded{{"", "md5", "ee"}})
	require.ErrorIs(t, err, ErrMismatch)
	require.ErrorContains(t, err, "unverifiable")
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
