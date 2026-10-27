package macports

import (
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// portGroupSystems are the PortGroups that build a port, or bring a
// language's dependencies to it, and what each uses. A PortGroup that
// doesn't, github or legacysupport, names none.
// No PortGroup builds with Swift or SCons, which are known only by their
// files.
var portGroupSystems = map[string]project.System{
	"cmake": project.CMake, "meson": project.Meson, "golang": project.Go, "cargo": project.Cargo, "cargo_fetch": project.Cargo,
	"rust": project.Cargo, "rust_build": project.Cargo, "python": project.Python, "perl5": project.Perl, "ruby": project.Ruby,
	"npm": project.Node, "bun": project.Node, "java": project.Java, "maven": project.Java, "R": project.R,
	"zig_toolchain": project.Zig, "qmake": project.QMake, "qmake5": project.QMake, "qmake6": project.QMake, "waf": project.Waf,
	"xcode": project.Xcode, "haskell_cabal": project.Haskell, "haskell_stack": project.Haskell, "bazel": project.Bazel,
	"dart": project.Dart, "pub": project.Dart,
}

// BuildSystems are the build systems a port uses, as MacPorts evaluates
// it: those of the PortGroups it loads, and autotools where it runs a
// configure script, as Base does by default. It reports false where it can
// tell of none, as for a port that builds by its own commands, or one
// whose PortGroups weren't read: then nothing is known not to be used.
func (p PortInfo) BuildSystems() ([]project.System, bool) {
	names, known := p.PortGroups()
	if !known {
		return nil, false
	}
	var systems []project.System
	add := func(system project.System) {
		if !slices.Contains(systems, system) {
			systems = append(systems, system)
		}
	}
	for _, name := range names {
		if system, ok := portGroupSystems[name]; ok {
			add(system)
		}
	}
	if configure, err := p.Bool("use_configure"); err == nil && configure && path.Base(p.Options["configure.cmd"]) == "configure" {
		add(project.Autotools)
	}
	return systems, len(systems) > 0
}

// WorksrcPlaceholder is how the evaluator writes ${worksrcpath} at the
// start of a directory option it reads against it, as cargo.dir and
// patch.dir: "@worksrc@/sub".
const WorksrcPlaceholder = "@worksrc@"

// buildDirectoryOptions are the options naming the directory each build
// system reads its build files from: CMake's source directory, the
// configure script's for Autotools and Meson, whose source directory it
// is, and the build's for Cargo, Go, Python, and Perl.
var buildDirectoryOptions = map[project.System]string{
	project.CMake: "cmake.source_dir", project.Autotools: "configure.dir", project.Meson: "configure.dir",
	project.Cargo: "cargo.dir", project.Go: "build.dir", project.Python: "build.dir", project.Perl: "build.dir",
}

// BuildDirectories are the directories below worksrcpath that the port's
// build systems (BuildSystems) read their build files from, as the
// evaluator writes them, "@worksrc@/build" for i2pd's cmake.source_dir:
// "build", each once. None for a build at worksrcpath, or one outside it.
// i2pd's build/CMakeLists.txt went unread, and its update said no build
// file had changed (field testing at c643559a).
func (p PortInfo) BuildDirectories() []string {
	systems, _ := p.BuildSystems()
	var directories []string
	for _, system := range systems {
		option, ok := buildDirectoryOptions[system]
		if !ok {
			continue
		}
		below, ok := strings.CutPrefix(p.Options[option], WorksrcPlaceholder+"/")
		if below = strings.Trim(path.Clean("/"+below), "/"); ok && below != "" && !slices.Contains(directories, below) {
			directories = append(directories, below)
		}
	}
	slices.Sort(directories)
	return directories
}

// ReadingSpec is where a reading of the port's source looks: below its
// worksrcdir's subdirectory (SourceSubdirectory), in the directories its
// build reads build files from (BuildDirectories), and at the Go packages
// its -X arguments set variables of (GoLinkPackages).
func (p PortInfo) ReadingSpec() project.Spec {
	return project.Spec{Subdirectory: SourceSubdirectory(p.Options["worksrcdir"]), Directories: p.BuildDirectories(), GoPackages: p.GoLinkPackages()}
}

// GOPATHLayout reports whether worksrcdir is the Go PortGroup's default,
// gopath/src/<go.package>, a post-extract location rather than an archive
// path.
func GOPATHLayout(worksrcdir string) bool {
	return strings.HasPrefix(strings.Trim(worksrcdir, "/"), "gopath/src/")
}

// SourceSubdirectory is the directory below a distfile's top that a port
// builds in: its worksrcdir, less the first element, which MacPorts names
// for the directory the distfile extracts to, as a port building a
// monorepo's Python bindings sets worksrcdir ${distname}/bindings/python.
// Empty where it builds at the top, or where worksrcdir is the Go
// PortGroup's GOPATH location, which post-extract makes from the top.
func SourceSubdirectory(worksrcdir string) string {
	if GOPATHLayout(worksrcdir) {
		return ""
	}
	_, subdirectory, _ := strings.Cut(strings.Trim(worksrcdir, "/"), "/")
	return subdirectory
}
