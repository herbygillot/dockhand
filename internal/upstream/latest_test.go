package upstream_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/stretchr/testify/require"
)

type catalog struct {
	releases []upstream.Release
	tags     []upstream.Tag
	err      error
	tagReads int
}

func (c *catalog) Releases(context.Context, string) ([]upstream.Release, error) {
	return c.releases, c.err
}

func (c *catalog) ListTags(context.Context, string) ([]upstream.Tag, error) {
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
	return &upstream.Service{Releases: c, Versions: &macports.Evaluator{Executable: executable}, Tags: tagFunc(func(_ context.Context, _ string, name string) (upstream.Tag, error) {
		return upstream.Tag{Name: name, Commit: strings.Repeat("a", 40)}, nil
	})}
}

func TestAutomaticSelectionHonorsArchiveModeVersionOrderingAndPrereleases(t *testing.T) {
	c := &catalog{releases: []upstream.Release{{Tag: "v1.9"}, {Tag: "v1.10"}, {Tag: "v2.0", Prerelease: true}, {Tag: "v3.0", Draft: true}, {Tag: "v4.0-rc1"}, {Tag: "other-99.0"}}, tags: []upstream.Tag{{Name: "v1.9"}, {Name: "v1.10"}, {Name: "v1.11"}, {Name: "v2.0"}, {Name: "v4.0-rc1"}}}
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
	require.Equal(t, "1.11", result.CandidateVersion)
	require.Equal(t, 1, c.tagReads)
	c.releases = nil
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, "2.0", result.CandidateVersion, "tag-only projects need no GitHub Release")
}

func TestAutomaticSelectionAppliesMaintainerFilterAndReportsCurrentOrAhead(t *testing.T) {
	c := &catalog{releases: []upstream.Release{{Tag: "v1.0"}, {Tag: "v2.0"}}}
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
			c := &catalog{releases: []upstream.Release{{Tag: "v2.0"}}}
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
				c.releases = append(c.releases, upstream.Release{Tag: "v2.00"})
			case "missing selected tag":
				service.Tags = tagFunc(func(context.Context, string, string) (upstream.Tag, error) {
					return upstream.Tag{}, upstream.ErrTagMissing
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
