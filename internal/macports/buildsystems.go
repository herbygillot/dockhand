package macports

import (
	"path"
	"slices"
)

// BuildSystem is a way software is built, or the language whose files
// declare what it depends on: what a port uses, and what upstream's files
// belong to.
type BuildSystem string

const (
	CMake     BuildSystem = "cmake"
	Meson     BuildSystem = "meson"
	Autotools BuildSystem = "autotools"
	Go        BuildSystem = "go"
	Cargo     BuildSystem = "cargo"
	Python    BuildSystem = "python"
	Perl      BuildSystem = "perl"
	Ruby      BuildSystem = "ruby"
	Node      BuildSystem = "node"
	Java      BuildSystem = "java"
	R         BuildSystem = "r"
	Zig       BuildSystem = "zig"
	QMake     BuildSystem = "qmake"
	Waf       BuildSystem = "waf"
	Xcode     BuildSystem = "xcode"
	Haskell   BuildSystem = "haskell"
	Bazel     BuildSystem = "bazel"
	Dart      BuildSystem = "dart"
	// Swift and SCons are known by their files, Package.swift and
	// SConstruct; no PortGroup builds with them.
	Swift BuildSystem = "swift"
	SCons BuildSystem = "scons"
)

// portGroupSystems are the PortGroups that build a port, or bring a
// language's dependencies to it, and what each uses. A PortGroup that
// doesn't, github or legacysupport, names none.
var portGroupSystems = map[string]BuildSystem{
	"cmake": CMake, "meson": Meson, "golang": Go, "cargo": Cargo, "cargo_fetch": Cargo, "rust": Cargo, "rust_build": Cargo,
	"python": Python, "perl5": Perl, "ruby": Ruby, "npm": Node, "bun": Node, "java": Java, "maven": Java, "R": R,
	"zig_toolchain": Zig, "qmake": QMake, "qmake5": QMake, "qmake6": QMake, "waf": Waf, "xcode": Xcode,
	"haskell_cabal": Haskell, "haskell_stack": Haskell, "bazel": Bazel, "dart": Dart, "pub": Dart,
}

// BuildSystems are the build systems a port uses, as MacPorts evaluates
// it: those of the PortGroups it loads, and autotools where it runs a
// configure script, as Base does by default. It reports false where it can
// tell of none, as for a port that builds by its own commands, or one
// whose PortGroups weren't read: then nothing is known not to be used.
func (p PortInfo) BuildSystems() ([]BuildSystem, bool) {
	names, known := p.PortGroups()
	if !known {
		return nil, false
	}
	var systems []BuildSystem
	add := func(system BuildSystem) {
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
		add(Autotools)
	}
	return systems, len(systems) > 0
}
