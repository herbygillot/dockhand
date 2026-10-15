package portedit

import (
	"errors"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// An archive differs where any of its group's values isn't what was
// fetched, which a checksum refresh's plan names.
func TestAnArchiveDiffersByAnyValue(t *testing.T) {
	group := distfiles.Group{Values: map[string]distfiles.Token{"sha256": {Value: "aa"}, "size": {Value: "3"}}}
	matching := distfetch.Download{Checksum: portfile.Checksum{SHA256: "aa", Size: 3}}
	require.False(t, differs(group, matching))
	require.True(t, differs(group, distfetch.Download{Checksum: portfile.Checksum{SHA256: "aa", Size: 4}}))
}

// What a refresh leaves is said by what and why, without the sentinels
// its reason wraps.
func TestALeftContextSaysWhy(t *testing.T) {
	left := ChecksumLeft{Kind: LeftContext, What: contextWords(model.Platform{OS: "darwin", Version: "21", Architecture: "x86_64"}, ""),
		Reason: reasonWords(errors.Join(errProbeInconclusive, errors.New("x")))}
	require.Equal(t, "darwin 21 x86_64", left.What)
	require.Equal(t, "+doc", contextWords(model.Platform{}, "doc"))
	require.Equal(t, "darwin 21 x86_64 (x)", leftWords([]ChecksumLeft{{What: left.What, Reason: "x"}}))
}
