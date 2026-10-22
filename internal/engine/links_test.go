package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// A check's undeclared links are its passed builds' links from ports
// their Portfiles don't declare in depends_lib, as the check's revision
// evaluates them, from a provider that reads links; a build whose links
// it couldn't read says so, and a provider that never reads them says
// nothing (v0.5.0 part 6).
func TestUndeclaredLinksSetABuildsLinksBesideItsDeclaredOnes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{active: []model.ActivePort{}, readsLinks: true, links: map[model.TargetID][]model.Link{
		"harbor-cli": {
			{Binary: "/opt/local/bin/harbor", Library: "/opt/local/lib/libharbor.1.dylib", Port: "libharbor"},
			{Binary: "/opt/local/bin/harbor", Library: "/opt/local/lib/libonig.5.dylib", Port: "oniguruma"},
		},
		"libharbor": {},
	}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	run, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm).ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	found, err := e.UndeclaredLinks(t.Context(), run)
	require.NoError(t, err)
	var said []string
	for _, finding := range found {
		if finding.Unread {
			said = append(said, string(finding.Target)+" unread")
		} else {
			said = append(said, finding.Subject())
		}
	}
	require.Contains(t, said, "harbor-cli links oniguruma", "libharbor is declared")
	require.NotContains(t, said, "libharbor unread", "nothing links is read")
	require.Contains(t, said, "harbor-viewer unread")

	provider.readsLinks = false
	found, err = e.UndeclaredLinks(t.Context(), run)
	require.NoError(t, err)
	require.Empty(t, found, "a provider that never reads links says nothing of them")
}
