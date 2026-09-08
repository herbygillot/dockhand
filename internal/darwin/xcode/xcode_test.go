package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/platform"
)

func release(t *testing.T, name string) platform.Release {
	t.Helper()
	r, err := platform.Parse(name)
	require.NoError(t, err)
	return r
}

// The recommendation and the bound are one table read from two ends,
// which is the whole reason they live in one package: a version
// recommended above its own release's ceiling is a guest told to
// install something it cannot run.
//
// It is checked with base's own version ordering rather than by
// eyeballing the numbers, because that is the ordering the archive
// picker uses to hold a candidate to the bound — 26.3 against 26.4 is
// the pair a text comparison gets right by luck and 4.0 against 30.0
// is the pair it gets wrong.
func TestNoRecommendationIsAboveItsOwnReleasesBound(t *testing.T) {
	for _, r := range platform.Releases {
		version, capped := Recommended(r)
		bound := Bound(r)
		if !capped {
			assert.Empty(t, version, "%s: an uncapped release takes the newest Xcode, and names none", r.Name)
			assert.Empty(t, bound, "%s: a release with a bound has a recommendation below it", r.Name)
			continue
		}
		require.NotEmpty(t, bound, "%s: a capped recommendation is capped BY something", r.Name)
		assert.Negative(t, macports.VerCmp(version, bound),
			"%s: Xcode %s is not below the %s this release cannot run", r.Name, version, bound)
	}
}

// The four releases the table caps, verbatim, so a bound raised without
// its recommendation fails here rather than in a guest.
func TestTheCappedReleasesAreNamedAndTheNewestIsNot(t *testing.T) {
	for name, want := range map[string]string{
		"monterey": "14.2",
		"ventura":  "15.2",
		"sonoma":   "16.2",
		"sequoia":  "26.3",
	} {
		version, capped := Recommended(release(t, name))
		assert.True(t, capped, name)
		assert.Equal(t, want, version, name)
	}

	// Tahoe is the newest release this repo's table knows, and "no
	// recommendation" here means "take the newest", which is why capped
	// is a fact of its own rather than an inference from an empty
	// string.
	version, capped := Recommended(release(t, "tahoe"))
	assert.False(t, capped)
	assert.Empty(t, version)
	assert.Empty(t, Bound(release(t, "tahoe")))
}

// A release older than anything Apple still ships an Xcode line for
// gets no answer, and that is an answer: the caller refuses rather than
// installing whatever it found.
func TestAReleaseThisTableDoesNotCoverGetsNoBound(t *testing.T) {
	assert.Empty(t, Bound(release(t, "snow leopard")))
	version, capped := Recommended(release(t, "snow leopard"))
	assert.Empty(t, version)
	assert.False(t, capped)
}
