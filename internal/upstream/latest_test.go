package upstream_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	"os/exec"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
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
	file         func(commit, path string) ([]byte, error)
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
	return &upstream.Service{EvaluateVersion: identityVersion, Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: c, portsource.GitLab: c}, Versions: &eval.Evaluator{Executable: executable}}

}

func TestAutomaticSelectionHonorsArchiveModeVersionOrderingAndPrereleases(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	for _, mode := range []string{"network", "no matches", "prerelease only", "invalid regex", "ambiguous", "missing selected tag", "custom source", "unknown current"} {
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
			case "unknown current":
				// A patch-letter spelling is neither stable nor a prerelease.
				port.Version = "1.0.2u"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestCalendarTagSelectionAndExplicitVersions(t *testing.T) {
	t.Parallel()
	c := &catalog{tags: []forge.Tag{{Name: "v2026-09-07"}, {Name: "v2026-09-14"}, {Name: "v2026-02-31"}, {Name: "nightly"}}}
	service := automaticService(t, c)
	port := automaticPort()
	port.Version = "20260907"
	port.Options["version"] = port.Version
	port.Options["github.version"] = "2026-09-07"
	port.Options["git.branch"] = "v2026-09-07"
	port.Options["github.tarball_from"] = "archive"
	port.Options["livecheck.version"] = "2026-09-07"
	service.EvaluateVersion = func(_ context.Context, raw string) (string, error) { return strings.ReplaceAll(raw, "-", ""), nil }
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, "20260914", result.CandidateVersion)
	require.Equal(t, "v2026-09-14", result.Release.Tag)
	require.NoError(t, service.Check(t.Context(), port, *result.Release))
	// The fake repository should only recognize real observed tags.
	c.tag = tagFunc(func(_ context.Context, _ string, name string) (forge.Tag, error) {
		if name != "v2026-09-14" {
			return forge.Tag{}, forge.ErrNotFound
		}
		return forge.Tag{Name: name, Commit: strings.Repeat("a", 40)}, nil
	})
	for _, input := range []string{"2026-09-14", "v2026-09-14"} {
		release, err := service.Resolve(t.Context(), port, input)
		require.NoError(t, err)
		require.Equal(t, "20260914", release.Version)
		require.Equal(t, "v2026-09-14", release.Tag)
	}
}

func TestAutomaticSelectionOrdersEvaluatedVersions(t *testing.T) {
	t.Parallel()
	c := &catalog{tags: []forge.Tag{{Name: "v2.0"}, {Name: "v3.0"}, {Name: "nightly"}}}
	service := automaticService(t, c)
	port := automaticPort()
	port.Options["github.tarball_from"] = "archive"
	var evaluated []string
	service.EvaluateVersion = func(_ context.Context, source string) (string, error) {
		evaluated = append(evaluated, source)
		if source == "2.0" {
			return "10.0", nil
		}
		return "9.0", nil
	}
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"2.0", "3.0"}, evaluated)
	require.Equal(t, "v2.0", result.Release.Tag)
	require.Equal(t, "10.0", result.Release.Version)
	require.False(t, result.Release.NoUpdate)
	service.EvaluateVersion = func(context.Context, string) (string, error) { return "1.0", nil }
	result, err = service.DiscoverPort(t.Context(), port)
	require.ErrorIs(t, err, upstream.ErrReleaseAmbiguous)
	c.tags = c.tags[:1]
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.True(t, result.Release.NoUpdate)
}

func TestAutomaticSelectionDoesNotHideFailedEvaluation(t *testing.T) {
	t.Parallel()
	c := &catalog{tags: []forge.Tag{{Name: "v2.0"}}}
	service := automaticService(t, c)
	port := automaticPort()
	port.Options["github.tarball_from"] = "archive"
	failed := errors.New("candidate evaluation failed")
	service.EvaluateVersion = func(context.Context, string) (string, error) { return "", failed }
	result, err := service.DiscoverPort(t.Context(), port)
	require.ErrorIs(t, err, failed)
	require.Equal(t, upstream.Unknown, result.Assessment)
	require.Nil(t, result.Release)
}

func TestAutomaticSelectionFollowsPrereleasesForPrereleasePorts(t *testing.T) {
	t.Parallel()
	c := &catalog{tags: []forge.Tag{{Name: "v3.0"}, {Name: "v4.0-rc1"}, {Name: "v4.0-rc2"}, {Name: "v4.0-beta.9"}}}
	service := automaticService(t, c)
	port := automaticPort()
	port.Version, port.Options["version"], port.Options["github.version"], port.Options["git.branch"], port.Options["livecheck.version"] = "4.0-rc1", "4.0-rc1", "4.0-rc1", "v4.0-rc1", "4.0-rc1"
	port.Options["github.tarball_from"] = "archive"
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "4.0-rc2", result.CandidateVersion, "a port on a prerelease follows prereleases")
	require.Equal(t, "prerelease", result.Release.Stability)
	require.False(t, result.Release.LeavesStable, "moving between prereleases does not leave stable")
	stable := automaticPort()
	stable.Options["github.tarball_from"] = "archive"
	stable.Version, stable.Options["version"], stable.Options["github.version"], stable.Options["git.branch"], stable.Options["livecheck.version"] = "3.0", "3.0", "3.0", "v3.0", "3.0"
	result, err = service.DiscoverPort(t.Context(), stable)
	require.NoError(t, err)
	require.Equal(t, upstream.Current, result.Assessment, "a stable port never selects a prerelease automatically")
	patch := automaticPort()
	patch.Version = "1.0.2u"
	_, err = service.DiscoverPort(t.Context(), patch)
	require.ErrorContains(t, err, "stable or prerelease numeric version")
}

func (c *catalog) File(_ context.Context, commit, path string, _ int64) ([]byte, error) {
	if c.file == nil {
		return nil, fmt.Errorf("%w: %s", forge.ErrNotFound, path)
	}
	return c.file(commit, path)
}

// A manifest is read from the resolved release's repository at its commit,
// only when the release names this port's repository; an absent file is a
// missing manifest, as the archive read reports it.
func TestManifestReadsTheReleaseCommit(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	c := &catalog{file: func(at, path string) ([]byte, error) {
		if at == commit && path == "go.mod" {
			return []byte("go 1.25\n"), nil
		}
		return nil, forge.ErrNotFound
	}}
	service := automaticService(t, c)
	port := automaticPort()
	release := record.Release{Version: "2.0", Forge: "github", Instance: "https://github.com", Repository: "owner/project", Tag: "v2.0", Commit: commit}
	data, err := service.Manifest(t.Context(), port, release, "go.mod")
	require.NoError(t, err)
	require.Equal(t, "go 1.25\n", string(data))
	_, err = service.Manifest(t.Context(), port, release, "Cargo.toml")
	require.ErrorIs(t, err, macports.ErrManifestMissing)
	other := release
	other.Repository = "owner/other"
	_, err = service.Manifest(t.Context(), port, other, "go.mod")
	require.Error(t, err, "the release must name this port's repository")
	_, err = service.Manifest(t.Context(), port, record.Release{Archive: true, Version: "2.0"}, "go.mod")
	require.Error(t, err, "an archive release has no commit to read at")
}
