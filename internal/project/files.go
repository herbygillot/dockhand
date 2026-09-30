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

// versionDeclarations are how a build file declares its project's own
// version, by the file's name, each a pattern with the version in place of
// %s and ending it where the syntax does: CMake's project(… VERSION v), or VERSION v on a line of its own
// inside a project() spread over lines; Meson's version: 'v' keyword;
// Autoconf's AC_INIT; setuptools' version; an R package's DESCRIPTION;
// Gradle's version; and ExtUtils::MakeMaker's VERSION. A file that
// declares none this way, such as Package.swift, declares none it can
// recognize.
var versionDeclarations = map[string][]string{
	"CMakeLists.txt": {`(?i)^\s*project\s*\(.*\bVERSION\s+"?%s("|\s|\)|$)`, `(?i)^\s*VERSION\s+"?%s("|\s|\)|$)`},
	"meson.build":    {`\bversion\s*:\s*['"]%s['"]`},
	"configure.ac":   {`^\s*AC_INIT\s*\(.*\[?%s(\]|,|\)|\s)`},
	"configure.in":   {`^\s*AC_INIT\s*\(.*\[?%s(\]|,|\)|\s)`},
	"setup.py":       {`\bversion\s*=\s*['"]%s['"]`},
	"setup.cfg":      {`^\s*version\s*=\s*%s\s*$`, `^\s*current_version\s*=\s*%s\s*$`},
	"DESCRIPTION":    {`^Version:\s*%s\s*$`},
	"build.gradle":   {`^\s*version\s*=?\s*['"]%s['"]`},
	"Makefile.PL":    {`\bVERSION\s*=>\s*['"]%s['"]`},
}

// setupFreeBackends are PEP 517 backends that read neither setup.py nor
// setup.cfg: a project building with one keeps them for other tools, if at
// all, as sshuttle, built with hatchling, keeps bumpversion's version in
// setup.cfg (the sshuttle run with f075232d). Any other backend,
// setuptools', pbr's, or a project's own in its tree, may read them.
var setupFreeBackends = []string{"hatchling.build", "flit_core.buildapi", "poetry.core.masonry.api", "pdm.backend", "maturin", "mesonpy", "scikit_build_core.build", "uv_build"}

// BackendReads reports whether a Python project that builds with a PEP 517
// backend reads a build file: setup.py and setup.cfg are setuptools', and
// a backend known not to read them doesn't; every other file, as far as
// this says, is read.
func BackendReads(backend, name string) bool {
	switch path.Base(name) {
	case "setup.py", "setup.cfg":
		return !slices.Contains(setupFreeBackends, backend)
	}
	return true
}

// DeclaresVersion reports whether a line of a build file declares the
// project's own version as the given one, as its build system declares
// it, and not some other version that happens to be the same, as
// find_package(SomeLibrary 1.0) names a dependency's (the update-workflow
// review's finding 2).
func DeclaresVersion(name, line, version string) bool {
	if version == "" {
		return false
	}
	// Each pattern ends the version where its syntax does, so 1.0 isn't
	// found in 1.0.1.
	for _, pattern := range versionDeclarations[path.Base(name)] {
		if regexp.MustCompile(strings.Replace(pattern, "%s", regexp.QuoteMeta(version), 1)).MatchString(line) {
			return true
		}
	}
	return false
}
