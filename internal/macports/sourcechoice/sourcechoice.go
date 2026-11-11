// Package sourcechoice chooses which of a forge release's files is the
// project's source, from what each holds: the candidates by name, what
// the repository's tag and the Portfile ask of one, and the one that
// holds it, or why none does. It is pure: what it reads, the release's
// descriptors and each archive's inventory (project.Inventory), is handed
// to it, and downloading, the edit, and its words are portedit's.
package sourcechoice

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// platformWords are the words a binary bundle's name carries, which a
// source archive's doesn't.
var platformWords = []string{"darwin", "macos", "osx", "linux", "windows", "win", "x86_64", "amd64", "arm64", "aarch64", "universal", "musl"}

// Candidates are the release's assets that may be its source, by name: a
// suffix MacPorts extracts and no platform or architecture word,
// ${project}-${version}.tar.gz first, the github PortGroup's default.
// That a candidate is the source is known by its content alone (Choose).
func Candidates(assets []model.ReleaseAsset, project, version string) []model.ReleaseAsset {
	var candidates []model.ReleaseAsset
	for _, asset := range assets {
		extraction, ok := macports.ExtractionOf(asset.Name)
		if !ok || binaryName(strings.TrimSuffix(asset.Name, extraction.Suffix)) {
			continue
		}
		candidates = append(candidates, asset)
	}
	preferred := project + "-" + version + ".tar.gz"
	if i := slices.IndexFunc(candidates, func(asset model.ReleaseAsset) bool { return asset.Name == preferred }); i > 0 {
		first := candidates[i]
		candidates = append([]model.ReleaseAsset{first}, slices.Delete(candidates, i, i+1)...)
	}
	return candidates
}

// binaryName reports a file's base that names a platform or architecture:
// tool-1.0-darwin-arm64, tool_win64.
func binaryName(base string) bool {
	lower := strings.ToLower(base)
	if strings.Contains(lower, "x86_64") || strings.Contains(lower, "x86-64") {
		return true
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' }) {
		for _, word := range platformWords {
			if rest, ok := strings.CutPrefix(token, word); ok && strings.Trim(rest, "0123456789") == "" {
				return true
			}
		}
	}
	return false
}

// Needs are what a release asset must hold to be the source: the root
// build and manifest files the repository has at the tag, the paths the
// Portfile names under ${worksrcpath} that the repository or a submodule
// has there, and, for a Git port, every submodule as a non-empty
// directory.
type Needs struct {
	Files      []string
	Submodules []string
}

// NeedsOf are what the tag's tree and the paths the Portfile names under
// ${worksrcpath} (portfile.WorksrcPaths) ask of an asset. A path the
// Portfile names that neither the tag nor a submodule has is the build's
// own, such as a build directory, and asked of no asset.
func NeedsOf(tag project.Inventory, worksrcPaths []string, git bool) Needs {
	var needs Needs
	if git {
		needs.Submodules = tag.Submodules()
	}
	for name := range tag.Files {
		if !strings.Contains(name, "/") && (project.BuildFile(name) || project.Manifest(name)) {
			needs.Files = append(needs.Files, name)
		}
	}
	for _, named := range worksrcPaths {
		inSubmodule := slices.ContainsFunc(needs.Submodules, func(sub string) bool { return named == sub || strings.HasPrefix(named, sub+"/") })
		if (tag.Has(named) || inSubmodule) && !slices.Contains(needs.Files, named) {
			needs.Files = append(needs.Files, named)
		}
	}
	slices.Sort(needs.Files)
	return needs
}

// Lacks is the first thing an asset's inventory doesn't hold of what's
// needed, in words: "extracts to more than one directory", or a path;
// empty where it holds everything.
func (n Needs) Lacks(inventory project.Inventory) string {
	if inventory.Top == "" {
		return "extracts to more than one directory"
	}
	for _, sub := range n.Submodules {
		if inventory.Dirs[sub] == 0 {
			return "lacks " + sub
		}
	}
	for _, name := range n.Files {
		if !inventory.Has(name) {
			return "lacks " + name
		}
	}
	return ""
}

// Judged is one candidate as read: its descriptor, its inventory, and
// what it lacks (Needs.Lacks), or why it couldn't be read; Lacks empty
// for one that holds the source.
type Judged struct {
	Asset     model.ReleaseAsset
	Inventory project.Inventory
	Lacks     string
}

// Ambiguous is several candidates that each hold the source with
// different files or contents, named in their order: none is chosen.
type Ambiguous struct{ Names []string }

func (a Ambiguous) Error() string {
	return fmt.Sprintf("%s each hold the source, with different contents", strings.Join(a.Names, ", "))
}

// NoneHolds is no candidate holding the source; First is the first
// judged, whose Lacks says why.
type NoneHolds struct{ First Judged }

func (n NoneHolds) Error() string { return n.First.Asset.Name + " " + n.First.Lacks }

// compressionOrder is which of one source in several compressions is
// taken: gzip, then those a use_ option extracts, bzip2, xz, lzip, lzma,
// plain tar, and zip, then zstd, which takes extract.cmd and a dependency,
// and 7z and a disk image last, a .dmg rarely holding source.
var compressionOrder = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.lz", ".tar.lzma", ".tar", ".zip", ".tar.zst", ".7z", ".dmg"}

// Choose is the candidate that is the source, among those judged:
// passing ones with the same files and contents (SameFiles) are one
// source in several compressions, the first in compressionOrder taken.
// Passing ones with different files, or the same files with different
// contents, are Ambiguous; none passing is NoneHolds.
func Choose(judged []Judged) (Judged, error) {
	var passing []Judged
	for _, candidate := range judged {
		if candidate.Lacks == "" {
			passing = append(passing, candidate)
		}
	}
	if len(passing) == 0 {
		if len(judged) == 0 {
			return Judged{}, NoneHolds{First: Judged{Lacks: "isn't there"}}
		}
		return Judged{}, NoneHolds{First: judged[0]}
	}
	for _, other := range passing[1:] {
		if !SameFiles(passing[0].Inventory, other.Inventory) {
			var names []string
			for _, candidate := range passing {
				names = append(names, candidate.Asset.Name)
			}
			return Judged{}, Ambiguous{Names: names}
		}
	}
	rank := func(name string) int {
		extraction, _ := macports.ExtractionOf(name)
		return slices.Index(compressionOrder, extraction.Implied)
	}
	return slices.MinFunc(passing, func(a, b Judged) int { return rank(a.Asset.Name) - rank(b.Asset.Name) }), nil
}

// SameFiles reports two inventories holding the same regular files with
// the same contents, by digest, and the same symbolic links to the same
// targets: one source in two compressions. Directories aren't compared,
// an inventory's files and links saying what it holds, nor are other
// members, such as hard links, which Extract leaves out.
func SameFiles(a, b project.Inventory) bool {
	return maps.Equal(a.Files, b.Files) && maps.Equal(a.Links, b.Links)
}
