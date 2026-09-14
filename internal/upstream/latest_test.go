package upstream_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

type catalog struct {
	releases     []forge.Release
	tags         []forge.Tag
	err          error
	tagReads     int
	releaseReads int
	releaseErr   error
	tag          tagFunc
	name         string
	instance     string
}

func (c *catalog) Releases(context.Context) ([]forge.Release, error) {
	c.releaseReads++
	if c.releaseErr != nil {
		return nil, c.releaseErr
	}
	return c.releases, c.err
}

func (c *catalog) ListTags(context.Context) ([]forge.Tag, error) {
	c.tagReads++
	return c.tags, c.err
}

func automaticPort() macports.PortInfo {
	port := githubPort()
	port.Options["github.tarball_from"] = "releases"
	port.Options["livecheck.type"] = "regex"
	port.Options["livecheck.url"] = "https://github.com/owner/project/tags"
	port.Options["livecheck.regex"] = `{archive/refs/tags/v([^/]+)\.tar\.gz}`
	port.Options["livecheck.version"] = port.Version
	return port
}

func automaticService(t *testing.T, c *catalog) *upstream.Service {
	t.Helper()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts is required")
	}
	c.tag = tagFunc(func(_ context.Context, _ string, name string) (forge.Tag, error) {
		return forge.Tag{Name: name, Commit: strings.Repeat("a", 40)}, nil
	})
	return &upstream.Service{Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: c, portsource.GitLab: c}, Versions: &macports.Evaluator{Executable: executable}}

}

func TestAutomaticSelectionHonorsArchiveModeVersionOrderingAndPrereleases(t *testing.T) {
	c := &catalog{releases: []forge.Release{{Tag: "v1.9"}, {Tag: "v1.10"}, {Tag: "v2.0", Prerelease: true}, {Tag: "v3.0", Draft: true}, {Tag: "v4.0-rc1"}, {Tag: "other-99.0"}}, tags: []forge.Tag{{Name: "v1.9"}, {Name: "v1.10"}, {Name: "v1.11"}, {Name: "v2.0"}, {Name: "v4.0-rc1"}}}
	service := automaticService(t, c)
	port := automaticPort()
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "1.10", result.CandidateVersion)
	require.Equal(t, "1.0", result.Release.CurrentVersion)
	require.Empty(t, result.Release.Requested)
	require.Zero(t, c.tagReads)
	port.Options["github.tarball_from"] = "archive"
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, "2.0", result.CandidateVersion, "release flags must not veto Git tags")
	require.Equal(t, 1, c.releaseReads)
	require.Equal(t, 1, c.tagReads)
	c.releases = nil
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, "2.0", result.CandidateVersion, "tag-only projects need no GitHub Release")
}

func TestAutomaticSelectionAppliesMaintainerFilterAndReportsCurrentOrAhead(t *testing.T) {
	c := &catalog{releases: []forge.Release{{Tag: "v1.0"}, {Tag: "v2.0"}}}
	service := automaticService(t, c)
	port := automaticPort()
	port.Options["livecheck.regex"] = `{archive/refs/tags/v(1\.[0-9]+)\.tar\.gz}`
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Current, result.Assessment)
	require.True(t, result.Release.NoUpdate)
	port.Version = "1.1"
	port.Options["github.version"] = "1.1"
	port.Options["git.branch"] = "v1.1"
	port.Options["livecheck.version"] = "1.1"
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.True(t, result.Release.NoUpdate)
	require.Equal(t, "1.0", result.Release.Version)
	require.Equal(t, "1.1", result.Release.CurrentVersion)
}

func TestAutomaticUnknownIsNeverReportedCurrent(t *testing.T) {
	for _, mode := range []string{"network", "no matches", "prerelease only", "invalid regex", "ambiguous", "missing selected tag", "custom source", "prerelease current"} {
		t.Run(mode, func(t *testing.T) {
			c := &catalog{releases: []forge.Release{{Tag: "v2.0"}}}
			service := automaticService(t, c)
			port := automaticPort()
			switch mode {
			case "network":
				c.err = errors.New("rate limited")
			case "no matches":
				c.releases = nil
			case "prerelease only":
				c.releases[0].Prerelease = true
			case "invalid regex":
				port.Options["livecheck.regex"] = "("
			case "ambiguous":
				c.releases = append(c.releases, forge.Release{Tag: "v2.00"})
			case "missing selected tag":
				c.tag = tagFunc(func(context.Context, string, string) (forge.Tag, error) {
					return forge.Tag{}, forge.ErrNotFound
				})
			case "custom source":
				port.Options["livecheck.url"] = "https://example.invalid/latest"
			case "prerelease current":
				port.Version = "1.0rc1"
				port.Options["github.version"] = port.Version
				port.Options["git.branch"] = "v" + port.Version
				port.Options["livecheck.version"] = port.Version
			}
			result, err := service.DiscoverPort(t.Context(), port)
			require.Error(t, err)
			require.Equal(t, upstream.Unknown, result.Assessment)
			require.Nil(t, result.Release)
			require.NotEmpty(t, result.Detail)
		})
	}
}

func (c *catalog) Repository(instance, name string) (forge.Repository, error) {
	c.instance = instance
	c.name = name
	return c, nil
}
func (c *catalog) Name() string { return c.name }
func (c *catalog) Tag(ctx context.Context, name string) (forge.Tag, error) {
	return c.tag(ctx, c.Name(), name)
}

func TestDiscoveryRecordsMacPortsSourceIdentityAndURL(t *testing.T) {
	c := &catalog{releases: []forge.Release{{Tag: "v2.0"}}}
	service := automaticService(t, c)
	port := automaticPort()
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "https://github.com/owner/project/archive/refs/tags/v2.0.tar.gz", result.Evidence[0].URL)
	require.Equal(t, string(portsource.GitHub), result.Release.Forge)
	require.Equal(t, "https://github.com", result.Release.Instance)
	require.Equal(t, c.Name(), result.Release.Repository)
}

func TestAutomaticGitLabSelectionUsesTagFeedConvention(t *testing.T) {
	c := &catalog{tags: []forge.Tag{{Name: "v1.9"}, {Name: "v1.10"}}}
	service := automaticService(t, c)
	port := macports.PortInfo{Name: "fixture", Version: "1.9", Options: map[string]string{
		"gitlab.author": "group/subgroup", "gitlab.project": "project", "gitlab.version": "1.9",
		"gitlab.tag_prefix": "v", "gitlab.tag_suffix": "", "gitlab.instance": "https://gitlab.example.com/root",
		"git.branch": "v1.9", "livecheck.type": "regex", "livecheck.url": "https://gitlab.example.com/root/group/subgroup/project/-/tags?format=atom",
		"livecheck.regex": `{tags/v([^<]+)</id>}`, "livecheck.version": "1.9",
	}}
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, "1.10", result.CandidateVersion)
	require.Equal(t, "gitlab-tags", result.Evidence[0].Source)
	require.Equal(t, "https://gitlab.example.com/root/group/subgroup/project/-/tags/v1.10", result.Evidence[0].URL)
	require.Equal(t, "https://gitlab.example.com/root", c.instance)
	require.Equal(t, "group/subgroup/project", result.Release.Repository)
	require.Equal(t, "https://gitlab.example.com/root", result.Release.Instance)
}

func TestTagDiscoveryNeverConsultsReleases(t *testing.T) {
	for _, mode := range []string{"", "archive", "tarball"} {
		t.Run("mode="+mode, func(t *testing.T) {
			c := &catalog{tags: []forge.Tag{{Name: "v1.9"}, {Name: "v1.10"}}, releaseErr: errors.New("release catalog must not be read")}
			service := automaticService(t, c)
			port := automaticPort()
			port.Options["github.tarball_from"] = mode
			result, err := service.DiscoverPort(t.Context(), port)
			require.NoError(t, err)
			require.Equal(t, "1.10", result.CandidateVersion)
			require.Equal(t, 1, c.tagReads)
			require.Zero(t, c.releaseReads)
		})
	}
}
