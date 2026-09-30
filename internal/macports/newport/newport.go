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

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
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

// Crate is one crates.io dependency a Cargo.lock pins.
type Crate struct{ Name, Version, Checksum string }

// CargoCrates reads the crates.io crates a Cargo.lock pins, with their
// checksums, sorted as cargo2port writes them, from the reading an update
// uses too (dependency.ReadCargoLock). A crate cargo.crates can't fetch,
// from another registry or from Git, is named in unfetched instead, for
// the Portfile to mark; the project's own packages are neither.
func CargoCrates(lock []byte) (crates []Crate, unfetched []string, err error) {
	packages, err := dependency.ReadCargoLock(lock)
	if err != nil {
		return nil, nil, fmt.Errorf("reading Cargo.lock: %w", err)
	}
	for _, p := range packages {
		switch p.Source {
		case dependency.FromCratesIO:
			crates = append(crates, Crate{Name: p.Name, Version: p.Version, Checksum: p.Checksum})
		case dependency.FromRegistry:
			unfetched = append(unfetched, fmt.Sprintf("%s %s comes from another registry, %s, which cargo.crates can't fetch", p.Name, p.Version, p.Origin))
		case dependency.FromGit:
			unfetched = append(unfetched, fmt.Sprintf("%s comes from Git, %s; cargo2port writes its cargo.crates_github", p.Name, strings.TrimPrefix(p.Origin, "git+")))
		}
	}
	slices.SortFunc(crates, func(a, b Crate) int {
		return strings.Compare(a.Name+" "+a.Version, b.Name+" "+b.Version)
	})
	return crates, unfetched, nil
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
	// License is the license line, in MacPorts' words (macports.License);
	// empty where none could be said. LicenseFrom is the manifest it came
	// from, Cargo.toml or pyproject.toml, and empty for the forge's
	// detection.
	License, LicenseFrom string
	// Maintainer is the maintainers line; nomaintainer when empty.
	Maintainer string
	Build      Build
	// Binaries are the programs a Cargo or Go build makes, which its
	// destroot installs (Binaries).
	Binaries []string
	Crates   []Crate
	// Unfetched are the crates cargo.crates can't fetch, each said as the
	// Portfile marks it.
	Unfetched []string
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
	// Neither PortGroup installs anything itself.
	if s.Build.System == "cargo" || s.Build.System == "go" {
		marked = append(marked, "destroot")
	}
	if len(s.Unfetched) > 0 {
		marked = append(marked, "cargo.crates")
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
		mark("neither the project's manifest nor the forge names a license MacPorts has a name for; read the project's license")
		line("license", "unknown")
	} else {
		if s.LicenseFrom != "" {
			mark("from " + s.LicenseFrom + "'s license field")
		} else {
			mark("from the forge's license detection")
		}
		line("license", s.License)
	}
	if s.Maintainer == "" {
		mark("no maintainer is set in dockhand's config")
		line("maintainers", macports.NoMaintainer)
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
	// A Cargo or Go port's crates or modules are distfiles too, whose
	// checksums they append, so the port's own names its file, as the
	// tree's do; lint refuses one that doesn't.
	if s.Build.System == "cargo" || s.Build.System == "go" {
		line("checksums", "${distname}${extract.suffix} \\")
		fmt.Fprintf(&b, "%-20s%s\n", "", "rmd160  0 \\")
	} else {
		line("checksums", "rmd160  0 \\")
	}
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
	if s.Build.System == "cargo" || s.Build.System == "go" {
		b.WriteString("\n")
		s.writeDestroot(&b)
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
		for _, crate := range s.Unfetched {
			mark(crate)
		}
	}
	return []byte(b.String())
}

// writeDestroot installs the programs the build makes, as the tree's Cargo
// and Go ports do: neither PortGroup installs anything itself. Each is
// named ${name} where it's the port's own name.
func (s Spec) writeDestroot(b *strings.Builder) {
	if len(s.Binaries) == 0 {
		fmt.Fprintf(b, "%s %s\n", Unconfirmed, "the manifest names no program; install what the build makes in a destroot block")
		return
	}
	fmt.Fprintf(b, "%s %s\n", Unconfirmed, "installs the programs the manifest names; add what else the port should install")
	b.WriteString("destroot {\n")
	for _, binary := range s.Binaries {
		if binary == s.Name {
			binary = "${name}"
		}
		if s.Build.System == "cargo" {
			fmt.Fprintf(b, "    xinstall -m 0755 \\\n        ${worksrcpath}/target/[cargo.rust_platform]/release/%s \\\n        ${destroot}${prefix}/bin/\n", binary)
		} else {
			fmt.Fprintf(b, "    xinstall -m 0755 ${worksrcpath}/%s ${destroot}${prefix}/bin/\n", binary)
		}
	}
	b.WriteString("}\n")
}

// tclWord writes a description as MacPorts does, as plain words, quoted as
// one Tcl word only when a character would mean something to Tcl.
func tclWord(value string) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return "{}"
	}
	if strings.ContainsAny(value, "{}[]$\\\";") {
		return syntax.Quote(value)
	}
	return value
}

// SplitTag parses a release tag into the prefix before the version and the
// version: v0.4.2 is "v" and 0.4.2, rift-0.4.2 is "rift-" and 0.4.2.
func SplitTag(tag string) (prefix, version string, ok bool) {
	i := strings.IndexFunc(tag, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 || strings.ContainsAny(tag, " /") {
		return "", "", false
	}
	return tag[:i], tag[i:], true
}
