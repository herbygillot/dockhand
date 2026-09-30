package project

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// System is a way software is built, or the language whose files declare
// what it depends on: what a project's files belong to, and what a port
// uses.
type System string

const (
	CMake     System = "cmake"
	Meson     System = "meson"
	Autotools System = "autotools"
	Go        System = "go"
	Cargo     System = "cargo"
	Python    System = "python"
	Perl      System = "perl"
	Ruby      System = "ruby"
	Node      System = "node"
	Java      System = "java"
	R         System = "r"
	Zig       System = "zig"
	QMake     System = "qmake"
	Waf       System = "waf"
	Xcode     System = "xcode"
	Haskell   System = "haskell"
	Bazel     System = "bazel"
	Dart      System = "dart"
	Swift     System = "swift"
	SCons     System = "scons"
)

// systems are the build system each build file and manifest belongs to,
// by its name.
var systems = map[string]System{
	"CMakeLists.txt": CMake, "configure.ac": Autotools, "configure.in": Autotools, "Makefile.am": Autotools,
	"meson.build": Meson, "meson_options.txt": Meson, "Makefile.PL": Perl, "cpanfile": Perl,
	"setup.py": Python, "setup.cfg": Python, "requirements.txt": Python, "pyproject.toml": Python,
	"build.gradle": Java, "pom.xml": Java, "SConstruct": SCons, "build.zig": Zig,
	"Package.swift": Swift, "Gemfile": Ruby, "DESCRIPTION": R,
	"go.mod": Go, "Cargo.toml": Cargo, CargoLock: Cargo, "package.json": Node,
}

// SystemOf is the build system a file belongs to, by its name; empty for
// one that belongs to none, such as a license file.
func SystemOf(name string) System {
	return systems[path.Base(name)]
}

// buildNames are the files that say how software builds.
var buildNames = []string{"CMakeLists.txt", "configure.ac", "configure.in", "meson.build", "meson_options.txt", "Makefile.am", "Makefile.PL",
	"setup.py", "setup.cfg", "build.gradle", "pom.xml", "SConstruct", "build.zig", "Package.swift", "Gemfile", "cpanfile", "DESCRIPTION"}

// BuildFile reports a file that says how software builds, by its name.
func BuildFile(name string) bool {
	return slices.Contains(buildNames, path.Base(name))
}

// Manifests are the files that declare a project's dependencies, by their
// names.
var Manifests = []string{"go.mod", "Cargo.toml", "package.json", "requirements.txt", "pyproject.toml"}

// Manifest reports a file that declares dependencies, by its name.
func Manifest(name string) bool {
	return slices.Contains(Manifests, path.Base(name))
}

// CargoLock is the file that pins a Rust project's crates.
const CargoLock = "Cargo.lock"

var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|copyright|notice|unlicense)([._-].*)?$`)

// sourceExtensions are a program's source, never a license file, whatever
// the file is named: usql's text/license.go is Go.
var sourceExtensions = []string{".c", ".cc", ".cpp", ".cs", ".go", ".h", ".java", ".js", ".kt", ".lua", ".m", ".php", ".pl", ".py", ".rb", ".rs", ".scala", ".sh", ".swift", ".tcl", ".ts"}

// LicenseFile reports a license file by its name: LICENSE, COPYING,
// NOTICE, and their kin, such as LICENSE-MIT or COPYING.LESSER, but not a
// program's source named for one.
func LicenseFile(name string) bool {
	base := path.Base(name)
	return licenseName.MatchString(base) && !slices.Contains(sourceExtensions, strings.ToLower(path.Ext(base)))
}
