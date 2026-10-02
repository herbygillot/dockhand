package portsource_test

import (
	"net/url"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
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
	spec, err := portsource.Interpret(githubPort(), portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, portsource.GitHub, spec.Forge)
	require.Equal(t, portsource.Releases, spec.Catalog)
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
	spec, err := portsource.Interpret(gitlabPort(), portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, portsource.GitLab, spec.Forge)
	require.Equal(t, portsource.Tags, spec.Catalog)
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
	spec, err := portsource.Interpret(githubPort(), portsource.Edit)
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
	spec, err := portsource.Interpret(archive, portsource.Edit)
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
		_, err := portsource.Interpret(port, portsource.Edit)
		require.Error(t, err)
	}
	port := githubPort()
	port.Options["livecheck.url"] = "https://example.invalid/releases"
	_, err = portsource.Interpret(port, portsource.Discovery)
	require.ErrorIs(t, err, portsource.ErrUnsupported, "an overriding livecheck needs the curl options the listing reads")
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
	spec, err := portsource.Interpret(overriding(githubPort()), portsource.Discovery)
	require.NoError(t, err)
	require.True(t, spec.Livecheck.Overridden)
	require.Equal(t, portsource.Releases, spec.Catalog, "the archive mode still says what must exist")
	require.Equal(t, "https://api.github.com/repos/owner/project/releases/latest", spec.Livecheck.URL)
	require.Equal(t, map[string]string{"Accept": "application/json"}, spec.Livecheck.Headers)
	require.True(t, spec.Livecheck.Compression)
	require.False(t, spec.Livecheck.Multiline)

	regexm := overriding(githubPort())
	regexm.Options["livecheck.type"] = "regexm"
	regexm.Options["livecheck.url"] = "https://github.com/owner/project/tags"
	spec, err = portsource.Interpret(regexm, portsource.Discovery)
	require.NoError(t, err)
	require.True(t, spec.Livecheck.Overridden, "a whole-page match of the tags page is the maintainer's own livecheck, not the PortGroup's")
	require.True(t, spec.Livecheck.Multiline)

	spec, err = portsource.Interpret(githubPort(), portsource.Discovery)
	require.NoError(t, err)
	require.False(t, spec.Livecheck.Overridden, "the PortGroup's default stays a catalog query")

	lab := gitlabPort()
	lab.Options["livecheck.url"] = "https://gitlab.example.com/root/group/subgroup/project/-/releases"
	for key, value := range map[string]string{"livecheck.ignore_sslcert": "no", "livecheck.compression": "no", "livecheck.curloptions": "", "dockhand.livecheck_standard": "1"} {
		lab.Options[key] = value
	}
	spec, err = portsource.Interpret(lab, portsource.Discovery)
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
		{"insecure as Tcl spells it", func(port *macports.PortInfo) { port.Options["livecheck.ignore_sslcert"] = "On" }, "livecheck.ignore_sslcert must be disabled"},
		{"compression not a boolean", func(port *macports.PortInfo) { port.Options["livecheck.compression"] = "sometimes" }, `invalid livecheck.compression value "sometimes"`},
		{"other version", func(port *macports.PortInfo) { port.Options["livecheck.version"] = "9" }, "require a regex livecheck for the evaluated port version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := overriding(githubPort())
			test.mutate(&port)
			_, err := portsource.Interpret(port, portsource.Discovery)
			require.ErrorIs(t, err, portsource.ErrUnsupported)
			require.ErrorContains(t, err, test.want)
			_, err = portsource.Interpret(port, portsource.Edit)
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
	spec, err := portsource.Interpret(port, portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, "release/2026-09-14-stable", spec.Pattern.Tag("2026-09-14"))
	version, ok := spec.Pattern.Version("release/2026-09-14-stable")
	require.True(t, ok)
	require.Equal(t, "2026-09-14", version)
	_, ok = spec.Pattern.Version("release/2026-02-31-stable")
	require.True(t, ok)
	port.Version = "20260908"
	_, err = portsource.Interpret(port, portsource.Edit)
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
	spec, err := portsource.Interpret(port, portsource.Edit)
	require.NoError(t, err)
	require.Equal(t, "4.110.0", spec.CurrentVersion)
	require.Equal(t, "4.11", spec.SourceVersion)
	spec, err = portsource.Interpret(port, portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, portsource.HTTPRegex, spec.Catalog)
	require.True(t, spec.Livecheck.Multiline)
	require.Equal(t, "4.11", spec.SourceVersion)
	port.OptionErrors = map[string]string{"livecheck.version": "cannot evaluate"}
	spec, err = portsource.Interpret(port, portsource.Edit)
	require.NoError(t, err)
	require.Equal(t, "4.110.0", spec.SourceVersion, "an unevaluated spelling falls back to the port version")
	_, err = portsource.Interpret(port, portsource.Discovery)
	require.Error(t, err)
	delete(port.OptionErrors, "livecheck.version")
	port.Options["livecheck.type"] = "none"
	_, err = portsource.Interpret(port, portsource.Discovery)
	require.Error(t, err)
}

// A port that tracks a branch, livecheck.type git, is read as Base's git
// livecheck reads it: the repository, its branch, HEAD where none is
// named, and the commit it pins; a forge port and a plain one alike
// (batch 30).
func TestAGitLivecheckTracksABranch(t *testing.T) {
	port := githubPort()
	port.Version = "20231125"
	port.Options["github.version"] = "42ffa05d4aca7941be9d9b90c5d243b69521dd61"
	port.Options["github.tag_prefix"], port.Options["github.tag_suffix"], port.Options["github.tarball_from"] = "", "", "archive"
	port.Options["git.branch"] = port.Options["github.version"]
	port.Options["livecheck.type"], port.Options["livecheck.url"], port.Options["livecheck.regex"] = "git", "https://github.com/owner/project.git", ""
	port.Options["livecheck.version"], port.Options["livecheck.branch"] = port.Options["github.version"], ""
	spec, err := portsource.Interpret(port, portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, portsource.GitHead, spec.Catalog)
	require.Equal(t, portsource.Livecheck{Type: "git", URL: "https://github.com/owner/project.git", Version: port.Options["github.version"], Branch: "HEAD"}, spec.Livecheck)

	plain := macports.PortInfo{Name: "goat", Version: "20220814", Options: map[string]string{
		"livecheck.type": "git", "livecheck.url": "https://example.org/goat.git", "livecheck.version": "6d4db35", "livecheck.branch": "main", "distname": "goat-20220814",
	}}
	spec, err = portsource.Interpret(plain, portsource.Discovery)
	require.NoError(t, err)
	require.Equal(t, portsource.GitHead, spec.Catalog)
	require.Equal(t, "main", spec.Livecheck.Branch)
}
