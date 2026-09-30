package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The evaluator's facts are read as their types, and an unread or failed
// one is told apart from a false one (the code-organization review's
// finding 27).
func TestTheEvaluatorsFactsAreReadAsTheirTypes(t *testing.T) {
	t.Parallel()
	port := PortInfo{Options: map[string]string{"dockhand.metadata_only": "1", "dockhand.livecheck_standard": "0", "dockhand.test_run": "0", "dockhand.portgroups": "github {cmake}",
		"fetch.has_credentials": "0", "fetch.archive_compatible": "1", "dockhand.base_version": "2.12.6"}}
	only, err := port.MetadataOnly()
	require.NoError(t, err)
	require.True(t, only)
	standard, err := port.LivecheckStandard()
	require.NoError(t, err)
	require.False(t, standard)
	declares, known := port.DeclaresTests()
	require.Equal(t, [2]bool{false, true}, [2]bool{declares, known})
	groups, known := port.PortGroups()
	require.True(t, known)
	require.Equal(t, []string{"github", "cmake"}, groups)
	credentials, err := port.FetchCredentials()
	require.NoError(t, err)
	require.False(t, credentials)
	compatible, problem, err := port.ArchiveCompatible()
	require.NoError(t, err)
	require.True(t, compatible)
	require.Empty(t, problem)
	base, ok := port.BaseVersion()
	require.Equal(t, "2.12.6", base)
	require.True(t, ok)

	unread := PortInfo{Options: map[string]string{}, OptionErrors: map[string]string{"dockhand.metadata_only": "cannot tell whether the port builds anything: boom"}}
	_, err = unread.MetadataOnly()
	require.ErrorContains(t, err, "cannot tell whether the port builds anything")
	_, known = unread.DeclaresTests()
	require.False(t, known)
	_, known = unread.PortGroups()
	require.False(t, known)
	_, err = unread.FetchCredentials()
	require.Error(t, err, "not evaluated isn't no credentials")
	_, _, err = unread.ArchiveCompatible()
	require.Error(t, err, "not assessed isn't compatible")
	_, ok = unread.BaseVersion()
	require.False(t, ok)

	custom := PortInfo{Options: map[string]string{"fetch.archive_compatible": "0", "dockhand.base_version": "2.12.6"}, Fetch: &FetchSemantics{Kind: "custom", Problem: "pre-fetch hook 1 writes distfiles"}}
	compatible, problem, err = custom.ArchiveCompatible()
	require.NoError(t, err)
	require.False(t, compatible)
	require.Equal(t, "MacPorts Base 2.12.6: pre-fetch hook 1 writes distfiles", problem)

	require.True(t, IsLivecheckOption("livecheck.regex"))
	require.True(t, IsLivecheckOption("dockhand.livecheck_standard"))
	require.False(t, IsLivecheckOption("dockhand.metadata_only"))
}

// A stub whose probe failed is said, not taken for an ordinary port that
// builds, which fidelity then refused with a misleading reason.
func TestAStubWhoseProbeFailedIsSaid(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{Ports: map[string]PortInfo{
		"py-requests":    {Name: "py-requests", Version: "2.32.0", OptionErrors: map[string]string{"dockhand.metadata_only": "cannot tell whether the port builds anything: boom"}},
		"py314-requests": {Name: "py314-requests", Version: "2.32.0", Options: map[string]string{"dockhand.metadata_only": "0"}},
	}}
	_, _, err := ResolveStub(snapshot, model.Target{Name: "py-requests"})
	require.ErrorContains(t, err, "can't tell whether py-requests builds anything")
}

// A port's plain-HTTP URLs are its homepage and its master_sites that are
// URLs, tags taken off as Base takes them, and never a mirror group, which
// is MacPorts' own; MacPorts prefers HTTPS.
func TestAPortsPlainHTTPURLs(t *testing.T) {
	t.Parallel()
	info := PortInfo{Options: map[string]string{
		"homepage":     "http://txt.hellman.io/",
		"master_sites": "http://ftp.example.org/pub/ http://ftp.example.org/pub/:docs http://mirror.example.org/x/:nosubdir:src https://example.org/ gnu sourceforge:project",
	}}
	require.Equal(t, []PlainURL{{"homepage", "http://txt.hellman.io/"}, {"master_sites", "http://ftp.example.org/pub/"}, {"master_sites", "http://mirror.example.org/x/"}}, info.PlainHTTP())
	require.Empty(t, PortInfo{Options: map[string]string{"homepage": "https://example.org", "master_sites": "https://example.org/ gnu"}}.PlainHTTP())
}
