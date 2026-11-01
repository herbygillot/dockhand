// Package reuse is decision 28's reuse of evidence: what each target's
// build read, recorded as its inputs, so that a result can stand for a
// build that would read the same.
package reuse

import (
	"context"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Trees names the trees a revision holds at directories, and reads a
// tree's entries; *git.Repository does.
type Trees interface {
	Directories(ctx context.Context, tree string, paths []string) (map[string]string, error)
	ReadTree(ctx context.Context, object string) ([]git.TreeEntry, error)
}

// Inputs are what a target's build read from a revision in an environment
// of an identity: the ports a provider saw active as it built
// (buildenv.Build.Consumed), and the revision's trees for the target's
// directory and each active port's directory, and _resources. A directory
// the revision doesn't hold is left without a tree, which leaves the
// inputs incomplete. Active is nil where the provider couldn't say which
// ports were active, which leaves them incomplete too.
//
// _resources is named by what the build read of it where the provider
// said which PortGroups it loaded (sourced, each file's blob by its path,
// as the guest recorded them): those, and what every port's build may read
// there, by the revision's objects (ResourcesRead). An active port the
// environment built from source for the check comes with the PortGroups
// its build loaded (model.ActivePort.PortGroups), which name what the
// build read of _resources too; one installed from an archive comes with
// none, its archive naming it. Where the provider didn't say what the
// build loaded, or named a file that isn't one of the tree's PortGroups,
// for the target or a port it built, _resources is named by its tree, as
// every build was before, and the ports' PortGroups are left out, the
// tree holding them.
func Inputs(ctx context.Context, trees Trees, tree model.ObjectID, identity string, target model.PlanTarget, active []model.ActivePort, sourced map[string]model.ObjectID) (model.TargetInputs, error) {
	paths := []string{target.Directory, macports.ResourcesDirectory}
	for _, port := range active {
		if port.Directory != "" {
			paths = append(paths, port.Directory)
		}
	}
	found, err := Objects(ctx, trees, tree, paths)
	if err != nil {
		return model.TargetInputs{}, err
	}
	var read []model.ActivePort
	if active != nil {
		read = make([]model.ActivePort, len(active))
	}
	for i, port := range active {
		port.Tree = ""
		port.PortGroups = maps.Clone(port.PortGroups)
		if port.Directory != "" {
			port.Tree = found[port.Directory]
		}
		read[i] = port
	}
	inputs := model.NewTargetInputs(identity, target.Directory, found[target.Directory], found[macports.ResourcesDirectory], target.Target.Variants, read)
	loaded := portGroups(sourced)
	for _, port := range inputs.Active {
		if port.PortGroups != nil && portGroups(port.PortGroups) == nil {
			loaded = nil
		}
	}
	if loaded != nil && found[macports.ResourcesDirectory] != "" {
		inputs.Resources = ""
		inputs.ResourcesRead = &model.ResourcesRead{Shared: shared(found), PortGroups: loaded}
		return inputs, nil
	}
	for i := range inputs.Active {
		inputs.Active[i].PortGroups = nil
	}
	return inputs, nil
}

// portGroups are the PortGroup files a provider said a build loaded, nil
// where it didn't say, or named a file that isn't a PortGroup of the tree,
// whose read dockhand can't name.
func portGroups(sourced map[string]model.ObjectID) map[string]model.ObjectID {
	if sourced == nil {
		return nil
	}
	for file, blob := range sourced {
		if _, ok := macports.PortGroupAt(file); !ok || !git.ValidObjectID(string(blob)) {
			return nil
		}
	}
	return maps.Clone(sourced)
}

// Objects are what a revision holds at the paths recorded inputs name
// (Paths): each directory's tree, and where _resources is among them,
// what of it inputs may name by its parts (model.ResourcesRead): each
// entry beside the PortGroups, on the way down to their directory, and
// each PortGroup file's blob. A path the revision doesn't hold is absent.
func Objects(ctx context.Context, trees Trees, tree model.ObjectID, paths []string) (map[string]model.ObjectID, error) {
	asked := slices.Clone(paths)
	slices.Sort(asked)
	found, err := trees.Directories(ctx, string(tree), slices.Compact(asked))
	if err != nil {
		return nil, err
	}
	objects := map[string]model.ObjectID{}
	for path, object := range found {
		objects[path] = model.ObjectID(object)
	}
	// Each directory from _resources down to the PortGroups' gives its
	// entries but the next one down, and the PortGroups' its files, each
	// found in the tree above it: ls-tree asked for a directory and one
	// inside it lists the outer's entries in place of the outer itself.
	levels := append([]string{macports.ResourcesDirectory}, towardPortGroups()...)
	object, ok := found[macports.ResourcesDirectory]
	if !slices.Contains(paths, macports.ResourcesDirectory) || !ok {
		return objects, nil
	}
	for i, directory := range levels {
		entries, err := trees.ReadTree(ctx, object)
		if err != nil {
			return nil, err
		}
		object = ""
		for _, entry := range entries {
			if i+1 < len(levels) && directory+"/"+entry.Name == levels[i+1] {
				object = entry.Object
				continue
			}
			objects[directory+"/"+entry.Name] = model.ObjectID(entry.Object)
		}
		if object == "" {
			break
		}
	}
	return objects, nil
}

// towardPortGroups are the directories below _resources down to the
// PortGroups', the last: _resources/port1.0, _resources/port1.0/group.
func towardPortGroups() []string {
	var directories []string
	for directory := macports.PortGroupDirectory; directory != macports.ResourcesDirectory; directory = path.Dir(directory) {
		directories = append([]string{directory}, directories...)
	}
	return directories
}

// shared are, of a revision's objects (Objects), the entries of _resources
// beside the PortGroups, which MacPorts reads for any port: the compiler
// lists, the fetch and livecheck definitions, the SDK lists, the variant
// descriptions, and whatever is added beside them.
func shared(objects map[string]model.ObjectID) map[string]model.ObjectID {
	found := map[string]model.ObjectID{}
	on := towardPortGroups()
	for path, object := range objects {
		if strings.HasPrefix(path, macports.ResourcesDirectory+"/") && !slices.Contains(on, path) && !strings.HasPrefix(path, macports.PortGroupDirectory+"/") {
			found[path] = object
		}
	}
	return found
}
