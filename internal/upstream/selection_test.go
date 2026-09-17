package upstream_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

func TestRequestedVersionUsesConfirmedTagConvention(t *testing.T) {
	t.Parallel()
	for _, pattern := range []upstream.TagPattern{{Prefix: "v"}, {Prefix: "jq-"}, {Prefix: "release/", Suffix: "-stable"}, {}} {
		tag := pattern.Prefix + "1.8.1" + pattern.Suffix
		observed := []upstream.Candidate{{Release: forge.Release{Tag: tag, URL: "https://example.invalid/release"}}}
		for _, requested := range []string{"1.8.1", tag} {
			selected, err := upstream.MatchRelease(requested, &pattern, observed)
			require.NoError(t, err)
			require.Equal(t, requested, selected.Requested)
			require.Equal(t, "1.8.1", selected.Candidate.Version)
			require.Equal(t, tag, selected.Candidate.Tag)
			require.Equal(t, requested != tag, selected.Inferred)
			require.Empty(t, observed[0].Version, "evidence must not be mutated")
		}
	}
	pattern, err := upstream.PatternFromCurrent("1.8.0", "jq-1.8.0")
	require.NoError(t, err)
	require.Equal(t, upstream.TagPattern{Prefix: "jq-"}, pattern)
	for _, values := range [][2]string{{"1.0", "release-current"}, {"1", "v1-build1"}, {"", "v1"}} {
		_, err := upstream.PatternFromCurrent(values[0], values[1])
		require.ErrorIs(t, err, upstream.ErrTagPattern)
	}
}

func TestReleaseSelectionDoesNotGuessPastAmbiguousOrMissingEvidence(t *testing.T) {
	t.Parallel()
	pattern := &upstream.TagPattern{Prefix: "v"}
	_, err := upstream.MatchRelease("2", pattern, []upstream.Candidate{{Version: "2", Release: forge.Release{Tag: "2"}}, {Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrReleaseAmbiguous)
	require.ErrorContains(t, err, "specify the exact tag")
	_, err = upstream.MatchRelease("2", pattern, []upstream.Candidate{{Release: forge.Release{Tag: "2"}}, {Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrReleaseAmbiguous)
	_, err = upstream.MatchRelease("v2", pattern, []upstream.Candidate{{Version: "3", Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrTagPattern)

	_, err = upstream.MatchRelease("v2", pattern, []upstream.Candidate{{Release: forge.Release{Tag: "vv2"}}})
	require.ErrorIs(t, err, upstream.ErrReleaseMissing, "explicit prefix must not be doubled")
	_, err = upstream.MatchRelease("3", pattern, []upstream.Candidate{{Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrReleaseMissing)
	_, err = upstream.MatchRelease("v2", nil, []upstream.Candidate{{Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrTagPattern)
	selected, err := upstream.MatchRelease("2-rc1", pattern, []upstream.Candidate{{Release: forge.Release{Tag: "v2-rc1", Prerelease: true}}})
	require.NoError(t, err, "an explicit prerelease is not automatic latest selection")
	require.True(t, selected.Candidate.Prerelease)
	selected, err = upstream.MatchRelease("2", nil, []upstream.Candidate{{Version: "2", Release: forge.Release{URL: "https://example.invalid/2.tar.gz"}}})
	require.NoError(t, err)
	require.Empty(t, selected.Candidate.Tag, "archive releases need not invent tags")
	_, err = upstream.MatchRelease("2", pattern, []upstream.Candidate{{Version: "3", Release: forge.Release{Tag: "v2"}}})
	require.ErrorIs(t, err, upstream.ErrTagPattern)
	for _, input := range []string{"", "1 2", "1\n", "-option", string([]byte{0xff})} {
		_, err := upstream.MatchRelease(input, pattern, nil)
		require.ErrorIs(t, err, upstream.ErrVersionInput)
	}
}
