package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
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

// A source is how and from where a port fetches too: a change of git.url
// alone, or from an archive to Git at the same version, read as the same
// source, so nothing upstream was compared, and a protected sibling's move
// went unseen; an option the evaluation couldn't settle is never the same
// (Codex's review of 832f059e, high 2).
func TestASourceIsHowAndWhereItFetches(t *testing.T) {
	port := func(options map[string]string) PortInfo {
		base := map[string]string{"fetch.type": "git", "git.url": "https://github.com/a/tool.git", "git.branch": "v1.0", "patchfiles": ""}
		for key, value := range options {
			base[key] = value
		}
		return PortInfo{Name: "tool", Version: "1.0", Options: base}
	}
	before := port(nil)
	require.True(t, SameSource(before, port(nil)))
	require.False(t, SameSource(before, port(map[string]string{"git.url": "https://github.com/b/tool.git"})), "another repository at the same tag")
	require.False(t, SameSource(before, port(map[string]string{"fetch.type": "standard"})), "an archive where it cloned")
	unsettled := port(nil)
	unsettled.OptionErrors = map[string]string{"git.url": "can't read \"github.author\""}
	require.False(t, SameSource(unsettled, unsettled), "unknown is never the same")

	pinned := model.Target{Name: "tool", Portfile: "devel/tool/Portfile"}
	scope := &ReleaseScope{Protected: []ReleaseMember{{Target: pinned, After: ReleaseStateOf(before)}}}
	_, err := RebindReleaseScope(scope, Snapshot{Ports: map[string]PortInfo{"tool": port(nil)}})
	require.NoError(t, err)
	_, err = RebindReleaseScope(scope, Snapshot{Ports: map[string]PortInfo{"tool": port(map[string]string{"git.url": "https://github.com/b/tool.git"})}})
	require.ErrorContains(t, err, "protected sibling tool changed source identity")
}
