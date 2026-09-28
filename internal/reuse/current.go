package reuse

import (
	"maps"

	"github.com/herbygillot/dockhand/internal/model"
)

// Paths are the directories whose trees say whether recorded inputs are
// what a build would read now: the target's own, _resources, and each
// active port's that has one. A port the guest couldn't place in the tree
// has none, and leaves the inputs incomplete (Current).
func Paths(recorded model.TargetInputs) []string {
	paths := []string{recorded.Directory, Resources}
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
func Current(recorded model.TargetInputs, identity string, target model.PlanTarget, trees map[string]model.ObjectID) bool {
	if !recorded.Complete() || identity == "" || recorded.Environment != identity {
		return false
	}
	if recorded.Directory != target.Directory || !maps.Equal(recorded.Variants, target.Target.Variants) {
		return false
	}
	if trees[recorded.Directory] != recorded.Tree || trees[Resources] != recorded.Resources {
		return false
	}
	for _, port := range recorded.Active {
		if tree, ok := trees[port.Directory]; !ok || tree != port.Tree {
			return false
		}
	}
	return true
}
