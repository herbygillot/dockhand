package macports_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

func portMaintainers(t *testing.T, port, value string) macports.PortMaintainers {
	t.Helper()
	maintainers, err := macports.ReadMaintainers(value)
	require.NoError(t, err)
	return macports.PortMaintainers{Port: port, Maintainers: maintainers}
}

// A pull request for a maintained port waits 72 hours from its opening
// before others merge it, openmaintainer or not, as the MacPorts Guide's
// section 7.4.1 says; by a test clock, the time left before, and none
// after, once the timeout has passed.
func TestAMaintainedPortWaitsOutTheTimeoutFromTheOpening(t *testing.T) {
	t.Parallel()
	opened := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	ports := []macports.PortMaintainers{portMaintainers(t, "jq", "{@ryandesign ryandesign} openmaintainer")}
	before := macports.MergeFitness(ports, "herbygillot", []string{"ryandesign"}, nil, opened, false, opened.Add(31*time.Hour))
	require.Equal(t, []macports.PortMerge{{Port: "jq", People: []string{"@ryandesign"}, OpenMaintainer: true}}, before.Ports)
	require.Equal(t, []string{"jq"}, before.Waiting(), "openmaintainer still waits on a pull request")
	require.Equal(t, opened.Add(72*time.Hour), before.Until)
	require.Equal(t, 41*time.Hour, before.Left)
	require.False(t, before.Passed())
	require.Equal(t, []string{"ryandesign"}, before.Mentioned)

	after := macports.MergeFitness(ports, "herbygillot", nil, nil, opened, false, opened.Add(73*time.Hour))
	require.Equal(t, opened.Add(72*time.Hour), after.Until)
	require.Zero(t, after.Left)
	require.True(t, after.Passed())

	unknown := macports.MergeFitness(ports, "herbygillot", nil, nil, time.Time{}, false, opened)
	require.Equal(t, []string{"jq"}, unknown.Waiting())
	require.True(t, unknown.Until.IsZero(), "no time without its opening")
	require.False(t, unknown.Passed())

	// A draft, which no one merges, is given no time; ready, it counts
	// from its opening again.
	draft := macports.MergeFitness(ports, "herbygillot", nil, nil, opened, true, opened.Add(31*time.Hour))
	require.True(t, draft.Draft)
	require.Equal(t, []string{"jq"}, draft.Waiting())
	require.True(t, draft.Until.IsZero(), "no time for a draft")
	require.False(t, draft.Passed())
}

// A nomaintainer port, one its maintainer opened, and one a maintainer
// approved don't wait; an approval by someone who isn't its maintainer,
// or by a maintainer named by an address alone, which no login matches,
// doesn't end the wait.
func TestWhatEndsTheWait(t *testing.T) {
	t.Parallel()
	opened := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	ports := []macports.PortMaintainers{
		portMaintainers(t, "gdal", "nomaintainer"),
		portMaintainers(t, "fd", "{@herbygillot gmail.com:herby.gillot}"),
		portMaintainers(t, "jq", "@ryandesign"),
		portMaintainers(t, "croc", "{example.org:ada} openmaintainer"),
	}
	merge := macports.MergeFitness(ports, "HerbyGillot", nil, []string{"ryandesign", "ada", "jmroot"}, opened, false, opened.Add(time.Hour))
	require.Equal(t, []macports.PortMerge{
		{Port: "gdal", NoMaintainer: true},
		{Port: "fd", People: []string{"@herbygillot"}, ByMaintainer: true},
		{Port: "jq", People: []string{"@ryandesign"}, ApprovedBy: []string{"@ryandesign"}},
		{Port: "croc", People: []string{"example.org:ada"}, OpenMaintainer: true},
	}, merge.Ports)
	require.Equal(t, []string{"croc"}, merge.Waiting())
	require.Equal(t, opened.Add(macports.MaintainerTimeout), merge.Until)

	none := macports.MergeFitness(ports[:3], "herbygillot", nil, []string{"ryandesign"}, opened, false, opened.Add(time.Hour))
	require.Empty(t, none.Waiting())
	require.True(t, none.Until.IsZero(), "nothing to wait for")
}
