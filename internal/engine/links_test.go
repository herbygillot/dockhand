package engine

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
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

// archLinksPorts are harborPorts, but for x86_64's harbor-cli, which
// declares oniguruma in its depends_lib, as a Portfile can by architecture.
type archLinksPorts struct{ fakePorts }

func (p archLinksPorts) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	infos, err := p.fakePorts.Ports(ctx, source, directory, environment, variants)
	infos = slices.Clone(infos)
	for i := range infos {
		if infos[i].Name == "harbor-cli" && environment.Platform.Architecture == "x86_64" {
			infos[i].Dependencies = append(slices.Clone(infos[i].Dependencies), macports.Dependency{Phase: "lib", Port: "oniguruma"})
		}
	}
	return infos, err
}

// A declaration in one environment hides no other's link: the arm64 build
// links oniguruma, which only x86_64's evaluation declares (Codex's review
// of 8c007979, C8-2, its probe).
func TestALinkIsJudgedInTheEnvironmentThatBuiltIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{active: []model.ActivePort{}, readsLinks: true, links: map[model.TargetID][]model.Link{
		"harbor-cli": {{Binary: "/opt/local/bin/harbor", Library: "/opt/local/lib/libonig.5.dylib", Port: "oniguruma"}}, "libharbor": {}}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	run, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm, tahoeX86).ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	usePorts(e, archLinksPorts{harborPorts()})
	found, err := e.UndeclaredLinks(t.Context(), run)
	require.NoError(t, err)
	i := slices.IndexFunc(found, func(f LinkFinding) bool { return f.Target == "harbor-cli" && f.Port == "oniguruma" })
	require.GreaterOrEqual(t, i, 0, "x86_64's declaration hides nothing on arm64: %#v", found)
	require.Equal(t, []model.Environment{tahoeArm}, found[i].Environments)
}
