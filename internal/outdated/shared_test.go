package outdated

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	portselection "github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Subports of one Portfile that share its version and source are looked
// up once, each with its own copy of the release found; one that differs
// in either is looked up apart (field testing at 6dd7745c: php and p5
// subports made most of ryandesign's 2,133).
func TestSubportsSharingASourceAreLookedUpOnce(t *testing.T) {
	port := func(version, project string) macports.PortInfo {
		return macports.PortInfo{Version: version, Options: map[string]string{"github.author": "php", "github.project": project}}
	}
	same := lookupKey("lang/php/Portfile", port("8.3.1", "php-src"))
	require.Equal(t, same, lookupKey("lang/php/Portfile", port("8.3.1", "php-src")))
	require.NotEqual(t, same, lookupKey("lang/php/Portfile", port("8.2.9", "php-src")), "another version")
	require.NotEqual(t, same, lookupKey("lang/php/Portfile", port("8.3.1", "pecl")), "another source")
	require.Empty(t, lookupKey("", port("8.3.1", "php-src")))
	// A subport whose name keeps it in a series is looked up apart from
	// one that isn't, at the same version and source (#120).
	named := func(name string) macports.PortInfo {
		info := port("1.0", "fixture")
		info.Name = name
		return info
	}
	require.NotEqual(t, lookupKey("devel/fixture/Portfile", named("fixture")), lookupKey("devel/fixture/Portfile", named("fixture-1")))

	shared := &sharedLookups{lookups: map[string]*sharedLookup{}}
	var asked atomic.Int32
	release := func(context.Context) (upstream.Result, error) {
		asked.Add(1)
		return upstream.Result{Assessment: upstream.UpdateAvailable, Release: &model.Release{Version: "8.3.2"}}, nil
	}
	var wg sync.WaitGroup
	results := make([]upstream.Result, 5)
	for i := range results {
		wg.Go(func() {
			var err error
			results[i], err = shared.do(t.Context(), same, release)
			require.NoError(t, err)
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, asked.Load())
	for _, result := range results {
		require.Equal(t, "8.3.2", result.Release.Version)
	}
	results[1].Release.Version = "changed"
	require.Equal(t, "8.3.2", results[2].Release.Version, "each its own copy")

	// One cut short by its own context isn't shared.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := shared.do(cancelled, "other", func(ctx context.Context) (upstream.Result, error) { return upstream.Result{}, ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	result, err := shared.do(t.Context(), "other", release)
	require.NoError(t, err)
	require.Equal(t, "8.3.2", result.Release.Version, "asked again")
}

// fixtureCatalog is a repository whose tags are v1.0, v1.1, and v2.0.
type fixtureCatalog struct{}

func (fixtureCatalog) Repository(_, _ string) (forge.Repository, error) {
	return fixtureCatalog{}, nil
}
func (fixtureCatalog) Name() string { return "owner/fixture" }
func (fixtureCatalog) Tag(_ context.Context, name string) (forge.Tag, error) {
	return forge.Tag{Name: name, Commit: strings.Repeat("a", 40)}, nil
}
func (c fixtureCatalog) ListTags(ctx context.Context) ([]forge.Tag, error) {
	var tags []forge.Tag
	for _, name := range []string{"v1.0", "v1.1", "v2.0"} {
		tag, _ := c.Tag(ctx, name)
		tags = append(tags, tag)
	}
	return tags, nil
}

// fixture-1 keeps to its series beside fixture, which doesn't: sharing
// fixture's lookup, it took 2.0 (#120; Codex's review of b2e7b616, its
// probe).
func TestASubportsSeriesSurvivesASharedLookup(t *testing.T) {
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	testsupport.WriteFiles(t, root, map[string]string{"devel/fixture/Portfile": `PortSystem 1.0
name fixture
version 1.0
categories devel
options github.author github.project github.version github.tag_prefix github.tag_suffix github.tarball_from
github.author owner
github.project fixture
github.version ${version}
github.tag_prefix v
github.tag_suffix {}
github.tarball_from archive
git.branch v${version}
master_sites https://example.invalid/fixture/${version}
distfiles fixture-${version}.tar.gz
livecheck.type regex
livecheck.url https://github.com/owner/fixture/tags
livecheck.regex {archive/refs/tags/v([^/]+)\.tar\.gz}
livecheck.version ${version}
subport fixture {}
subport fixture-1 {}
`})
	testsupport.Git(t, root, "init", "-q", "-b", "master")
	testsupport.Git(t, root, "add", ".")
	testsupport.Git(t, root, "commit", "-qm", "fixture: two release policies in one Portfile")
	repo, err := git.Open(t.Context(), root, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	ports := &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}
	index := &portindex.Stager{Repo: repo, Config: portindex.Config{Env: os.Environ(), Executable: testsupport.MacPortsTool(t, "portindex"), CacheDirectory: t.TempDir()}, NativePlatform: ports.NativePlatform}
	service := Service{Repo: repo, Ports: &portselection.Reader{Evaluator: ports, Index: index}, Index: index, Upstream: &upstream.Service{Versions: ports, Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: fixtureCatalog{}}}, Concurrency: 1}
	alone, err := service.Observe(t.Context(), Selection{Ports: []string{"fixture-1"}})
	require.NoError(t, err)
	require.Len(t, alone.Ports, 1)
	require.Equal(t, "1.1", alone.Ports[0].CandidateVersion)
	both, err := service.Observe(t.Context(), Selection{Ports: []string{"fixture", "fixture-1"}})
	require.NoError(t, err)
	require.Len(t, both.Ports, 2)
	for _, result := range both.Ports {
		if result.Selector == "fixture-1" {
			require.Equal(t, "1.1", result.CandidateVersion)
		}
	}
}
