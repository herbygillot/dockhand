package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Eligibility reads each option as MacPorts does, names an exclusion by
// the option behind it, and keeps what couldn't be read apart from both
// (the private-helper review's finding 1, and the beekeeper-studio run's
// finding 3).
func TestEligibilityReadsOptionsAsMacPortsDoes(t *testing.T) {
	t.Parallel()
	arm := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	port := func(options map[string]string, failures map[string]string) PortInfo {
		return PortInfo{Name: "demo", Options: options, OptionErrors: failures}
	}
	for _, test := range []struct {
		name   string
		port   PortInfo
		want   Eligibility
		reason string
	}{
		{"eligible", port(map[string]string{"dockhand.known_fail": "0", "supported_archs": "arm64 x86_64"}, nil), Eligibility{}, ""},
		{"replaced", port(map[string]string{"replaced_by": "demo2"}, nil), Eligibility{Excluded: ExcludedReplaced, Detail: "demo2"}, "replaced by demo2"},
		{"known to fail, as MacPorts tests it", port(map[string]string{"dockhand.known_fail": "1", "dockhand.platforms_compatible": "1"}, nil), Eligibility{Excluded: ExcludedKnownFail}, "the Portfile marks it known_fail here"},
		{"known_fail on, read as Tcl reads it", port(map[string]string{"known_fail": "on"}, nil), Eligibility{Excluded: ExcludedKnownFail}, "the Portfile marks it known_fail here"},
		{"excluded by platforms, which MacPorts marks known to fail", port(map[string]string{"dockhand.known_fail": "1", "dockhand.platforms_compatible": "0", "platforms": "{darwin >= 23}"}, nil),
			Eligibility{Excluded: ExcludedPlatforms, Detail: "{darwin >= 23}"}, "its platforms, {darwin >= 23}, exclude this release"},
		{"a Tcl list of architectures", port(map[string]string{"dockhand.known_fail": "0", "supported_archs": "{arm64}"}, nil), Eligibility{}, ""},
		{"another architecture only", port(map[string]string{"dockhand.known_fail": "0", "supported_archs": "x86_64"}, nil), Eligibility{Excluded: ExcludedArchs, Detail: "x86_64"}, "supported_archs x86_64 only"},
		{"noarch", port(map[string]string{"dockhand.known_fail": "0", "supported_archs": "noarch"}, nil), Eligibility{}, ""},
	} {
		got, err := BuildEligibility(test.port, arm)
		require.NoError(t, err, test.name)
		require.Equal(t, test.want, got, test.name)
		require.Equal(t, test.reason, got.Reason(), test.name)
		require.Equal(t, test.reason == "", got.Eligible(), test.name)
	}
	for name, failures := range map[string]map[string]string{
		"known_fail unread":      {"dockhand.known_fail": "boom"},
		"supported_archs unread": {"supported_archs": "boom"},
		"replaced_by unread":     {"replaced_by": "boom"},
	} {
		_, err := BuildEligibility(port(map[string]string{}, failures), arm)
		require.Error(t, err, "%s is neither eligible nor excluded", name)
	}
	_, err := BuildEligibility(port(map[string]string{"dockhand.known_fail": "0", "supported_archs": "{arm64"}, nil), arm)
	require.ErrorContains(t, err, "isn't a Tcl list")
}
