package project

import (
	"context"
	"io"
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
	tops := map[string]bool{}
	flat := false
	has := map[string]bool{}
	err := archive.Walk(ctx, filename, func(member archive.Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular || ignorable(name) {
			return nil
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
		if !wanted(name, subdirectory) && !wanted(name, "") && (!nested || !wanted(rest, subdirectory) && !wanted(rest, "")) {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, FileLimit+1))
		if err != nil {
			return err
		}
		if len(data) > FileLimit {
			candidates[name] = File{Data: data[:FileLimit], Truncated: true}
			return nil
		}
		candidates[name] = File{Data: data}
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
	return found, found.readWorkspaces(ctx, filename)
}

// RootFile is a file at the project's root, by its name, as read.
func (r Reading) RootFile(name string) (File, bool) {
	file, ok := r.Files[path.Join(r.Root, name)]
	return file, ok
}

// readWorkspaces reads the package.json of each workspace the root's
// package.json names, as yarn and npm install them with it: beekeeper-studio
// moved electron in apps/studio/package.json, which reading the root alone
// didn't see (the beekeeper-studio run's finding 1). They're read in a
// second pass, only where the root names workspaces, since the root's may
// come after theirs in the archive; a node_modules directory is never one.
func (r *Reading) readWorkspaces(ctx context.Context, filename string) error {
	root, ok := r.RootFile("package.json")
	if !ok || root.Truncated {
		return nil
	}
	manifest, err := ReadPackageJSON(root.Data)
	if err != nil || len(manifest.Workspaces) == 0 {
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
		if !ok || path.Base(below) != "package.json" {
			return nil
		}
		directory := path.Dir(below)
		if directory == "." || slices.Contains(strings.Split(directory, "/"), "node_modules") || !workspace(manifest.Workspaces, directory) {
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
