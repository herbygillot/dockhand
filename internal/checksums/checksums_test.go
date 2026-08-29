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
