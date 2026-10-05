package macports

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
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

// FetchesNothing reports a port with nothing to download, no distfiles
// and no Git clone, as a metaport or a _select port has; an error where
// its distfiles weren't read.
func (p PortInfo) FetchesNothing() (bool, error) {
	if p.GitFetched() {
		return false, nil
	}
	files, set, err := p.optionList("distfiles")
	if err == nil && !set {
		err = fmt.Errorf("macports: distfiles wasn't read")
	}
	// distfiles {} is a list of one empty name, which names nothing.
	return !slices.ContainsFunc(files, func(file string) bool { return file != "" }), err
}

// OwnVersion reports a port with no release upstream to follow: it
// fetches nothing, and its livecheck reads no version, as a _select
// port's. Its version is MacPorts' own. False where either fact wasn't
// read, so the port is taken as any other.
func (p PortInfo) OwnVersion() bool {
	nothing, err := p.FetchesNothing()
	if err != nil || !nothing {
		return false
	}
	reads, err := p.LivecheckReadsVersion()
	return err == nil && !reads
}

// LivecheckReadsVersion reports a port whose livecheck, as MacPorts
// resolves it, reads a version: regex or regexm, which the tree's checkers
// resolve a PortGroup's type to. none reads nothing, git a branch's
// commit, and fallback, md5, and moddate only whether a page changed.
func (p PortInfo) LivecheckReadsVersion() (bool, error) {
	value, _, err := p.option("livecheck.type")
	return err == nil && (value == "regex" || value == "regexm"), err
}

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

// BuildsInto reports a directory below ${worksrcpath} that the port's
// build makes, which its source needn't have: Cargo's target, for a port
// of the cargo or rust PortGroup, which a release's tarball may carry by
// accident and the next may not (field testing's perry, 0.5.1159 to
// 0.5.1520: "upstream's source no longer has target/").
func (p PortInfo) BuildsInto(directory string) bool {
	if directory != "target" {
		return false
	}
	groups, _ := p.PortGroups()
	return slices.ContainsFunc(groups, func(group string) bool { return group == "cargo" || group == "rust" })
}

// libraryPorts are the ports named otherwise than the native libraries
// they provide, as Rust's -sys crates name them: onig_sys links
// oniguruma6, and libz-sys zlib.
var libraryPorts = map[string][]string{
	"onig":          {"oniguruma6"},
	"libz":          {"zlib"},
	"lzma":          {"xz"},
	"tikv-jemalloc": {"jemalloc"},
}

// LibraryPorts are the names a port providing a native library may have,
// as the library is named, in the order to look for them: the library's
// own, zstd; without its lib prefix, sqlite3 for libsqlite3; and those
// named otherwise, zlib for libz. A port named for a library may be
// versioned too, as openssl3 is, which TiesTo reads.
func LibraryPorts(library string) []string {
	return LibraryPortsAt(library, "")
}

// LibraryPortsAt are LibraryPorts for a library at a version its crate
// names, the versioned port first, as MacPorts names one series of a
// library a port of its own: llvm-22 for LLVM 22.
func LibraryPortsAt(library, version string) []string {
	if version != "" {
		return append([]string{library + "-" + version}, libraryPortNames(library)...)
	}
	return libraryPortNames(library)
}

func libraryPortNames(library string) []string {
	names := []string{library}
	if bare := strings.TrimPrefix(library, "lib"); bare != library && bare != "" {
		names = append(names, bare)
	}
	return append(names, libraryPorts[library]...)
}

// LibraryTies are what of a port is there for a native library, by the
// names MacPorts gives it (LibraryPorts): PortGroups named for it, as
// openssl is, and the ports it depends on named for it, as openssl3 is,
// versioned, sqlite3 for libsqlite3, or oniguruma6 for onig.
type LibraryTies struct {
	PortGroups, Ports []string
}

// TiesTo are what of the port is there for a native library, as the
// library is named; none where nothing is named for it. Only the
// library's own name is taken versioned, so libz's z doesn't name z3.
func (p PortInfo) TiesTo(library string) LibraryTies {
	names := LibraryPorts(library)
	named := func(name string) bool {
		version, ok := strings.CutPrefix(name, library)
		// openssl3, or llvm-22 as MacPorts names a series.
		return ok && strings.Trim(strings.TrimPrefix(version, "-"), "0123456789") == "" || slices.Contains(names[1:], name)
	}
	var ties LibraryTies
	groups, _ := p.PortGroups()
	for _, group := range groups {
		if named(group) {
			ties.PortGroups = append(ties.PortGroups, group)
		}
	}
	for _, dependency := range p.Dependencies {
		if named(dependency.Port) && !slices.Contains(ties.Ports, dependency.Port) {
			ties.Ports = append(ties.Ports, dependency.Port)
		}
	}
	return ties
}

// Patchfiles are the patches the port applies, by name, in order; an error
// where the evaluation couldn't settle them, or they aren't a list.
func (p PortInfo) Patchfiles() ([]string, error) {
	names, _, err := p.optionList("patchfiles")
	return names, err
}

// FilesPath is where one of the port's files, a patch by its name, is in
// its tree: below the port's directory, where filespath names, which the
// evaluation wrote with its own root; files where it wrote none. False
// where filespath isn't below the port's directory.
func (p PortInfo) FilesPath(directory, name string) (string, bool) {
	files := filepath.ToSlash(p.Options["filespath"])
	if files == "" {
		return path.Join(directory, "files", name), true
	}
	files += "/"
	i := strings.LastIndex(files, "/"+directory+"/")
	if i < 0 {
		return "", false
	}
	return path.Join(directory, files[i+len(directory)+2:], name), true
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

// MinimumXcode is the Xcode the port requires where it was evaluated, by
// minimum_xcodeversions for that macOS, where the Xcode it was evaluated
// with is older, or there's none; empty where that's met, or the port
// declares none. An error where it couldn't be read.
func (p PortInfo) MinimumXcode() (string, error) {
	if problem := p.OptionErrors["dockhand.minimum_xcode"]; problem != "" {
		return "", fmt.Errorf("minimum_xcodeversions: %s", problem)
	}
	return p.Options["dockhand.minimum_xcode"], nil
}

// Xcode is the Xcode the port was evaluated with, where its minimum
// (MinimumXcode) isn't met: a version, or "none" for the Command Line
// Tools alone; empty where the minimum is met, or the evaluation didn't
// say.
func (p PortInfo) Xcode() string {
	return p.Options["dockhand.xcode"]
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
