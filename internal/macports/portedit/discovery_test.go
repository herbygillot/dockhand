package portedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

type probeCatalog struct{ tag string }

func (c probeCatalog) Repository(string, string) (forge.Repository, error) { return c, nil }
func (c probeCatalog) Name() string                                        { return "owner/fixture" }
func (c probeCatalog) Tag(_ context.Context, name string) (forge.Tag, error) {
	if name != c.tag {
		return forge.Tag{}, forge.ErrNotFound
	}
	return forge.Tag{Name: name, Commit: strings.Repeat("b", 40)}, nil
}
func (c probeCatalog) ListTags(context.Context) ([]forge.Tag, error) {
	return []forge.Tag{{Name: c.tag, Commit: strings.Repeat("b", 40)}}, nil
}

func TestSourceBoundDiscoveryDoesNotRequireABumpOrPermitUnsafeEdits(t *testing.T) {
	for _, test := range []struct {
		body, tag, version string
		unsafe             bool
	}{
		{"set release 12\ngithub.setup owner fixture $release v\nversion [expr {${github.version} * 10 + 7}]", "v13", "137", false},
		{"github.setup owner fixture 1.2.3 v\nsubport fixture-child {}", "v1.2.4", "1.2.4", true},
	} {
		service, request, input := probeFixture(t, test.body+`
options github.tarball_from
github.tarball_from archive
livecheck.type regex
livecheck.url https://github.com/owner/fixture/tags
livecheck.regex {archive/refs/tags/v([^/]+)\.tar\.gz}
livecheck.version ${github.version}
`)
		probe, err := service.Probe(t.Context(), ProbeSource{Source: request.Source, Root: request.Root, Selection: request.Selection, Platform: request.Platform})
		require.NoError(t, err)
		info := probe.Port()
		info.Options["github.project"] = "changed-by-caller"
		require.Equal(t, "fixture", probe.Port().Options["github.project"])
		catalogs := &upstream.Service{Versions: service.Ports.(*eval.Evaluator), Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: probeCatalog{test.tag}}}
		discovery, err := catalogs.Bind(probe)
		require.NoError(t, err)
		assessment, err := discovery.Discover(t.Context())
		require.NoError(t, err)
		require.Equal(t, upstream.UpdateAvailable, assessment.Assessment)
		require.Equal(t, test.version, assessment.CandidateVersion)
		require.Nil(t, catalogs.EvaluateVersion, "binding must not mutate the shared discovery service")
		release, err := discovery.Resolve(t.Context(), test.tag)
		require.NoError(t, err)
		require.Equal(t, test.version, release.Version)
		err = probe.CheckRelease(t.Context(), release)
		if test.unsafe {
			require.ErrorIs(t, err, ErrFidelity)
		} else {
			require.NoError(t, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = probe.EvaluateVersion(ctx, strings.TrimPrefix(test.tag, "v"))
		require.ErrorIs(t, err, context.Canceled)
		original, err := os.ReadFile(filepath.Join(request.Root, "devel/fixture/Portfile"))
		require.NoError(t, err)
		require.Equal(t, input.data, original)
	}
}
