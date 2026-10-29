package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// indexedPorts stands in for a port index's dependency graph and versions.
type indexedPorts struct {
	graph    map[string][]string
	versions map[string]string
}

func (p indexedPorts) DependencyGraph(context.Context, model.Source) (map[string][]string, error) {
	return p.graph, nil
}

func (p indexedPorts) ArchiveVersions(context.Context, model.Source) (map[string]string, error) {
	return p.versions, nil
}

// prefixProvider is a local prefix with some ports active and global
// variants its variants.conf sets.
type prefixProvider struct {
	scriptedProvider
	active, variants []string
}

func (p *prefixProvider) Installed(context.Context, model.Environment) ([]string, []string, error) {
	return p.active, p.variants, nil
}

// A check's estimate of what it installs is the closure of its targets'
// dependencies by the port index, less what the environment has installed,
// none in Tart's, and the archives packages.macports.org lists for its
// release, whatever their variants; a local prefix whose variants.conf sets
// global variants has its archives left unsaid, and a command provider's
// environment gets none (the install estimate, 2026-10-29).
func TestAnInstallEstimate(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	asked := map[string]int{}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := strings.Trim(r.URL.Path, "/")
		mu.Lock()
		asked[port]++
		mu.Unlock()
		switch port {
		case "glib2":
			fmt.Fprint(w, `<a href="glib2-2.84.0_1+x11.darwin_25.arm64.tbz2">`)
		case "libffi":
			fmt.Fprint(w, `<a href="libffi-3.4.8_0.darwin_24.arm64.tbz2">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	e := &Engine{ArchiveSite: site.URL}
	e.Ports.Graphs = indexedPorts{
		graph:    map[string][]string{"gnome-voice": {"glib2", "cairo"}, "glib2": {"libffi"}},
		versions: map[string]string{"glib2": "2.84.0_1", "libffi": "3.4.8_0", "cairo": "1.18.4_0"},
	}
	e.Ports.Reader = fakePorts{}
	tahoe := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	local := model.Environment{Provider: buildenv.Local, Prefix: "/opt/local", Platform: tahoe.Platform}
	prefix := &prefixProvider{active: []string{"cairo"}}
	e.Providers = map[string]buildenv.Provider{buildenv.Tart: &scriptedProvider{}, buildenv.Local: prefix, buildenv.Command: &scriptedProvider{}}
	plan := model.Plan{Targets: []model.PlanTarget{{ID: "gnome-voice", Target: model.Target{Name: "gnome-voice"}}},
		Environments: []model.Environment{tahoe, local, {Provider: buildenv.Command}}}

	kept := e.InstallEstimates(t.Context(), plan, model.Source{}, false)
	require.Len(t, kept, 2, "no estimate for a command provider")
	require.Equal(t, "about 3 dependencies to install, by the port index; the clone decides", kept[0].Words(), "nothing kept yet: the archives are unsaid")
	require.Empty(t, asked, "only what's kept is read")

	estimates := e.InstallEstimates(t.Context(), plan, model.Source{}, true)
	require.Equal(t, "about 3 dependencies to install, 2 with no archive on packages.macports.org for this release, by the port index; the clone decides", estimates[0].Words(),
		"glib2's archive for 25 counts whatever its variants; libffi's is for 24, and cairo has none")
	require.Equal(t, "about 2 dependencies to install, 1 with no archive on packages.macports.org for this release, by the port index; the clone decides", estimates[1].Words(), "cairo is active in the prefix")
	require.Equal(t, 1, asked["glib2"], "each port asked once, kept after")

	prefix.variants = []string{"+universal"}
	estimates = e.InstallEstimates(t.Context(), plan, model.Source{}, true)
	require.Equal(t, "about 2 dependencies to install (archives not estimated: variants.conf sets +universal), by the port index; the clone decides", estimates[1].Words())
	require.Equal(t, "no dependencies to install, by the port index; the clone decides", InstallEstimate{}.Words())
}
