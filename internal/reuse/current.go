package reuse

import (
	"fmt"
	"maps"
	"slices"
	"strings"

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
// Portfile lines inside other subports' literal blocks (judge.Blocks):
// MacPorts runs a `subport X { … }` block's body only when evaluating X,
// so an edit inside terraform-1.16's reaches none of its siblings, which
// each patch bump built again on every release (field testing's terraform
// replay, 2026-10-22). And where it is only options no build reads
// (judge.Inert): omnidiff's openmaintainer line and its categories'
// reorder each rebuilt it (the omnidiff session's note 8). A port that
// moved, the directory it was built in gone from the revision, is found
// by its name, a target where the plan has it and an active port by
// judge.Moved, and its new directory judged against the recorded tree by
// the same rule. The zero Judge compares trees alone.
func Current(recorded model.TargetInputs, identity string, target model.PlanTarget, git *model.GitSource, trees map[string]model.ObjectID, judge Judge) bool {
	_, ok := Carry(recorded, "", identity, target, git, trees, judge)
	return ok
}

// Carry is Current, with the ports whose directories differ only in
// options no build reads, or moved, each with the options changed and
// where it moved from, which a result carried over says: none where every
// directory is the same tree, or differs only in other subports' blocks.
// origin is the run whose build read the inputs, whose change records
// judge.Inert reads.
func Carry(recorded model.TargetInputs, origin model.RunID, identity string, target model.PlanTarget, git *model.GitSource, trees map[string]model.ObjectID, judge Judge) ([]model.InertPort, bool) {
	if recorded.NotReusable != "" || !recorded.Complete() || identity == "" || recorded.Environment != identity {
		return nil, false
	}
	if !maps.Equal(recorded.Variants, target.Target.Variants) {
		return nil, false
	}
	if git == nil && recorded.Fetched != "" || git != nil && !git.BuiltBy(recorded.Fetched) {
		return nil, false
	}
	var carried []model.InertPort
	// The same directory, or one whose difference is only in other
	// subports' blocks than port's, or only in inert options: the
	// directory a build read (from) at its tree then, and where the port
	// is now (at).
	same := func(from string, then model.ObjectID, at string, port string) bool {
		now, ok := trees[at]
		moved := from != at
		entry := model.InertPort{Port: port}
		if moved {
			entry.From = from
		}
		switch {
		case !ok:
			return false
		case now == then && !moved:
			return true
		case now == then:
			carried = append(carried, entry)
			return true
		}
		if !moved && judge.Blocks != nil {
			if subports, ok := judge.Blocks(from, then, now); ok && !slices.Contains(subports, port) {
				return true
			}
		}
		// A Portfile that only declares ports the build had active, as
		// tidy declares what a check found it linking.
		if !moved && judge.Declared != nil {
			if ports, ok := judge.Declared(origin, from, then, now); ok && allActive(ports, recorded.Active) {
				entry.Declared = ports
				carried = append(carried, entry)
				return true
			}
		}
		if judge.Inert == nil {
			return false
		}
		options, ok := judge.Inert(origin, from, then, at, now)
		if !ok {
			return false
		}
		entry.Options = options
		carried = append(carried, entry)
		return true
	}
	// A target that moved is where the plan has it, once the directory it
	// was built in is gone.
	at := recorded.Directory
	if recorded.Directory != target.Directory {
		if _, held := trees[recorded.Directory]; held || judge.Inert == nil {
			return nil, false
		}
		at = target.Directory
	}
	if !same(recorded.Directory, recorded.Tree, at, target.Target.Name) || ResourcesChanged(recorded, trees) != "" {
		return nil, false
	}
	for _, port := range recorded.Active {
		at := port.Directory
		if _, held := trees[at]; !held && judge.Inert != nil && judge.Moved != nil {
			if moved, ok := judge.Moved(port.Name); ok {
				at = moved
			}
		}
		if !same(port.Directory, port.Tree, at, port.Name) {
			return nil, false
		}
	}
	return carried, true
}

// Judge reads a port directory whose tree differs from the one a build
// read, for whether the build stands for it still (Current): Blocks where
// the difference is only in other subports' literal blocks, Inert where
// it is only in options no build reads, and Moved where an active port's
// directory is now elsewhere, by its name. A nil field says nothing, and
// a move is followed only with Inert.
type Judge struct {
	Blocks   Blocks
	Inert    Inert
	Declared Declared
	Moved    func(port string) (directory string, ok bool)
}

// Declared says, of a port directory whose tree now differs from the one
// a build read, the ports its Portfile now declares that it didn't, ok
// only where that is all that differs (portfile.DeclaredChanges), its
// evaluations confirming it (fidelity.DeclaredBetween). Carry reuses the
// build where each port declared is one it had active, so declaring it
// builds nothing differently.
type Declared func(origin model.RunID, directory string, then, now model.ObjectID) (ports []string, ok bool)

// Blocks says, of a port directory whose tree now differs from the one a
// build read, which subports' literal blocks alone hold the difference
// (portfile.BlockChanges); ok only where nothing but the Portfile differs,
// both versions parse, every change is inside some subport's block, and
// the earlier tree is still held.
type Blocks func(directory string, then, now model.ObjectID) (subports []string, ok bool)

// Inert says, of a port directory a build read at one tree (then, at the
// directory from) that the revision now holds at another (now, at the
// directory at, from's own unless the port moved), the inert options that
// alone tell them apart (portfile.InertChanges): every entry but the
// Portfile the same object, the Portfile's changed lines each an inert
// option's, and the change records of the build's revision, origin's,
// and of the revision now finding nothing else moved
// (fidelity.InertBetween); ok only then.
type Inert func(origin model.RunID, from string, then model.ObjectID, at string, now model.ObjectID) (options []string, ok bool)

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

// allActive reports whether each port was among those active as a build
// ran, by name, as MacPorts reads names.
func allActive(ports []string, active []model.ActivePort) bool {
	for _, port := range ports {
		if !slices.ContainsFunc(active, func(a model.ActivePort) bool { return strings.EqualFold(a.Name, port) }) {
			return false
		}
	}
	return true
}
