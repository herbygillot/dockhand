package macports

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestRebindReleaseScopePreservesMembershipAndPins(t *testing.T) {
	root := record.Target{Name: "root", Portfile: "devel/root/Portfile"}
	sibling := root
	sibling.Name = "sibling"
	sibling.Subport = sibling.Name
	pin := root
	pin.Name = "pinned"
	pin.Subport = pin.Name
	info := func(name, version string) PortInfo {
		return PortInfo{Name: name, Version: version, Options: map[string]string{"checksums": "sha256 aaaa", "distfiles": "source.tar.gz", "master_sites": "https://example.invalid/source"}}
	}
	snapshot := Snapshot{Ports: map[string]PortInfo{"root": info("root", "2"), "sibling": info("sibling", "2"), "pinned": info("pinned", "1")}}
	scope := &record.ReleaseScope{Input: record.ReleaseInput{Portfile: root.Portfile}, Affected: []record.ReleaseMember{{Target: root, After: ReleaseState(snapshot.Ports[root.Name])}, {Target: sibling, After: ReleaseState(snapshot.Ports[sibling.Name])}}, Protected: []record.ReleaseMember{{Target: pin, After: ReleaseState(snapshot.Ports[pin.Name])}}}
	updated, err := RebindReleaseScope(scope, snapshot)
	require.NoError(t, err)
	require.True(t, scope.SameMembership(updated))
	snapshot.Ports["sibling"].Options["checksums"] = "sha256 bbbb"
	updated, err = RebindReleaseScope(scope, snapshot)
	require.NoError(t, err)
	require.Equal(t, "sha256 aaaa", scope.Affected[1].After.Checksums)
	require.Equal(t, "sha256 bbbb", updated.Affected[1].After.Checksums)
	snapshot.Ports["pinned"].Options["master_sites"] = "https://example.invalid/different"
	_, err = RebindReleaseScope(scope, snapshot)
	require.ErrorContains(t, err, "protected sibling")
	delete(snapshot.Ports, "sibling")
	_, err = RebindReleaseScope(scope, snapshot)
	require.ErrorContains(t, err, "target set")
	updated.Affected = updated.Affected[:1]
	require.False(t, scope.SameMembership(updated))
}
