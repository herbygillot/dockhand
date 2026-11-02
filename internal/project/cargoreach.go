package project

import (
	"path"
	"slices"
	"strings"
)

// CargoReach are the packages of a Cargo.lock a build can reach, by name:
// Cargo.lock pins every crate any feature or target could build, so it
// named omnidiff's libgit2-sys, libsqlite3-sys, libmimalloc-sys, and
// onig_sys, none of which its build compiles (the omnidiff session's note
// 6). manifests are the project's Cargo.toml files, by their directory
// below the lock's, the root's "" (a member's is read with what it
// inherits). The built packages are those -p names, else the root's and
// its workspace's members'; from each, the dependencies the build turns
// on, every non-optional one of a dependencies or build-dependencies
// table, an optional one its features turn on (CargoManifest.Enabled),
// and a target's table unless it doesn't apply on macOS
// (CargoTargetOnMacOS); then every package the lock's edges reach from
// those. A local package another local package depends on takes every
// optional dependency, since its dependents' feature requests aren't
// read, and any other package every edge the lock gives it: it
// over-approximates, never dropping what the build compiles. False where
// the lock or manifests don't say which packages are built.
func CargoReach(lock []CargoPackage, manifests map[string]CargoManifest, build CargoFeatures) (map[string]bool, bool) {
	root, ok := manifests[""]
	if !ok {
		return nil, false
	}
	// Each local package's manifest, by its name, and whether it's built.
	local := map[string]CargoManifest{}
	built := map[string]bool{}
	for dir, manifest := range manifests {
		if manifest.Package == nil || manifest.Package.Name == "" {
			continue
		}
		manifest = manifest.Inherit(root)
		local[manifest.Package.Name] = manifest
		member := dir == "" || root.Workspace != nil && root.Workspace.Member(dir)
		if len(build.Packages) > 0 && slices.Contains(build.Packages, manifest.Package.Name) || len(build.Packages) == 0 && member {
			built[manifest.Package.Name] = true
		}
	}
	for _, name := range build.Packages {
		if !built[name] {
			return nil, false
		}
	}
	if len(built) == 0 {
		return nil, false
	}
	byName := map[string][]int{}
	for i, pkg := range lock {
		byName[pkg.Name] = append(byName[pkg.Name], i)
	}
	// enabled are the crates a local package's build turns on.
	enabled := func(manifest CargoManifest, features CargoFeatures, everyOptional bool) map[string]bool {
		_, optional := manifest.Enabled(features)
		crates := map[string]bool{}
		for _, dependency := range manifest.Dependencies {
			table := dependency.Table
			if key, ok := strings.CutPrefix(table, "target."); ok {
				applies, err := dependency.OnMacOS()
				if err == nil && applies == No {
					continue
				}
				table = path.Ext(key)
				table = strings.TrimPrefix(table, ".")
			}
			if table != "dependencies" && table != "build-dependencies" {
				continue
			}
			if dependency.Optional && !everyOptional && !optional[dependency.Name] {
				continue
			}
			crates[dependency.Crate()] = true
		}
		return crates
	}
	// A local package another depends on may have features turned on by
	// it, which aren't read: every optional dependency of it is taken.
	dependedOn := map[string]bool{}
	for _, pkg := range lock {
		if pkg.Source != FromLocal {
			continue
		}
		for _, edge := range pkg.Dependencies {
			if name, _ := cargoEdge(edge); name != pkg.Name {
				dependedOn[name] = true
			}
		}
	}
	reach := map[string]bool{}
	seen := map[int]bool{}
	var queue []int
	for i, pkg := range lock {
		if pkg.Source == FromLocal && built[pkg.Name] {
			seen[i] = true
			queue = append(queue, i)
		}
	}
	if len(queue) == 0 {
		return nil, false
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		pkg := lock[i]
		reach[pkg.Name] = true
		var follows map[string]bool
		if manifest, ok := local[pkg.Name]; ok && pkg.Source == FromLocal {
			follows = enabled(manifest, build, !built[pkg.Name] || dependedOn[pkg.Name])
		}
		for _, edge := range pkg.Dependencies {
			name, version := cargoEdge(edge)
			if follows != nil && !follows[name] {
				continue
			}
			for _, j := range byName[name] {
				if version != "" && lock[j].Version != version || seen[j] {
					continue
				}
				seen[j] = true
				queue = append(queue, j)
			}
		}
	}
	return reach, true
}

// cargoEdge is a lock's dependency edge's name and version, the version
// empty where the lock names the package alone: "name", "name version",
// or "name version (source)".
func cargoEdge(edge string) (name, version string) {
	fields := strings.Fields(edge)
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) > 1 {
		version = fields[1]
	}
	return fields[0], version
}
