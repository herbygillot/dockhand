package project

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
)

// Spec says where in a source its project is: Subdirectory, the directory
// below the archive's top that the project is in, as a port building a
// monorepo's Python bindings names bindings/python; empty for the top
// itself. It's the caller's to know, from how the port is built.
type Spec struct {
	Subdirectory string
	// Directories are those below the project's root that its build reads
	// build files from too, as a port's cmake.source_dir names one
	// (macports.PortInfo.BuildDirectories): i2pd builds from build/, and
	// its build/CMakeLists.txt wasn't read (field testing at c643559a).
	Directories []string
	// GoPackages are Go import paths whose package's files the reading
	// keeps, as a Go port's -X arguments name them
	// (macports.PortInfo.GoLinkPackages): each directory whose path is the
	// end of one, below the top, and the top itself for package main.
	GoPackages []string
}

// sourceFiles are the extensions of the source files a reading reads for
// host library paths: C, C++, Objective-C, Swift, Go, and Rust.
var sourceFiles = map[string]bool{".c": true, ".cc": true, ".cpp": true, ".cxx": true, ".h": true, ".hh": true, ".hpp": true,
	".m": true, ".mm": true, ".swift": true, ".go": true, ".rs": true}

// sourceFile reports a file a reading reads for host library paths: not a
// manifest or build file, as Package.swift, which a reading keeps as one.
func sourceFile(name string) bool {
	base := path.Base(name)
	return sourceFiles[path.Ext(name)] && !Manifest(base) && !BuildFile(base)
}

// hostPathLiteral is a string literal naming a path under a Homebrew
// prefix's library directories.
var hostPathLiteral = regexp.MustCompile(`"((?:/opt/homebrew/|/usr/local/lib/|/usr/local/opt/)[^"\s]*)"`)

// hostPathLiterals are the host library paths a source file's literals
// name, each once, sorted.
func hostPathLiterals(data []byte) []string {
	if !bytes.Contains(data, []byte("/opt/homebrew/")) && !bytes.Contains(data, []byte("/usr/local/")) {
		return nil
	}
	var found []string
	for _, match := range hostPathLiteral.FindAllSubmatch(data, -1) {
		if literal := string(match[1]); !slices.Contains(found, literal) {
			found = append(found, literal)
		}
	}
	slices.Sort(found)
	return found
}

// goPackageFile reports a Go source file, not a test's, in a directory one
// of packages ends with, or at the top where one is main.
func goPackageFile(rest string, packages []string) bool {
	if len(packages) == 0 || !strings.HasSuffix(rest, ".go") || strings.HasSuffix(rest, "_test.go") {
		return false
	}
	dir := path.Dir(rest)
	for _, imported := range packages {
		if dir == "." && imported == "main" || dir != "." && (imported == dir || strings.HasSuffix(imported, "/"+dir)) {
			return true
		}
	}
	return false
}

// subdirectory is the spec's subdirectory as a path below the top, however
// it was written: "/bindings/python/" is bindings/python.
func (s Spec) subdirectory() string {
	return strings.Trim(path.Clean("/"+s.Subdirectory), "/")
}

// directories are the spec's Directories as paths below root, cleaned,
// each once, leaving out root itself and any that would leave it.
func (s Spec) directories(root string) []string {
	var found []string
	for _, directory := range s.Directories {
		directory = strings.Trim(path.Clean("/"+directory), "/")
		if directory == "" {
			continue
		}
		if joined := path.Join(root, directory); !slices.Contains(found, joined) {
			found = append(found, joined)
		}
	}
	slices.Sort(found)
	return found
}

// wantedIn reports whether a reading keeps a path: at the top or the
// root (wanted), or a build file of one of the build's directories below
// it (buildFileIn).
func wantedIn(rest, root string, directories []string) bool {
	return wanted(rest, "") || root != "" && wanted(rest, root) || buildFileIn(rest, directories)
}

// buildFileIn reports a build file or manifest at the top of one of the
// directories: what the build reads there, not its licenses or the rest.
func buildFileIn(rest string, directories []string) bool {
	for _, directory := range directories {
		if below, ok := strings.CutPrefix(rest, directory+"/"); ok && !strings.Contains(below, "/") && (BuildFile(below) || Manifest(below) || below == CargoLock) {
			return true
		}
	}
	return false
}

// Layout is how an archive holds its files.
type Layout string

const (
	// Enclosed is one top directory holding everything, as a release
	// archive's, named for its version, does.
	Enclosed Layout = "enclosed"
	// Flat is files at the archive's root.
	Flat Layout = "flat"
	// Ambiguous is several top directories and no file beside them: which
	// holds the project isn't known, so nothing is read.
	Ambiguous Layout = "ambiguous"
)

// Reading is what a project's files were found to be, and where.
type Reading struct {
	Layout Layout
	// Top is the directory an enclosed archive holds its files in; empty
	// for a flat one.
	Top string
	// Tops are an ambiguous archive's top directories, in name order.
	Tops []string
	// Root is the project's directory below Top, the Spec's subdirectory
	// where the archive has it; empty for Top itself.
	Root string
	// Missing is a subdirectory the Spec named that the archive doesn't
	// have, so the project was read at its top: an archive the port
	// fetches beside the one it builds in, perhaps.
	Missing string
	// HostPaths are the library paths each source file names as string
	// literals under /opt/homebrew/, /usr/local/lib/, or /usr/local/opt/,
	// a Homebrew prefix's, by its path below Top, sorted: what a program
	// may dlopen at run time outside ${prefix}, as hashcat 8.0's
	// src/ext_zlib.c names /opt/homebrew/lib/libz.1.dylib.
	HostPaths map[string][]string
	// Files are what was read: the license files at the top and at the
	// root, each there or one directory down, the root's build files,
	// manifests, and Cargo.lock, and the package.json of each Node
	// workspace the root's names, each by its path below Top.
	Files map[string]File
	// Directories are the archive's directories below Top, to
	// DirectoryDepth deep, sorted: a Portfile's build names some, as
	// semgrep's ${worksrcpath}/pfff, which a new version may not have.
	Directories []string `json:",omitempty"`
	// LibraryVersions are the libtool version each Makefile.am anywhere
	// in the archive gives the libraries it builds, -version-info's
	// current:revision:age, by the file's path below Top: a new one names
	// a new dylib, whose dependents need rebuilding. libunibreak 8.0 moved
	// src/Makefile.am's from 7:0:0 to 8:0:0, and only the root's
	// Makefile.am was read (field testing, batch 12).
	LibraryVersions map[string]string `json:",omitempty"`
	// Programs are the directories below Top whose Cargo package builds
	// a program by Cargo's conventions, having src/main.rs or a .rs file
	// in src/bin, "." for Top itself: one with no [[bin]] builds a
	// program only so, and a library otherwise, which mise's new
	// mise-dotenv was, and was held as a binary added (field testing,
	// batch 13).
	Programs []string `json:",omitempty"`
}

// DirectoryDepth is how deep below Top a reading lists directories.
const DirectoryDepth = 3

// HasDirectory reports whether the reading's archive has a directory at
// the path below Top; known is false where the path is deeper than a
// reading lists, or the reading listed none, as one made before it did.
func (r Reading) HasDirectory(dir string) (has, known bool) {
	dir = strings.Trim(path.Clean(dir), "/")
	if len(r.Directories) == 0 || strings.Count(dir, "/") >= DirectoryDepth {
		return false, false
	}
	_, found := slices.BinarySearch(r.Directories, dir)
	return found, true
}

// CargoProgram reports whether the Cargo package in a directory below
// Top builds a program by Cargo's conventions (Programs). None found is
// none: a reading made before Programs were kept counted every package a
// program, but ReaderVersion 7 serves none of those, and a source whose
// programs are all [[bin]]s with paths of their own has no src/main.rs,
// where that read its libraries as programs (Codex's review, checked
// against a268b6e5).
func (r Reading) CargoProgram(dir string) bool {
	if dir == "" {
		dir = "."
	}
	return slices.Contains(r.Programs, dir)
}

// cargoProgram is the directory of the Cargo package a file makes a
// program of: its src/main.rs, or a .rs file directly in its src/bin.
func cargoProgram(name string) (string, bool) {
	dir, file := path.Split(name)
	dir = strings.TrimSuffix(dir, "/")
	var src string
	switch {
	case file == "main.rs" && path.Base(dir) == "src":
		src = dir
	case strings.HasSuffix(file, ".rs") && path.Base(dir) == "bin" && path.Base(path.Dir(dir)) == "src":
		src = path.Dir(dir)
	default:
		return "", false
	}
	return path.Dir(src), true
}

// File is a file as it was read: its first FileLimit bytes, and whether
// there was more.
type File struct {
	Data      []byte
	Truncated bool
}

// FileLimit is the most of one file a reading keeps.
const FileLimit = 1 << 20

// ignorable are what an archive may hold beside its project that says
// nothing of how it's laid out: macOS's resource forks, as ._name files
// and a zip's __MACOSX directory.
func ignorable(name string) bool {
	first, _, _ := strings.Cut(name, "/")
	return first == "__MACOSX" || strings.HasPrefix(path.Base(name), "._")
}

// wanted reports a file a reading keeps, by its path below the top and the
// project's root there: a license file near either, and the root's own
// build files and manifests.
func wanted(rest, root string) bool {
	near := func(below string) bool {
		return LicenseFile(below) && strings.Count(below, "/") <= 1
	}
	if near(rest) {
		return true
	}
	if root != "" {
		below, ok := strings.CutPrefix(rest, root+"/")
		if !ok {
			return false
		}
		if near(below) {
			return true
		}
		rest = below
	}
	return !strings.Contains(rest, "/") && (BuildFile(rest) || Manifest(rest) || rest == CargoLock)
}

// ReadFile reads a file that isn't an archive, as a distfile MacPorts
// doesn't extract is, as the one file it is, by name: a flat reading that
// keeps it where a reading of an archive would, as a license or build
// file, and keeps nothing otherwise, as for bashunit's script (field
// testing at 285a496f, where reading it as a tar was refused and held
// the update).
func ReadFile(filename, name string) (Reading, error) {
	reading := Reading{Layout: Flat, Files: map[string]File{}}
	if !wanted(name, "") {
		return reading, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return Reading{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, FileLimit+1))
	if err != nil {
		return Reading{}, err
	}
	reading.Files[name] = File{Data: data[:min(len(data), FileLimit)], Truncated: len(data) > FileLimit}
	return reading, nil
}

// Read reads a project from an archive: which layout it has, where its
// project is, and the files that say what the project is. An archive
// that can't be read is an error; one whose project can't be found is
// Ambiguous, which the caller says.
func Read(ctx context.Context, filename string, spec Spec) (Reading, error) {
	subdirectory := spec.subdirectory()
	buildDirectories := spec.directories(subdirectory)
	// Each file any layout could keep, by its whole path, and what's
	// learned of the layout on the way.
	candidates := map[string]File{}
	// manifests are every Cargo.toml, which a workspace's root may name as
	// a member: kept in this pass, since a second walk of rust's source,
	// gigabytes, is minutes.
	manifests := map[string]File{}
	// modules are every .cmake file, which a CMakeLists.txt may include():
	// kept in this pass too.
	modules := map[string]File{}
	tops := map[string]bool{}
	libraries := map[string]string{}
	directories := map[string]bool{}
	programs := map[string]bool{}
	// hostPaths are each source file's host library paths, by its path.
	hostPaths := map[string][]string{}
	flat := false
	has := map[string]bool{}
	err := archive.Walk(ctx, filename, func(member archive.Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular || ignorable(name) {
			return nil
		}
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			directories[dir] = true
		}
		if program, ok := cargoProgram(name); ok {
			programs[program] = true
		}
		first, rest, nested := strings.Cut(name, "/")
		if !nested {
			flat = true
		} else {
			tops[first] = true
			if subdirectory != "" && strings.HasPrefix(rest, subdirectory+"/") {
				has[first] = true
			}
		}
		if subdirectory != "" && strings.HasPrefix(name, subdirectory+"/") {
			has[""] = true
		}
		// A source file is read for the library paths it names outside
		// any prefix, a Homebrew one's (HostPaths), and kept where it's a
		// named Go package's.
		if sourceFile(name) {
			data, err := io.ReadAll(io.LimitReader(member.Body, FileLimit+1))
			if err != nil {
				return err
			}
			if found := hostPathLiterals(data); len(found) > 0 {
				hostPaths[name] = found
			}
			if goPackageFile(name, spec.GoPackages) || nested && goPackageFile(rest, spec.GoPackages) {
				candidates[name] = File{Data: data[:min(len(data), FileLimit)], Truncated: len(data) > FileLimit}
			}
			return nil
		}
		keep := candidates
		if path.Base(name) == "Makefile.am" {
			data, err := io.ReadAll(io.LimitReader(member.Body, FileLimit))
			if err != nil {
				return err
			}
			if versions := libtoolVersions(data); versions != "" {
				libraries[name] = versions
			}
			// What a reading keeps of the root's own is read from these
			// bytes, the member's body being read once.
			if wantedIn(name, subdirectory, buildDirectories) || nested && wantedIn(rest, subdirectory, buildDirectories) {
				keep[name] = File{Data: data, Truncated: len(data) >= FileLimit}
			}
			return nil
		}
		goFile := goPackageFile(name, spec.GoPackages) || nested && goPackageFile(rest, spec.GoPackages)
		if !goFile && !wantedIn(name, subdirectory, buildDirectories) && (!nested || !wantedIn(rest, subdirectory, buildDirectories)) {
			switch {
			case path.Base(name) == "Cargo.toml":
				keep = manifests
			case strings.HasSuffix(name, ".cmake"):
				keep = modules
			default:
				return nil
			}
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, FileLimit+1))
		if err != nil {
			return err
		}
		if len(data) > FileLimit {
			keep[name] = File{Data: data[:FileLimit], Truncated: true}
			return nil
		}
		keep[name] = File{Data: data}
		return nil
	})
	if err != nil {
		return Reading{}, err
	}
	var found Reading
	switch {
	case flat || len(tops) == 0:
		found.Layout = Flat
	case len(tops) == 1:
		found.Layout = Enclosed
		for top := range tops {
			found.Top = top
		}
	default:
		for top := range tops {
			found.Tops = append(found.Tops, top)
		}
		slices.Sort(found.Tops)
		return Reading{Layout: Ambiguous, Tops: found.Tops, Files: map[string]File{}}, nil
	}
	if subdirectory != "" {
		if has[found.Top] {
			found.Root = subdirectory
		} else {
			found.Missing = subdirectory
		}
	}
	for dir := range directories {
		rest := dir
		if found.Top != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(dir, found.Top+"/"); !ok {
				continue
			}
		}
		if strings.Count(rest, "/") < DirectoryDepth {
			found.Directories = append(found.Directories, rest)
		}
	}
	slices.Sort(found.Directories)
	for dir := range programs {
		rest := dir
		if found.Top != "" {
			switch {
			case dir == found.Top:
				rest = "."
			case strings.HasPrefix(dir, found.Top+"/"):
				rest = strings.TrimPrefix(dir, found.Top+"/")
			default:
				continue
			}
		}
		found.Programs = append(found.Programs, rest)
	}
	slices.Sort(found.Programs)
	for name, paths := range hostPaths {
		rest := name
		if found.Top != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(name, found.Top+"/"); !ok {
				continue
			}
		}
		if found.HostPaths == nil {
			found.HostPaths = map[string][]string{}
		}
		found.HostPaths[rest] = paths
	}
	found.Files = map[string]File{}
	for name, file := range candidates {
		rest := name
		if found.Top != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(name, found.Top+"/"); !ok {
				continue
			}
		}
		below := strings.TrimPrefix(rest, found.Root+"/")
		if wanted(rest, found.Root) || buildFileIn(rest, spec.directories(found.Root)) || (found.Root == "" || below != rest) && goPackageFile(below, spec.GoPackages) {
			found.Files[rest] = file
		}
	}
	for name, versions := range libraries {
		rest := name
		if found.Top != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(name, found.Top+"/"); !ok {
				continue
			}
		}
		if found.LibraryVersions == nil {
			found.LibraryVersions = map[string]string{}
		}
		found.LibraryVersions[rest] = versions
	}
	found.cargoMembers(manifests)
	found.cmakeIncludes(modules)
	return found, found.readWorkspaces(ctx, filename)
}

// cmakeIncludes keeps each file a CMakeLists.txt the reading has
// include()s, as CMakeDocument finds it, from modules, every .cmake file
// the archive holds by its whole path: fluent-bit keeps its plugins'
// options in cmake/plugins_options.cmake, which reading the root alone
// didn't see (the dogfood run with 58e2d7eb).
func (r *Reading) cmakeIncludes(modules map[string]File) {
	below := map[string]File{}
	for name, file := range modules {
		rest, ok := name, true
		if r.Top != "" {
			rest, ok = strings.CutPrefix(name, r.Top+"/")
		}
		if ok {
			below[rest] = file
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r.Files)) {
		if path.Base(name) != "CMakeLists.txt" {
			continue
		}
		document := parseCMake(string(r.Files[name].Data), path.Dir(name), func(include string) (string, bool) {
			file, ok := below[include]
			return string(file.Data), ok && !file.Truncated
		})
		for _, include := range document.Included() {
			r.Files[include] = below[include]
		}
	}
}

// cargoMembers keeps the Cargo.toml of each member a Cargo workspace's
// root names, by its members globs less what it excludes (The Cargo
// Book, "Workspaces"), from manifests, every one the archive holds by its
// whole path.
func (r *Reading) cargoMembers(manifests map[string]File) {
	root, ok := r.RootFile("Cargo.toml")
	if !ok || root.Truncated {
		return
	}
	manifest, err := ReadCargoManifest(root.Data)
	if err != nil || manifest.Workspace == nil {
		return
	}
	for name, file := range manifests {
		rest := name
		if r.Top != "" {
			if rest, ok = strings.CutPrefix(name, r.Top+"/"); !ok {
				continue
			}
		}
		below := rest
		if r.Root != "" {
			if below, ok = strings.CutPrefix(rest, r.Root+"/"); !ok {
				continue
			}
		}
		if directory := path.Dir(below); directory != "." && manifest.Workspace.Member(directory) {
			r.Files[rest] = file
		}
	}
}

// RootFile is a file at the project's root, by its name, as read.
func (r Reading) RootFile(name string) (File, bool) {
	file, ok := r.Files[path.Join(r.Root, name)]
	return file, ok
}

// DeclaredLicense is the license the project's own manifest at its root
// declares, as an SPDX expression, and the manifest's path below Top:
// Cargo.toml's [package], pyproject.toml's [project], or package.json's,
// the first of them that declares one as a string; false where none does.
// zola declares EUPL-1.2 in Cargo.toml, which its license files alone
// don't say (the zola run with 68df8b57). A Cargo package's license
// inherited from its workspace is the workspace's, and a virtual
// workspace's is what its members inherit. One that's an SPDX expression is
// given as its specification normalizes it (LicenseExpression), so "mit"
// and "MIT" are one license; one that isn't is given as written.
func (r Reading) DeclaredLicense() (license, file string, ok bool) {
	for _, name := range []string{"Cargo.toml", "pyproject.toml", "package.json"} {
		found, ok := r.RootFile(name)
		if !ok || found.Truncated {
			continue
		}
		switch name {
		case "Cargo.toml":
			if manifest, err := ReadCargoManifest(found.Data); err == nil && manifest.Package != nil {
				license = manifest.Package.License
			} else if err == nil {
				license = r.inheritedLicense(manifest)
			}
		case "pyproject.toml":
			if manifest, err := ReadPyproject(found.Data); err == nil && manifest.Project != nil {
				license = manifest.Project.License
			}
		default:
			if manifest, err := ReadPackageJSON(found.Data); err == nil {
				license = manifest.License
			}
		}
		if license != "" {
			if normalized, ok := LicenseExpression(license); ok {
				license = normalized
			}
			return license, path.Join(r.Root, name), true
		}
	}
	return "", "", false
}

// inheritedLicense is the license a virtual Cargo workspace's members
// inherit from its [workspace.package], where a member read does: uv's
// root declares no package, and its crates license = { workspace = true }.
// Empty where none inherits it, since a workspace's field is no package's
// until one takes it.
func (r Reading) inheritedLicense(root CargoManifest) string {
	if root.Workspace == nil || root.Workspace.Package.License == "" {
		return ""
	}
	for name, file := range r.Files {
		directory, ok := strings.CutPrefix(path.Dir(name), r.Root)
		if path.Base(name) != "Cargo.toml" || !ok || file.Truncated || !root.Workspace.Member(strings.TrimPrefix(directory, "/")) {
			continue
		}
		if member, err := ReadCargoManifest(file.Data); err == nil && member.Package != nil && slices.Contains(member.Package.Inherited, "license") {
			return root.Workspace.Package.License
		}
	}
	return ""
}

// PythonBackend is the PEP 517 backend the project's pyproject.toml at its
// root builds with, as it names it; false where it names none, or there's
// none that could be read.
func (r Reading) PythonBackend() (string, bool) {
	found, ok := r.RootFile("pyproject.toml")
	if !ok || found.Truncated {
		return "", false
	}
	manifest, err := ReadPyproject(found.Data)
	if err != nil || manifest.BuildBackend == "" {
		return "", false
	}
	return manifest.BuildBackend, true
}

// readWorkspaces reads the manifest of each workspace member the root's
// manifest names, as the build reads them with it: each package.json a
// Node root's workspaces name, as yarn and npm install them, since
// beekeeper-studio moved electron in apps/studio/package.json, which
// reading the root alone didn't see (the beekeeper-studio run's finding
// 1). They're read in a second pass, only where the root names
// workspaces, since the root's may come after theirs in the archive; a
// node_modules directory is never one. A Cargo workspace's are kept from
// the first (cargoMembers).
func (r *Reading) readWorkspaces(ctx context.Context, filename string) error {
	var node []string
	if root, ok := r.RootFile("package.json"); ok && !root.Truncated {
		if manifest, err := ReadPackageJSON(root.Data); err == nil {
			node = manifest.Workspaces
		}
	}
	if len(node) == 0 {
		return nil
	}
	return archive.Walk(ctx, filename, func(member archive.Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular || ignorable(name) {
			return nil
		}
		rest, ok := name, true
		if r.Top != "" {
			rest, ok = strings.CutPrefix(name, r.Top+"/")
		}
		below := rest
		if ok && r.Root != "" {
			below, ok = strings.CutPrefix(rest, r.Root+"/")
		}
		directory := path.Dir(below)
		if !ok || directory == "." || path.Base(below) != "package.json" || slices.Contains(strings.Split(directory, "/"), "node_modules") || !workspace(node, directory) {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, FileLimit+1))
		if err != nil {
			return err
		}
		if len(data) > FileLimit {
			r.Files[rest] = File{Data: data[:FileLimit], Truncated: true}
			return nil
		}
		r.Files[rest] = File{Data: data}
		return nil
	})
}

// workspace reports whether a directory below a Node project's root is one
// of its workspaces, as the root's package.json names them: glob patterns
// of its path, "**" standing for any number of directories, and one
// starting "!" leaving out what it matches, the last that matches saying.
func workspace(patterns []string, directory string) bool {
	in := false
	for _, pattern := range patterns {
		pattern, exclude := strings.CutPrefix(pattern, "!")
		pattern = strings.Trim(path.Clean("/"+pattern), "/")
		if pattern != "" && glob(strings.Split(pattern, "/"), strings.Split(directory, "/")) {
			in = !exclude
		}
	}
	return in
}

// glob matches a path's segments against a pattern's, each as path.Match
// matches one, and "**" any number of them.
func glob(pattern, segments []string) bool {
	switch {
	case len(pattern) == 0:
		return len(segments) == 0
	case pattern[0] == "**":
		for i := range len(segments) + 1 {
			if glob(pattern[1:], segments[i:]) {
				return true
			}
		}
		return false
	case len(segments) == 0:
		return false
	}
	matched, err := path.Match(pattern[0], segments[0])
	return err == nil && matched && glob(pattern[1:], segments[1:])
}

// libtoolVersion is a -version-info flag's value, current:revision:age.
var libtoolVersion = regexp.MustCompile(`-version-info\s+([0-9]+:[0-9]+:[0-9]+)`)

// libtoolVersions are the -version-info values a Makefile.am gives,
// joined in order, or empty for one that gives none.
func libtoolVersions(data []byte) string {
	var found []string
	for _, match := range libtoolVersion.FindAllSubmatch(data, -1) {
		found = append(found, string(match[1]))
	}
	return strings.Join(found, " ")
}
