// Package newport writes a new port's first Portfile (Design v3 §6.4) from
// what can be observed of an upstream project: its forge, its latest
// release, and the build files at that release. It fills in what it
// observed and marks what it guessed with a comment, since a guessed
// license or maintainer must never read as fact. The layout is what `port
// lint --nitpick` expects: the modeline, values in one column, and rmd160,
// sha256, and size checksums.
package newport

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Modeline is the first line of every Portfile in MacPorts' tree.
const Modeline = "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"

// Unconfirmed begins the comment that marks a guessed line.
const Unconfirmed = "# dockhand: unconfirmed,"

// Build is how the project builds, as its files say.
type Build struct {
	// System is cargo, go, cmake, meson, python, autotools, autoreconf, or
	// empty when nothing was recognized.
	System string
	// Evidence is the file that says so.
	Evidence string
}

// buildFiles are the top-level files Detect reads, most telling first.
var buildFiles = []struct{ file, system string }{
	{"Cargo.toml", "cargo"}, {"go.mod", "go"}, {"meson.build", "meson"}, {"CMakeLists.txt", "cmake"},
	{"pyproject.toml", "python"}, {"setup.py", "python"}, {"configure", "autotools"}, {"configure.ac", "autoreconf"},
}

// Files are the top-level files Detect and Write read, to be fetched at
// the release.
func Files() []string {
	files := []string{"Cargo.lock"}
	for _, f := range buildFiles {
		files = append(files, f.file)
	}
	return files
}

// Detect names the build system from the top-level files present.
func Detect(files map[string][]byte) Build {
	for _, f := range buildFiles {
		if _, ok := files[f.file]; ok {
			return Build{System: f.system, Evidence: f.file}
		}
	}
	return Build{}
}

// Language words a build system for a person.
func (b Build) Language() string {
	switch b.System {
	case "cargo":
		return "Rust"
	case "go":
		return "Go"
	case "python":
		return "Python"
	case "cmake", "meson", "autotools", "autoreconf":
		return b.System
	}
	return "build system not recognized"
}

// Category is the category a port of this kind usually goes in.
func (b Build) Category() string {
	if b.System == "python" {
		return "python"
	}
	return "devel"
}

// Crate is one registry dependency a Cargo.lock pins.
type Crate struct{ Name, Version, Checksum string }

// CargoCrates reads the registry crates a Cargo.lock pins, with their
// checksums, sorted as cargo2port writes them. Path and git dependencies,
// which carry no checksum, are not crates to fetch.
func CargoCrates(lock []byte) ([]Crate, error) {
	var parsed struct {
		Package []struct {
			Name, Version, Source, Checksum string
		} `toml:"package"`
	}
	if _, err := toml.Decode(string(lock), &parsed); err != nil {
		return nil, fmt.Errorf("reading Cargo.lock: %w", err)
	}
	var crates []Crate
	for _, p := range parsed.Package {
		if strings.HasPrefix(p.Source, "registry+") && p.Checksum != "" {
			crates = append(crates, Crate{Name: p.Name, Version: p.Version, Checksum: p.Checksum})
		}
	}
	slices.SortFunc(crates, func(a, b Crate) int {
		return strings.Compare(a.Name+" "+a.Version, b.Name+" "+b.Version)
	})
	return crates, nil
}

// Spec is what a new Portfile says.
type Spec struct {
	Name     string
	Category string
	// CategoryGuessed marks the category as unconfirmed.
	CategoryGuessed bool
	// Owner and Project are the GitHub repository; Version and TagPrefix
	// make the release's tag.
	Owner, Project     string
	Version, TagPrefix string
	Description        string
	Homepage           string
	// License is MacPorts' name for the forge's detection; empty when it
	// detected none, or one MacPorts has no name for.
	License string
	// Maintainer is the maintainers line; nomaintainer when empty.
	Maintainer string
	Build      Build
	Crates     []Crate
}

// Unconfirmed lists what the Portfile marks as guessed.
func (s Spec) Unconfirmed() []string {
	var marked []string
	if s.CategoryGuessed {
		marked = append(marked, "category")
	}
	marked = append(marked, "license", "long_description")
	if s.Maintainer == "" {
		marked = append(marked, "maintainers")
	}
	if s.Build.System == "" || s.Build.System == "python" || s.Build.System == "go" {
		marked = append(marked, "build")
	}
	return marked
}

// Write is the Portfile, with placeholder checksums for `dockhand
// checksums` to fill in.
func Write(s Spec) []byte {
	var b strings.Builder
	line := func(key, value string) { fmt.Fprintf(&b, "%-20s%s\n", key, value) }
	mark := func(why string) { fmt.Fprintf(&b, "%s %s\n", Unconfirmed, why) }
	fmt.Fprintf(&b, "%s\n\n", Modeline)
	line("PortSystem", "1.0")
	switch s.Build.System {
	case "go":
		line("PortGroup", "golang 1.0")
	default:
		line("PortGroup", "github 1.0")
	}
	switch s.Build.System {
	case "cargo":
		line("PortGroup", "cargo 1.0")
	case "cmake":
		line("PortGroup", "cmake 1.1")
	case "meson":
		line("PortGroup", "meson 1.0")
	case "python":
		line("PortGroup", "python 1.0")
	}
	b.WriteString("\n")
	setup := strings.TrimSpace(fmt.Sprintf("%s %s %s %s", s.Owner, s.Project, s.Version, s.TagPrefix))
	if s.Build.System == "go" {
		line("go.setup", strings.TrimSpace(fmt.Sprintf("github.com/%s/%s %s %s", s.Owner, s.Project, s.Version, s.TagPrefix)))
	} else {
		line("github.setup", setup)
		line("github.tarball_from", "archive")
	}
	if s.Name != s.Project {
		line("name", s.Name)
	}
	line("revision", "0")
	b.WriteString("\n")
	if s.CategoryGuessed {
		mark("guessed from the build system")
	}
	line("categories", s.Category)
	if s.License == "" {
		mark("the forge detected no license MacPorts names; read the project's license")
		line("license", "unknown")
	} else {
		mark("from the forge's license detection")
		line("license", s.License)
	}
	if s.Maintainer == "" {
		mark("no maintainer is set in dockhand's config")
		line("maintainers", "nomaintainer")
	} else {
		line("maintainers", s.Maintainer)
	}
	b.WriteString("\n")
	line("description", tclWord(s.Description))
	mark("write a longer description")
	line("long_description", "{*}${description}")
	if s.Homepage != "" {
		b.WriteString("\n")
		line("homepage", s.Homepage)
	}
	b.WriteString("\n")
	line("checksums", "rmd160  0 \\")
	fmt.Fprintf(&b, "%-20s%s\n", "", "sha256  0 \\")
	fmt.Fprintf(&b, "%-20s%s\n", "", "size    0")
	switch s.Build.System {
	case "python":
		b.WriteString("\n")
		mark("pick the Python versions, and whether it fetches from PyPI instead")
		line("python.versions", "313")
	case "go":
		b.WriteString("\n")
		mark("run go2port for go.vendors, and check the build")
		line("build.cmd", "${go.bin} build")
	case "autoreconf":
		b.WriteString("\n")
		line("use_autoreconf", "yes")
	case "":
		b.WriteString("\n")
		mark("the build system was not recognized; say how it builds")
	}
	if s.Build.System == "cargo" {
		b.WriteString("\n")
		if len(s.Crates) == 0 {
			mark("no Cargo.lock upstream; run cargo2port for cargo.crates")
		} else {
			b.WriteString("cargo.crates \\\n")
			width := 0
			for _, c := range s.Crates {
				width = max(width, len(c.Name))
			}
			vwidth := 0
			for _, c := range s.Crates {
				vwidth = max(vwidth, len(c.Version))
			}
			for i, c := range s.Crates {
				end := " \\"
				if i == len(s.Crates)-1 {
					end = ""
				}
				fmt.Fprintf(&b, "    %-*s  %-*s  %s%s\n", width, c.Name, vwidth, c.Version, c.Checksum, end)
			}
		}
	}
	return []byte(b.String())
}

// tclWord writes a description as MacPorts does, as plain words, braced
// only when a character would mean something to Tcl.
func tclWord(value string) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return "{}"
	}
	if strings.ContainsAny(value, "{}[]$\\\";") {
		escaped := strings.NewReplacer("{", "\\{", "}", "\\}", "\\", "\\\\").Replace(value)
		return "{" + escaped + "}"
	}
	return value
}

// licenses are MacPorts' names for the SPDX identifiers forges report.
var licenses = map[string]string{
	"MIT": "MIT", "Apache-2.0": "Apache-2", "BSD-2-Clause": "BSD", "BSD-3-Clause": "BSD", "ISC": "ISC",
	"GPL-2.0": "GPL-2", "GPL-2.0-only": "GPL-2", "GPL-2.0-or-later": "GPL-2+", "GPL-3.0": "GPL-3", "GPL-3.0-only": "GPL-3", "GPL-3.0-or-later": "GPL-3+",
	"LGPL-2.1": "LGPL-2.1", "LGPL-2.1-only": "LGPL-2.1", "LGPL-2.1-or-later": "LGPL-2.1+", "LGPL-3.0": "LGPL-3", "LGPL-3.0-only": "LGPL-3", "LGPL-3.0-or-later": "LGPL-3+",
	"AGPL-3.0": "AGPL-3", "AGPL-3.0-only": "AGPL-3", "AGPL-3.0-or-later": "AGPL-3+", "MPL-2.0": "MPL-2", "Unlicense": "Unlicense", "Zlib": "zlib",
	"BSL-1.0": "Boost-1", "0BSD": "BSD", "CC0-1.0": "CC0-1", "EPL-2.0": "EPL-2", "Artistic-2.0": "Artistic-2", "WTFPL": "WTFPL-2",
}

// License is MacPorts' name for an SPDX identifier, or empty for one it
// has no name for, such as GitHub's NOASSERTION.
func License(spdx string) string { return licenses[spdx] }

// SplitTag parses a release tag into the prefix before the version and the
// version: v0.4.2 is "v" and 0.4.2, rift-0.4.2 is "rift-" and 0.4.2.
func SplitTag(tag string) (prefix, version string, ok bool) {
	i := strings.IndexFunc(tag, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 || strings.ContainsAny(tag, " /") {
		return "", "", false
	}
	return tag[:i], tag[i:], true
}
