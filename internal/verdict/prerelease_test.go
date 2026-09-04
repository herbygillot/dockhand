package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// One heuristic, two callers. The planners ask it about a version they
// are being offered and the mint asks it about the target a change was
// minted against; these are the answers both of them get.
//
// upstream.Stable and upstream's releaseBase delegate here, and their
// own tests still stand — which is the point of the move: two regexps
// would have been two heuristics the first time either was fixed.
func TestPrereleaseReadsTheTokensAndNotTheirNeighbours(t *testing.T) {
	for version, want := range map[string]bool{
		"1.17.0":            false,
		"1.8.1":             false,
		"2026.9.1":          false,
		"1.17.0-rc.3":       true,
		"2.3.2-beta":        true,
		"3.0.0-pre":         true,
		"0.3.1-alpha":       true,
		"1.0-snapshot":      true,
		"2026.9.1-pr5150.5": true,
		// The neighbours the token spelling has to survive. A version
		// whose letters merely contain a token is not a prerelease, and a
		// heuristic that thought so would hold ordinary ports at mint.
		"1.0-precision": false,
		"4.0-preview":   true,
	} {
		assert.Equal(t, want, Prerelease(version), version)
	}
}

func TestPrereleaseBaseNamesTheReleaseOrRefuses(t *testing.T) {
	for version, want := range map[string]string{
		"1.17.0-rc.3":       "1.17.0",
		"2.3.2-beta":        "2.3.2",
		"1.2.3.rc1":         "1.2.3",
		"2026.9.1-pr5150.5": "2026.9.1",
	} {
		base, ok := PrereleaseBase(version)
		assert.True(t, ok, version)
		assert.Equal(t, want, base, version)
	}
	_, ok := PrereleaseBase("1.17.0")
	assert.False(t, ok, "a release has no prerelease base")
	_, ok = PrereleaseBase("rc1")
	assert.False(t, ok, "all token, no base")
}
