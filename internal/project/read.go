package project

import (
	"context"
	"io"
	"maps"
	"path"
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
}

// subdirectory is the spec's subdirectory as a path below the top, however
// it was written: "/bindings/python/" is bindings/python.
func (s Spec) subdirectory() string {
	return strings.Trim(path.Clean("/"+s.Subdirectory), "/")
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
	// Files are what was read: the license files at the top and at the
	// root, each there or one directory down, the root's build files,
	// manifests, and Cargo.lock, and the package.json of each Node
	// workspace the root's names, each by its path below Top.
	Files map[string]File
	// Directories are the archive's directories below Top, to
	// DirectoryDepth deep, sorted: a Portfile's build names some, as
	// semgrep's ${worksrcpath}/pfff, which a new version may not have.
	Directories []string `json:",omitempty"`
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

// Read reads a project from an archive: which layout it has, where its
// project is, and the files that say what the project is. An archive
// that can't be read is an error; one whose project can't be found is
// Ambiguous, which the caller says.
func Read(ctx context.Context, filename string, spec Spec) (Reading, error) {
	subdirectory := spec.subdirectory()
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
	directories := map[string]bool{}
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
		keep := candidates
		if !wanted(name, subdirectory) && !wanted(name, "") && (!nested || !wanted(rest, subdirectory) && !wanted(rest, "")) {
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
	found.Files = map[string]File{}
	for name, file := range candidates {
		rest := name
		if found.Top != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(name, found.Top+"/"); !ok {
				continue
			}
		}
		if wanted(rest, found.Root) {
			found.Files[rest] = file
		}
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
