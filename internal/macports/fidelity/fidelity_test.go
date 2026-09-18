package fidelity

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func snapshot(ports map[string]macports.PortInfo) macports.Snapshot {
	return macports.Snapshot{Target: record.Target{Name: "main", Portfile: "devel/main/Portfile"}, Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Ports: ports}
}

func TestEquivalentNormalizesRelocatedWorkspaces(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/before/devel/main/files"}}})
	after := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	require.NoError(t, Equivalent(before, after, "/before", "/after"), "paths under different roots compare equal")
	require.ErrorIs(t, Equivalent(before, after, "/before", "/elsewhere"), ErrMismatch, "an unexpected root shows as a changed option")
	require.Equal(t, "/before/devel/main/files", before.Ports["main"].Options["filespath"], "comparison must not mutate observations")
	changed := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 3, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	require.ErrorIs(t, Equivalent(before, changed, "/before", "/after"), ErrMismatch)
}

func TestRevisionAndChecksumsReportOnlyIntendedChanges(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	bumped := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	report := Revision(before, bumped, "main", "/source")
	require.Equal(t, []string{"main.revision +1"}, report.ExpectedChanges)
	require.Empty(t, report.UnexpectedChanges)
	sibling := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 4, Options: map[string]string{}},
	})
	require.NotEmpty(t, Revision(before, sibling, "main", "/source").UnexpectedChanges, "a sibling revision change is unexpected")

	refreshed := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"checksums": "sha256 bbbb"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	require.Empty(t, Checksums(before, refreshed, "main", "/source", "sha256 bbbb").UnexpectedChanges)
	require.NotEmpty(t, Checksums(before, refreshed, "main", "/source", "sha256 cccc").UnexpectedChanges, "checksums must match the intended values")
	require.Error(t, CheckSnapshot(macports.Snapshot{}, macports.Context{}))
}

// A homepage that spells the version moves with a bump: the perl5 ports'
// metacpan release pages do, and nothing is fetched from it.
func TestVersionAllowsAHomepageThatFollowsTheVersion(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main": {Name: "main", Version: "0.21.0", Revision: 0, Options: map[string]string{"checksums": "sha256 aaaa", "homepage": "https://metacpan.org/release/List-Uniq-v0.21.0"}},
	})
	after := snapshot(map[string]macports.PortInfo{
		"main": {Name: "main", Version: "0.230.0", Revision: 0, Options: map[string]string{"checksums": "sha256 bbbb", "homepage": "https://metacpan.org/release/List-Uniq-0.23"}},
	})
	report := ScopedVersion(false, before, after, "main", "/source", record.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.Empty(t, report.UnexpectedChanges)
	after.Ports["main"].Options["description"] = "changed"
	report = ScopedVersion(false, before, after, "main", "/source", record.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.NotEmpty(t, report.UnexpectedChanges, "other metadata still may not move")
}
