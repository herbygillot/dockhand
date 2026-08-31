package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two entries anchored against running systems rather than recall.
func TestMeasuredAnchors(t *testing.T) {
	// A guest reporting macOS 15.7.7 on Darwin 24.6.0.
	seq, ok := ByProduct("15")
	require.True(t, ok)
	assert.Equal(t, "Sequoia", seq.Name)
	assert.Equal(t, 24, seq.Darwin)

	// A host reporting macOS 26.6.2 on Darwin 25.6.0 — where Apple's
	// product numbering jumps and Darwin's does not.
	tahoe, ok := ByProduct("26")
	require.True(t, ok)
	assert.Equal(t, "Tahoe", tahoe.Name)
	assert.Equal(t, 25, tahoe.Darwin)
	assert.Equal(t, seq.Darwin+1, tahoe.Darwin, "Darwin continues across the product jump")
}

func TestParseAcceptsEveryNameForOneThing(t *testing.T) {
	for _, in := range []string{"sequoia", "Sequoia", "SEQUOIA", "15", "macos-15", " sequoia "} {
		r, err := Parse(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, 24, r.Darwin, "input %q", in)
	}
}

// The 10.x releases are why Product is a string: an int major would
// drop everything before Big Sur.
func TestParseHandlesTheTenPointReleases(t *testing.T) {
	r, err := Parse("10.15")
	require.NoError(t, err)
	assert.Equal(t, "Catalina", r.Name)
	assert.Equal(t, 19, r.Darwin)
}

func TestNamesWithSpacesFoldEitherWay(t *testing.T) {
	for _, in := range []string{"High Sierra", "highsierra", "HIGH SIERRA", "Big Sur", "bigsur"} {
		_, err := Parse(in)
		require.NoError(t, err, "input %q", in)
	}
}

// A bare number is a product version. The ranges overlap — 25 is
// Tahoe's kernel and 26 its product — so the ambiguity has to resolve
// one way, and it resolves the way users and CI labels write.
func TestBareNumbersAreProductVersions(t *testing.T) {
	r, err := Parse("26")
	require.NoError(t, err)
	assert.Equal(t, "Tahoe", r.Name)

	// 25 is no product version, so it does not resolve, even though it
	// is a Darwin major.
	_, err = Parse("25")
	require.ErrorIs(t, err, ErrUnknownRelease)

	byKernel, ok := ByDarwin(25)
	require.True(t, ok, "the caller who holds a kernel major says so")
	assert.Equal(t, "Tahoe", byKernel.Name)
}

func TestFromUnameReadsAKernelVersion(t *testing.T) {
	r, err := FromUname("24.6.0")
	require.NoError(t, err)
	assert.Equal(t, "Sequoia", r.Name)

	_, err = FromUname("not-a-version")
	require.ErrorIs(t, err, ErrUnknownRelease)
}

// An unknown release is an error, never the nearest guess: verifying on
// a platform dockhand does not know is not verifying.
func TestUnknownReleaseIsRefused(t *testing.T) {
	for _, in := range []string{"", "cheetah", "99", "macos-99"} {
		_, err := Parse(in)
		require.ErrorIs(t, err, ErrUnknownRelease, "input %q", in)
	}
}

func TestTableIsOrderedAndConsistent(t *testing.T) {
	for i, r := range Releases {
		assert.NotEmpty(t, r.Name)
		assert.NotEmpty(t, r.Product)
		if i > 0 {
			assert.Equal(t, Releases[i-1].Darwin+1, r.Darwin,
				"Darwin majors are contiguous; %s breaks the run", r.Name)
		}
		byName, ok := ByName(r.Name)
		require.True(t, ok)
		assert.Equal(t, r, byName, "every entry must be findable by its own name")
	}
}

// The forgiving forms: whatever separator a tool prefers, whatever
// precision a person has to hand, with or without the macos prefix.
func TestParseIsForgivingAboutSpelling(t *testing.T) {
	for input, want := range map[string]string{
		"big-sur":       "Big Sur",
		"big_sur":       "Big Sur",
		"BIG SUR":       "Big Sur",
		"el-capitan":    "El Capitan",
		"macos-sequoia": "Sequoia",
		"macos ventura": "Ventura",
		"macos-13":      "Ventura",
		"macos-14":      "Sonoma",
		"14.5":          "Sonoma",
		"15.7.7":        "Sequoia",
		"26.0":          "Tahoe",
		"11.7.10":       "Big Sur",
		"10.15.4":       "Catalina",
		"10.13.6":       "High Sierra",
	} {
		r, err := Parse(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, r.Name, input)
	}
}

// Point precision resolves the release; nonsense still refuses. 10.16
// never shipped (it is Big Sur's compatibility fiction), and a bare
// "macos" names nothing.
func TestForgivenessIsNotGuessing(t *testing.T) {
	for _, input := range []string{"10.16", "27.1", "9.5", "macos", "sequoia-vanilla"} {
		_, err := Parse(input)
		require.ErrorIs(t, err, ErrUnknownRelease, input)
	}
}
