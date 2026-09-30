package macports

import (
	"errors"
	"regexp"
	"slices"

	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The facts the evaluator computes of a port, beside its options, each read
// here as its own type. They travel in Options, under a dockhand. or fetch.
// key, so fidelity compares them as it compares options; nothing else reads
// those keys as strings (the code-organization review's finding 27).

// MetadataOnly reports a port that builds nothing itself, as a python
// stub over its subports doesn't; an error where the evaluator's probe of
// it failed.
func (p PortInfo) MetadataOnly() (bool, error) { return p.Bool("dockhand.metadata_only") }

// LivecheckStandard reports a port whose livecheck is MacPorts' own,
// without a procedure of its own replacing it.
func (p PortInfo) LivecheckStandard() (bool, error) { return p.Bool("dockhand.livecheck_standard") }

// DeclaresTests reports whether the port declares tests, as MacPorts
// reads test.run; known is false where that wasn't read.
func (p PortInfo) DeclaresTests() (declares, known bool) {
	if _, set := p.Options["dockhand.test_run"]; !set {
		return false, false
	}
	declares, err := p.Bool("dockhand.test_run")
	return declares, err == nil
}

// PortGroups are the PortGroups the port loads, by name; known is false
// where they weren't read.
func (p PortInfo) PortGroups() (groups []string, known bool) {
	groups, set, err := p.optionList("dockhand.portgroups")
	return groups, set && err == nil
}

// FetchCredentials reports whether MacPorts credentials apply to the port's
// downloads; an error where the evaluator couldn't tell.
func (p PortInfo) FetchCredentials() (bool, error) {
	if _, set := p.Options["fetch.has_credentials"]; !set {
		return false, errors.New("macports: fetch.has_credentials was not evaluated")
	}
	return p.Bool("fetch.has_credentials")
}

// ArchiveCompatible reports whether MacPorts' fetch of the port's archives
// is one the direct downloader can repeat: false with why for a custom
// fetch, as the evaluator's assessment of it says; an error where it
// couldn't assess it.
func (p PortInfo) ArchiveCompatible() (bool, string, error) {
	if failure := p.OptionErrors["fetch.archive_compatible"]; failure != "" {
		return false, "", errors.New(failure)
	}
	if _, set := p.Options["fetch.archive_compatible"]; !set {
		return false, "", errors.New("macports: the fetch was not assessed")
	}
	compatible, err := p.Bool("fetch.archive_compatible")
	if err != nil || compatible {
		return compatible, "", err
	}
	problem := "the port customizes its fetch"
	if p.Fetch != nil && p.Fetch.Problem != "" {
		problem = p.Fetch.Problem
	}
	if base, ok := p.BaseVersion(); ok {
		problem = "MacPorts Base " + base + ": " + problem
	}
	return false, problem, nil
}

// KnownFail reports a port known to fail where it was evaluated, as
// MacPorts tests known_fail, string is true -strict, in its interpreter;
// an error where it couldn't be read. A port evaluated without the fact,
// as a test's is, is read as Tcl reads a boolean.
func (p PortInfo) KnownFail() (bool, error) {
	if _, set := p.Options["dockhand.known_fail"]; set || p.OptionErrors["dockhand.known_fail"] != "" {
		return p.Bool("dockhand.known_fail")
	}
	return p.Bool("known_fail")
}

// PlatformsCompatible reports whether the port's platforms admit the
// release it was evaluated for, as Base's own check decides; known is
// false where it couldn't say.
func (p PortInfo) PlatformsCompatible() (compatible, known bool) {
	if _, set := p.Options["dockhand.platforms_compatible"]; !set {
		return false, false
	}
	compatible, err := p.Bool("dockhand.platforms_compatible")
	return compatible, err == nil
}

// BaseVersion is the MacPorts Base that evaluated the port; false where it
// wasn't read.
func (p PortInfo) BaseVersion() (string, bool) {
	version := p.Options["dockhand.base_version"]
	return version, version != "" && p.OptionErrors["dockhand.base_version"] == ""
}

// IsLivecheckOption reports an option of a port's livecheck, its own or
// the evaluator's reading of it, which a stub's subport takes from the
// stub as one.
func IsLivecheckOption(key string) bool {
	return strings.HasPrefix(key, "livecheck.") || strings.HasPrefix(key, "dockhand.livecheck_")
}

// PlainURL is a URL a port names over plain HTTP, and the option it's in.
type PlainURL struct {
	Option, URL string
}

// PlainHTTP are the port's URLs over plain HTTP, which MacPorts prefers
// over HTTPS: its homepage, and each of its master_sites that is a URL
// (without the ":tag" that names which distfiles it serves). A mirror
// group, "gnu" or "sourceforge:project", is MacPorts' own list, and none
// of the port's.
func (p PortInfo) PlainHTTP() []PlainURL {
	var plain []PlainURL
	if homepage := p.Options["homepage"]; strings.HasPrefix(homepage, "http://") {
		plain = append(plain, PlainURL{Option: "homepage", URL: homepage})
	}
	sites, _ := syntax.ListValues(p.Options["master_sites"])
	for _, site := range sites {
		if !strings.HasPrefix(site, "http://") {
			continue
		}
		// Its tags, a mirror option or which distfiles it serves, are
		// taken off as Base takes them, one or two.
		for range 2 {
			if m := taggedURL.FindStringSubmatch(site); m != nil {
				site = m[1]
			}
		}
		if !slices.ContainsFunc(plain, func(u PlainURL) bool { return u.URL == site }) {
			plain = append(plain, PlainURL{Option: "master_sites", URL: site})
		}
	}
	return plain
}

// taggedURL is Base's pattern for a site with a tag after it
// (fetch_common.tcl, tagged_url_re).
var taggedURL = regexp.MustCompile(`^([a-zA-Z]+://.+/?):([0-9A-Za-z_-]+)$`)

// Description is the port's one line as a person reads it: its
// description option's words, which the evaluation reports as a Tcl list.
func (p PortInfo) Description() string {
	words, _, err := p.optionList("description")
	if err != nil {
		return strings.TrimSpace(p.Options["description"])
	}
	return strings.Join(words, " ")
}

// GitFetched reports a port whose source is a Git checkout, fetch.type git,
// as MacPorts evaluated it, rather than archives.
func (p PortInfo) GitFetched() bool {
	return p.Options["fetch.type"] == "git" && p.OptionErrors["fetch.type"] == ""
}
