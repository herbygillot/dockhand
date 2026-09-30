package fidelity

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func snapshot(ports map[string]macports.PortInfo) macports.Snapshot {
	return macports.Snapshot{Target: model.Target{Name: "main", Portfile: "devel/main/Portfile"}, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Ports: ports}
}

func TestEquivalentNormalizesRelocatedWorkspaces(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/before/devel/main/files"}}})
	before.Root = "/before"
	after := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	after.Root = "/after"
	require.NoError(t, Equivalent(before, after), "paths under different roots compare equal, each normalized by its own")
	elsewhere := after
	elsewhere.Root = "/elsewhere"
	require.ErrorIs(t, Equivalent(before, elsewhere), ErrMismatch, "a snapshot claiming another root shows as a changed option")
	require.Equal(t, "/before/devel/main/files", before.Ports["main"].Options["filespath"], "comparison must not mutate observations")
	changed := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 3, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	changed.Root = "/after"
	require.ErrorIs(t, Equivalent(before, changed), ErrMismatch)
	unrooted := before
	unrooted.Root = ""
	require.ErrorIs(t, Equivalent(unrooted, after), ErrMismatch, "a snapshot read back without its root keeps its paths, and they differ from a normalized one")
	require.NoError(t, Equivalent(unrooted, unrooted), "and compares equal to itself")
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
	report := Revision(before, bumped, "main")
	require.Equal(t, []string{"main.revision +1"}, report.ExpectedChanges)
	require.Empty(t, report.UnexpectedChanges)
	sibling := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 4, Options: map[string]string{}},
	})
	require.NotEmpty(t, Revision(before, sibling, "main").UnexpectedChanges, "a sibling revision change is unexpected")

	refreshed := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"checksums": "sha256 bbbb"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	require.Empty(t, Checksums(before, refreshed, "main", "sha256 bbbb").UnexpectedChanges)
	require.NotEmpty(t, Checksums(before, refreshed, "main", "sha256 cccc").UnexpectedChanges, "checksums must match the intended values")

	// A python stub and its subports share one declaration: refreshing the
	// selected subport's refreshes the stub's (the py-flatbuffers run's
	// finding 1). A sibling left behind, or one of another version, isn't
	// the selected port's to change.
	family := func(stub, other string) macports.Snapshot {
		return snapshot(map[string]macports.PortInfo{
			"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 bbbb"}},
			"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": stub}},
			"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": other}},
		})
	}
	shared := snapshot(map[string]macports.PortInfo{
		"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": "sha256 aaaa"}},
	})
	moved := Checksums(shared, family("sha256 bbbb", "sha256 aaaa"), "py313-demo", "sha256 bbbb")
	require.Empty(t, moved.UnexpectedChanges)
	require.Equal(t, []string{"py313-demo.checksums", "py-demo.checksums, shared"}, moved.ExpectedChanges)
	require.Empty(t, Checksums(shared, family("sha256 aaaa", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "a sibling keeping its own declaration is left as it was")
	require.NotEmpty(t, Checksums(shared, family("sha256 bbbb", "sha256 bbbb"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "another version's aren't shared")
	require.NotEmpty(t, Checksums(shared, family("sha256 cccc", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "nor are other checksums")
	apart := snapshot(map[string]macports.PortInfo{
		"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": "sha256 zzzz"}},
		"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": "sha256 aaaa"}},
	})
	require.NotEmpty(t, Checksums(apart, family("sha256 bbbb", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "a sibling with checksums of its own shares nothing")
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
	report := ScopedVersion(false, before, after, "main", model.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.Empty(t, report.UnexpectedChanges)
	after.Ports["main"].Options["description"] = "changed"
	report = ScopedVersion(false, before, after, "main", model.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.NotEmpty(t, report.UnexpectedChanges, "other metadata still may not move")
}
