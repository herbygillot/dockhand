package source_test

import (
	"net/url"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/macports/source"
	"github.com/stretchr/testify/require"
)

func githubPort() macports.PortInfo {
	return macports.PortInfo{Name: "fixture", Version: "1.0", Options: map[string]string{
		"github.author": "owner", "github.project": "project", "github.version": "1.0",
		"github.tag_prefix": "release/", "github.tag_suffix": "-stable", "github.tarball_from": "releases",
		"git.branch": "release/1.0-stable", "livecheck.type": "regex", "livecheck.url": "https://github.com/owner/project/tags",
		"livecheck.regex": `{archive/refs/tags/release/([^/]+)-stable\.tar\.gz}`, "livecheck.version": "1.0",
	}}
}

func TestGitHubSourceSeparatesPortfileConventionFromRemoteAccess(t *testing.T) {
	spec, err := source.Discover(githubPort())
	require.NoError(t, err)
	require.Equal(t, source.GitHub, spec.Forge)
	require.Equal(t, source.Releases, spec.Catalog)
	require.Equal(t, "https://github.com", spec.Instance)
	require.Equal(t, "owner/project", spec.Repository)
	require.Equal(t, "release/3.0-stable", spec.Pattern.Tag("3.0"))
	version, ok := spec.Pattern.Version("release/3.0-stable")
	require.True(t, ok)
	require.Equal(t, "3.0", version)
	match, err := spec.MatchText("release/3.0-stable")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/owner/project/archive/refs/tags/release/3.0-stable.tar.gz", match)
}

func TestSourceURLsEscapeTagData(t *testing.T) {
	spec, err := source.Interpret(githubPort())
	require.NoError(t, err)
	value, err := spec.EvidenceURL("release/2#meta%-stable")
	require.NoError(t, err)
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	require.Empty(t, parsed.Fragment)
	require.Equal(t, "/owner/project/archive/refs/tags/release/2#meta%-stable.tar.gz", parsed.Path)
}

func TestSourceInterpretationRejectsAmbiguousOrInconsistentMetadata(t *testing.T) {
	for _, mutate := range []func(*macports.PortInfo){
		func(port *macports.PortInfo) { delete(port.Options, "github.author") },
		func(port *macports.PortInfo) { port.Options["git.branch"] = "other" },
		func(port *macports.PortInfo) { port.OptionErrors = map[string]string{"github.version": "failed"} },
	} {
		port := githubPort()
		mutate(&port)
		_, err := source.Interpret(port)
		require.Error(t, err)
	}
	port := githubPort()
	port.Options["livecheck.url"] = "https://example.invalid/releases"
	_, err := source.Discover(port)
	require.ErrorIs(t, err, source.ErrUnsupported)
}
