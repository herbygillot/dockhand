package source_test

import (
	"net/url"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/source"
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

func gitlabPort() macports.PortInfo {
	return macports.PortInfo{Name: "fixture", Version: "2.0", Options: map[string]string{
		"gitlab.author": "group/subgroup", "gitlab.project": "project", "gitlab.version": "2.0",
		"gitlab.tag_prefix": "v", "gitlab.tag_suffix": "", "gitlab.instance": "https://gitlab.example.com/root/",
		"git.branch": "v2.0", "livecheck.type": "regex", "livecheck.url": "https://gitlab.example.com/root/group/subgroup/project/-/tags?format=atom",
		"livecheck.regex": `{tags/v([^<]+)</id>}`, "livecheck.version": "2.0",
	}}
}

func TestGitHubSourceSeparatesPortfileConventionFromRemoteAccess(t *testing.T) {
	spec, err := source.Interpret(githubPort(), source.Discovery)
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

func TestGitLabSourceRetainsInstanceNamespaceAndAtomMatchText(t *testing.T) {
	spec, err := source.Interpret(gitlabPort(), source.Discovery)
	require.NoError(t, err)
	require.Equal(t, source.GitLab, spec.Forge)
	require.Equal(t, source.Tags, spec.Catalog)
	require.Equal(t, "https://gitlab.example.com/root", spec.Instance)
	require.Equal(t, "group/subgroup/project", spec.Repository)
	match, err := spec.MatchText("v3.0")
	require.NoError(t, err)
	require.Equal(t, "https://gitlab.example.com/root/group/subgroup/project/-/tags/v3.0</id>", match)
	evidence, err := spec.EvidenceURL("v3.0")
	require.NoError(t, err)
	require.Equal(t, "https://gitlab.example.com/root/group/subgroup/project/-/tags/v3.0", evidence)
}

func TestSourceURLsEscapeTagData(t *testing.T) {
	spec, err := source.Interpret(githubPort(), source.Edit)
	require.NoError(t, err)
	value, err := spec.EvidenceURL("release/2#meta%-stable")
	require.NoError(t, err)
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	require.Empty(t, parsed.Fragment)
	require.Equal(t, "/owner/project/archive/refs/tags/release/2#meta%-stable.tar.gz", parsed.Path)
}

func TestSourceInterpretationRejectsAmbiguousOrInconsistentMetadata(t *testing.T) {
	archive := githubPort()
	delete(archive.Options, "github.author")
	spec, err := source.Interpret(archive, source.Edit)
	require.NoError(t, err, "without a forge PortGroup an evaluated version is an archive source for editing")
	require.Empty(t, spec.Forge)
	require.Equal(t, archive.Version, spec.SourceVersion)
	for _, mutate := range []func(*macports.PortInfo){
		func(port *macports.PortInfo) { port.Options["gitlab.author"] = "other" },
		func(port *macports.PortInfo) { port.Options["git.branch"] = "other" },
		func(port *macports.PortInfo) { port.OptionErrors = map[string]string{"github.version": "failed"} },
	} {
		port := githubPort()
		mutate(&port)
		_, err := source.Interpret(port, source.Edit)
		require.Error(t, err)
	}
	port := githubPort()
	port.Options["livecheck.url"] = "https://example.invalid/releases"
	_, err = source.Interpret(port, source.Discovery)
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestSourceSpellingIsIndependentOfCalculatedPortVersion(t *testing.T) {
	port := githubPort()
	port.Version = "20260907"
	port.Options["github.version"] = "2026-09-07"
	port.Options["git.branch"] = "release/2026-09-07-stable"
	port.Options["livecheck.version"] = "2026-09-07"
	spec, err := source.Interpret(port, source.Discovery)
	require.NoError(t, err)
	require.Equal(t, "release/2026-09-14-stable", spec.Pattern.Tag("2026-09-14"))
	version, ok := spec.Pattern.Version("release/2026-09-14-stable")
	require.True(t, ok)
	require.Equal(t, "2026-09-14", version)
	_, ok = spec.Pattern.Version("release/2026-02-31-stable")
	require.True(t, ok)
	port.Version = "20260908"
	_, err = source.Interpret(port, source.Edit)
	require.NoError(t, err)
}
