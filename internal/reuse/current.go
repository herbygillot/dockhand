package reuse

import (
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Paths are the directories whose trees say whether recorded inputs are
// what a build would read now: the target's own, _resources, and each
// active port's that has one. A port the guest couldn't place in the tree
// has none, and leaves the inputs incomplete (Current).
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
// the target's directory, _resources, and every active port's directory
// holding the same trees in the revision now (trees, by path, from
// Paths). Identical reads mean the same build, so its result stands.
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
	if !same(recorded.Directory, recorded.Tree, target.Target.Name) || trees[macports.ResourcesDirectory] != recorded.Resources {
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
