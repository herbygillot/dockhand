// Package reuse is decision 28's reuse of evidence: what each target's
// build read, recorded as its inputs, so that a result can stand for a
// build that would read the same.
package reuse

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Trees names the trees a revision holds at directories;
// *git.Repository does.
type Trees interface {
	Directories(ctx context.Context, tree string, paths []string) (map[string]string, error)
}

// Inputs are what a target's build read from a revision in an environment
// of an identity: the ports a provider saw active as it built
// (buildenv.Build.Consumed), and the revision's trees for the target's
// directory, _resources, and each active port's directory. A directory the
// revision doesn't hold is left without a tree, which leaves the inputs
// incomplete. Active is nil where the provider couldn't say which ports
// were active, which leaves them incomplete too.
func Inputs(ctx context.Context, trees Trees, tree model.ObjectID, identity string, target model.PlanTarget, active []model.ActivePort) (model.TargetInputs, error) {
	paths := []string{target.Directory, macports.ResourcesDirectory}
	for _, port := range active {
		if port.Directory != "" {
			paths = append(paths, port.Directory)
		}
	}
	found, err := trees.Directories(ctx, string(tree), paths)
	if err != nil {
		return model.TargetInputs{}, err
	}
	var read []model.ActivePort
	if active != nil {
		read = make([]model.ActivePort, len(active))
	}
	for i, port := range active {
		port.Tree = ""
		if port.Directory != "" {
			port.Tree = model.ObjectID(found[port.Directory])
		}
		read[i] = port
	}
	return model.NewTargetInputs(identity, target.Directory, model.ObjectID(found[target.Directory]), model.ObjectID(found[macports.ResourcesDirectory]), target.Target.Variants, read), nil
}
