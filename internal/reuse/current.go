package reuse

import (
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Paths are the directories whose trees say whether recorded inputs are
// what a build would read now: the target's own, _resources, and each
// active port's that has one, read with Objects. A port the guest
// couldn't place in the tree has none, and leaves the inputs incomplete
// (Current).
func Paths(recorded model.TargetInputs) []string {
	if recorded.NotReusable != "" {
		return nil
	}
	paths := []string{recorded.Directory, macports.ResourcesDirectory}
	for _, port := range recorded.Active {
		if port.Directory != "" {
			paths = append(paths, port.Directory)
		}
	}
	return paths
}

// Current reports whether a build's recorded inputs are what the target's
// build would read now (decision 28): complete, recorded in an environment
// of the identity the environment has now, for the same variants, with
// the target's directory and every active port's directory holding the
// same trees in the revision now, and what it read of _resources the same
// (ResourcesChanged; trees, by path, from Objects of Paths). Identical
// reads mean the same build, so its result stands.
//
// An active port's archive isn't known before a build runs, so it isn't
// compared here: the same directory at the same tree is the same port and
// version, whose archive MacPorts' packages serve, or dockhand kept. A
// build step reading another port's directory, a download over the
// network, and a nondeterministic build are the accepted gaps.
//
// A port fetched with Git is the exception: the same Portfile names a tag,
// which binds nothing, so the same tree can fetch another source. Where
// the target is fetched with Git now (git, from the plan), its earlier
// build stands only where it recorded fetching the commit the plan expects
// now (batch 20); one that recorded none, as every build before dockhand
// recorded it, or whose provider couldn't say, can't be established to
// have built it. A build that recorded a commit stands for no target that
// isn't fetched with Git now.
//
// A directory whose tree differs stands too where the difference is only
// Portfile lines inside other subports' literal blocks (blocks): MacPorts
// runs a `subport X { … }` block's body only when evaluating X, so an edit
// inside terraform-1.16's reaches none of its siblings, which each patch
// bump built again on every release (field testing's terraform replay,
// 2026-10-22). blocks nil compares trees alone.
func Current(recorded model.TargetInputs, identity string, target model.PlanTarget, git *model.GitSource, trees map[string]model.ObjectID, blocks Blocks) bool {
	if recorded.NotReusable != "" || !recorded.Complete() || identity == "" || recorded.Environment != identity {
		return false
	}
	if recorded.Directory != target.Directory || !maps.Equal(recorded.Variants, target.Target.Variants) {
		return false
	}
	if git == nil && recorded.Fetched != "" || git != nil && !git.BuiltBy(recorded.Fetched) {
		return false
	}
	// The same directory, or one whose difference is only in other
	// subports' blocks than port's.
	same := func(directory string, then model.ObjectID, port string) bool {
		now, ok := trees[directory]
		switch {
		case !ok:
			return false
		case now == then:
			return true
		case blocks == nil:
			return false
		}
		subports, ok := blocks(directory, then, now)
		return ok && !slices.Contains(subports, port)
	}
	if !same(recorded.Directory, recorded.Tree, target.Target.Name) || ResourcesChanged(recorded, trees) != "" {
		return false
	}
	for _, port := range recorded.Active {
		if !same(port.Directory, port.Tree, port.Name) {
			return false
		}
	}
	return true
}

// Blocks says, of a port directory whose tree now differs from the one a
// build read, which subports' literal blocks alone hold the difference
// (portfile.BlockChanges); ok only where nothing but the Portfile differs,
// both versions parse, every change is inside some subport's block, and
// the earlier tree is still held.
type Blocks func(directory string, then, now model.ObjectID) (subports []string, ok bool)

// ResourcesChanged says what a build read of _resources that the revision
// now holds otherwise (trees, by path, from Objects), or nothing where it
// is all the same. Inputs that name _resources by its tree read all of
// it, so any change to it is one: "_resources". Those that name what of
// it the build read (model.ResourcesRead) changed only where a PortGroup
// the build loaded did, "the cmake 1.1 PortGroup", or an entry every port
// reads, or one was added or went, "_resources/port1.0/compilers": a
// PortGroup the build didn't load changes nothing of it. A port that would
// load another PortGroup now does so by a change to its Portfile, or to a
// PortGroup it loaded, so that is a change already.
//
// What an active port the environment built from source for the check
// loaded is among what it read (model.ActivePort.PortGroups), so a change
// to one of those is a change too: "the golang 1.0 PortGroup, which
// gopls, built from source, loaded". A port installed from an archive,
// MacPorts' or the one dockhand kept, is named by its directory's tree and
// its archive alone, whichever PortGroup changed since: the archive is
// what the build read, and only a revision bump, a change to its
// directory, replaces it.
func ResourcesChanged(recorded model.TargetInputs, trees map[string]model.ObjectID) string {
	read := recorded.ResourcesRead
	if read == nil {
		if now, ok := trees[macports.ResourcesDirectory]; !ok || now != recorded.Resources {
			return macports.ResourcesDirectory
		}
		return ""
	}
	if changed := PortGroupChanged(read.PortGroups, trees); changed != "" {
		return changed
	}
	for _, port := range recorded.Active {
		if changed := PortGroupChanged(port.PortGroups, trees); changed != "" {
			return fmt.Sprintf("%s, which %s, built from source, loaded", changed, port.Name)
		}
	}
	now := shared(trees)
	paths := slices.Sorted(maps.Keys(now))
	for path := range read.Shared {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	for _, path := range paths {
		if then, ok := read.Shared[path]; !ok || now[path] != then {
			return path
		}
	}
	return ""
}

// PortGroupChanged says the first of the PortGroup files, each by its blob
// by its path, that the revision now holds otherwise or not at all (trees,
// from Objects), "the cmake 1.1 PortGroup", or nothing where none does: of
// a build's, or of a kept archive's (model.DependencyArchive).
func PortGroupChanged(files map[string]model.ObjectID, trees map[string]model.ObjectID) string {
	for _, file := range slices.Sorted(maps.Keys(files)) {
		if now, ok := trees[file]; !ok || now != files[file] {
			if group, ok := macports.PortGroupAt(file); ok {
				return fmt.Sprintf("the %s %s PortGroup", group.Name, group.Version)
			}
			return file
		}
	}
	return ""
}
