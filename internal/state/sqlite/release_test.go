package sqlite

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// An archive release names the evaluated port version and, when the
// Portfile derives it from the source's spelling, that spelling beside it:
// an explicit request matches the spelling, an automatic one carries its
// listing, and a spelling that is the version itself or is not a version
// is refused.
func TestArchiveReleaseValidationFollowsTheSourceSpelling(t *testing.T) {
	t.Parallel()
	listing := &record.ReleaseListing{URL: "https://fastapi.metacpan.org/v1/release/JSON/", SHA256: strings.Repeat("ab", 32)}
	for name, scenario := range map[string]struct {
		release record.Release
		valid   bool
	}{
		"explicit evaluated":        {record.Release{Archive: true, Selection: record.Selection{Requested: "1.2.4", CurrentVersion: "1.2.3"}, Version: "1.2.4"}, true},
		"explicit derived":          {record.Release{Archive: true, Selection: record.Selection{Requested: "0.42", CurrentVersion: "0.410.0"}, Version: "0.420.0", SourceVersion: "0.42"}, true},
		"explicit mismatched":       {record.Release{Archive: true, Selection: record.Selection{Requested: "0.42", CurrentVersion: "0.410.0"}, Version: "0.420.0"}, false},
		"automatic derived":         {record.Release{Archive: true, Selection: record.Selection{CurrentVersion: "0.410.0"}, Version: "0.420.0", SourceVersion: "0.42", Listing: listing}, true},
		"spelling is the version":   {record.Release{Archive: true, Selection: record.Selection{Requested: "1.2.4", CurrentVersion: "1.2.3"}, Version: "1.2.4", SourceVersion: "1.2.4"}, false},
		"spelling is not a version": {record.Release{Archive: true, Selection: record.Selection{Requested: "1 2", CurrentVersion: "1.2.3"}, Version: "1.2.4", SourceVersion: "1 2"}, false},
	} {
		require.Equal(t, scenario.valid, validReleaseSource(scenario.release), name)
	}
}
