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
	require.ErrorIs(t, err, source.ErrUnsupported, "an overriding livecheck needs the curl options the listing reads")
}

// A maintainer's own livecheck, anything but the PortGroup's catalog page,
// is run as written and proven against the catalog: the interpretation says
// so and keeps the curl options Base would use. A disabled livecheck, a
// non-regex type, and custom livecheck hooks refuse automatic selection
// with the version named as the way forward.
func TestOverridingLivecheckIsRunAndProvenRatherThanRefused(t *testing.T) {
	overriding := func(port macports.PortInfo) macports.PortInfo {
		port.Options["livecheck.url"] = "https://api.github.com/repos/owner/project/releases/latest"
		port.Options["livecheck.regex"] = `{"tag_name": "release/([^"]+)-stable"}`
		for key, value := range map[string]string{"livecheck.ignore_sslcert": "no", "livecheck.compression": "yes", "livecheck.curloptions": `--append-http-header {Accept: application/json}`, "dockhand.livecheck_standard": "1"} {
			port.Options[key] = value
		}
		return port
	}
	spec, err := source.Interpret(overriding(githubPort()), source.Discovery)
	require.NoError(t, err)
	require.True(t, spec.Livecheck.Overridden)
	require.Equal(t, source.Releases, spec.Catalog, "the archive mode still says what must exist")
	require.Equal(t, "https://api.github.com/repos/owner/project/releases/latest", spec.Livecheck.URL)
	require.Equal(t, map[string]string{"Accept": "application/json"}, spec.Livecheck.Headers)
	require.True(t, spec.Livecheck.Compression)
	require.False(t, spec.Livecheck.Multiline)

	regexm := overriding(githubPort())
	regexm.Options["livecheck.type"] = "regexm"
	regexm.Options["livecheck.url"] = "https://github.com/owner/project/tags"
	spec, err = source.Interpret(regexm, source.Discovery)
	require.NoError(t, err)
	require.True(t, spec.Livecheck.Overridden, "a whole-page match of the tags page is the maintainer's own livecheck, not the PortGroup's")
	require.True(t, spec.Livecheck.Multiline)

	spec, err = source.Interpret(githubPort(), source.Discovery)
	require.NoError(t, err)
	require.False(t, spec.Livecheck.Overridden, "the PortGroup's default stays a catalog query")

	lab := gitlabPort()
	lab.Options["livecheck.url"] = "https://gitlab.example.com/root/group/subgroup/project/-/releases"
	for key, value := range map[string]string{"livecheck.ignore_sslcert": "no", "livecheck.compression": "no", "livecheck.curloptions": "", "dockhand.livecheck_standard": "1"} {
		lab.Options[key] = value
	}
	spec, err = source.Interpret(lab, source.Discovery)
	require.NoError(t, err)
	require.True(t, spec.Livecheck.Overridden)

	for _, test := range []struct {
		name   string
		mutate func(*macports.PortInfo)
		want   string
	}{
		{"disabled", func(port *macports.PortInfo) { port.Options["livecheck.type"] = "none" }, "the port's livecheck is disabled (livecheck.type none); name the version to update to"},
		{"other type", func(port *macports.PortInfo) { port.Options["livecheck.type"] = "sourceforge" }, "livecheck.type sourceforge is not a regex livecheck; name the version to update to"},
		{"custom hooks", func(port *macports.PortInfo) { port.Options["dockhand.livecheck_standard"] = "0" }, "the port's livecheck has custom hooks; name the version to update to"},
		{"insecure", func(port *macports.PortInfo) { port.Options["livecheck.ignore_sslcert"] = "yes" }, "livecheck.ignore_sslcert must be disabled"},
		{"other version", func(port *macports.PortInfo) { port.Options["livecheck.version"] = "9" }, "require a regex livecheck for the evaluated port version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := overriding(githubPort())
			test.mutate(&port)
			_, err := source.Interpret(port, source.Discovery)
			require.ErrorIs(t, err, source.ErrUnsupported)
			require.ErrorContains(t, err, test.want)
			_, err = source.Interpret(port, source.Edit)
			require.NoError(t, err, "an explicit version never needs the livecheck")
		})
	}
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

// An archive source's version is spelled the way livecheck.version spells it,
// which the perl5 PortGroup derives the port version from; its multiline
// regex livecheck is accepted for discovery like the line-oriented one.
func TestArchiveSourceVersionIsTheLivecheckSpelling(t *testing.T) {
	port := macports.PortInfo{Name: "p5.34-json", Version: "4.110.0", Options: map[string]string{
		"livecheck.type": "regexm", "livecheck.url": "https://fastapi.metacpan.org/v1/release/JSON/", "livecheck.regex": `{"name"} : {"JSON-([^"]+?)"}`, "livecheck.version": "4.11",
		"livecheck.ignore_sslcert": "no", "livecheck.compression": "yes", "livecheck.curloptions": "", "dockhand.livecheck_standard": "1",
	}}
	spec, err := source.Interpret(port, source.Edit)
	require.NoError(t, err)
	require.Equal(t, "4.110.0", spec.CurrentVersion)
	require.Equal(t, "4.11", spec.SourceVersion)
	spec, err = source.Interpret(port, source.Discovery)
	require.NoError(t, err)
	require.Equal(t, source.HTTPRegex, spec.Catalog)
	require.True(t, spec.Livecheck.Multiline)
	require.Equal(t, "4.11", spec.SourceVersion)
	port.OptionErrors = map[string]string{"livecheck.version": "cannot evaluate"}
	spec, err = source.Interpret(port, source.Edit)
	require.NoError(t, err)
	require.Equal(t, "4.110.0", spec.SourceVersion, "an unevaluated spelling falls back to the port version")
	_, err = source.Interpret(port, source.Discovery)
	require.Error(t, err)
	delete(port.OptionErrors, "livecheck.version")
	port.Options["livecheck.type"] = "none"
	_, err = source.Interpret(port, source.Discovery)
	require.Error(t, err)
}
