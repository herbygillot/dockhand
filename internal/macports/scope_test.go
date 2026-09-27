package macports

import (
	"github.com/herbygillot/dockhand/internal/model"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRebindReleaseScopePreservesMembershipAndPins(t *testing.T) {
	root := model.Target{Name: "root", Portfile: "devel/root/Portfile"}
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
	scope := &ReleaseScope{Input: ReleaseInput{Portfile: root.Portfile}, Affected: []ReleaseMember{{Target: root, After: ReleaseStateOf(snapshot.Ports[root.Name])}, {Target: sibling, After: ReleaseStateOf(snapshot.Ports[sibling.Name])}}, Protected: []ReleaseMember{{Target: pin, After: ReleaseStateOf(snapshot.Ports[pin.Name])}}}
	updated, err := RebindReleaseScope(scope, snapshot)
	require.NoError(t, err)
	require.Equal(t, members(scope), members(updated), "rebinding keeps the members")
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
	require.NotEqual(t, members(scope), members(updated))
}

// members names a scope's affected and protected ports, in order.
func members(scope *ReleaseScope) [2][]string {
	var names [2][]string
	for i, group := range [][]ReleaseMember{scope.Affected, scope.Protected} {
		for _, member := range group {
			names[i] = append(names[i], member.Target.Name)
		}
	}
	return names
}
