package assess

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// cargoLockVersions checks each source lock against the package versions
// declared by manifests below it. It is independent of Cargo feature reach:
// Cargo checks the lock file itself before building any selected feature.
func (a *assessment) cargoLockVersions(pair Pair) []model.UpstreamChange {
	patchName, suppressed, patched := a.cargoLockPatch()
	var found []model.UpstreamChange
	locks := slices.Sorted(func(yield func(string) bool) {
		for name := range pair.After.Files {
			if !yield(name) {
				return
			}
		}
	})
	for _, name := range locks {
		file := pair.After.Files[name]
		if path.Base(name) != project.CargoLock || file.Truncated {
			continue
		}
		packages, err := project.ReadCargoLock(file.Data)
		if err != nil {
			continue
		}
		if suppressed {
			if patched {
				found = append(found, model.UpstreamChange{Kind: "dependency", Path: name, Rule: CargoLockPatched, Subject: "Cargo.lock", Class: model.Present,
					Message: fmt.Sprintf("Cargo.lock is patched by %s, so dockhand leaves its package versions to the patch", patchName)})
			}
			continue
		}
		top := path.Dir(name)
		manifests := map[string]project.CargoManifest{}
		files := make(map[string]project.File, len(pair.After.CargoManifests)+len(pair.After.Files))
		for manifestName, manifestFile := range pair.After.Files {
			if path.Base(manifestName) == "Cargo.toml" {
				files[manifestName] = manifestFile
			}
		}
		for manifestName, manifestFile := range pair.After.CargoManifests {
			files[manifestName] = manifestFile
		}
		for _, manifestName := range slices.Sorted(func(yield func(string) bool) {
			for manifestName := range files {
				if !yield(manifestName) {
					return
				}
			}
		}) {
			manifestFile := files[manifestName]
			if path.Base(manifestName) != "Cargo.toml" || manifestFile.Truncated {
				continue
			}
			dir := path.Dir(manifestName)
			relative, ok := cargoRelative(top, dir)
			if !ok {
				continue
			}
			manifest, err := project.ReadCargoManifest(manifestFile.Data)
			if err == nil {
				manifests[relative] = manifest
			}
		}
		type manifestVersion struct {
			dir, version string
		}
		byName := map[string][]manifestVersion{}
		for _, dir := range slices.Sorted(func(yield func(string) bool) {
			for dir := range manifests {
				if !yield(dir) {
					return
				}
			}
		}) {
			manifest := manifests[dir]
			if manifest.Package == nil {
				continue
			}
			resolved := manifest
			if slices.Contains(manifest.Package.Inherited, "version") {
				root, ok := cargoWorkspace(manifests, dir)
				if !ok {
					continue
				}
				resolved = manifest.Inherit(root)
			}
			pkg := resolved.Package
			if pkg.Name == "" || pkg.Version == "" {
				continue
			}
			byName[pkg.Name] = append(byName[pkg.Name], manifestVersion{dir: dir, version: pkg.Version})
		}
		for _, packageName := range slices.Sorted(func(yield func(string) bool) {
			for packageName := range byName {
				if !yield(packageName) {
					return
				}
			}
		}) {
			var local []string
			matched := false
			for _, locked := range packages {
				if locked.Source != project.FromLocal || locked.Name != packageName {
					continue
				}
				local = append(local, locked.Version)
				matched = matched || slices.ContainsFunc(byName[packageName], func(manifest manifestVersion) bool { return manifest.version == locked.Version })
			}
			if len(local) == 0 || matched {
				continue
			}
			slices.Sort(local)
			local = slices.Compact(local)
			locked := strings.Join(local, ", ")
			nameManifests := byName[packageName]
			slices.SortFunc(nameManifests, func(a, b manifestVersion) int {
				return cmp.Or(cmp.Compare(cargoManifestDepth(a.dir), cargoManifestDepth(b.dir)), strings.Compare(a.dir, b.dir))
			})
			manifest := nameManifests[0]
			found = append(found, model.UpstreamChange{Kind: "dependency", Path: name, Rule: CargoLockStale, Subject: packageName,
				Class: model.Introduced, Hold: true, Message: fmt.Sprintf("Cargo.lock pins local package %s at %s, but %s says %s; a Cargo build with --frozen will refuse because it cannot update the lock file; a patch that refreshes Cargo.lock clears this hold", packageName, locked, cargoManifestName(manifest.dir), manifest.version)})
		}
	}
	return found
}

func cargoManifestDepth(directory string) int {
	if directory == "" || directory == "." {
		return 0
	}
	return strings.Count(directory, "/") + 1
}

// cargoLockPatch reports whether a named patch changes Cargo.lock, or an
// unreadable patch makes that answer uncertain. Either case suppresses a
// lock-version hold; only a known patch earns the notice.
func (a *assessment) cargoLockPatch() (name string, suppressed, patched bool) {
	uncertain := false
	for _, patch := range a.input.Patches {
		if patch.Dropped {
			continue
		}
		if !patch.ContentKnown {
			uncertain = true
			continue
		}
		if patchTouchesCargoLock(patch.Content) {
			return patch.Name, true, true
		}
	}
	for _, name := range portfile.PatchfileNames(a.input.Portfile) {
		at := slices.IndexFunc(a.input.Patches, func(p Patch) bool { return p.Name == name && !p.Dropped })
		if at < 0 {
			uncertain = true
			continue
		}
		patch := a.input.Patches[at]
		if !patch.ContentKnown {
			uncertain = true
			continue
		}
		if patchTouchesCargoLock(patch.Content) {
			return name, true, true
		}
	}
	return "", uncertain, false
}

func patchTouchesCargoLock(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !(strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "Index: ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "*** ")) {
			continue
		}
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, "\"'")
			if field == "Cargo.lock" || strings.HasSuffix(field, "/Cargo.lock") {
				return true
			}
		}
	}
	return false
}

func cargoRelative(top, directory string) (string, bool) {
	if top == directory {
		return "", true
	}
	if top == "." {
		return directory, directory != ".." && !strings.HasPrefix(directory, "../")
	}
	relative, ok := strings.CutPrefix(directory, top+"/")
	return relative, ok
}

func cargoWorkspace(manifests map[string]project.CargoManifest, directory string) (project.CargoManifest, bool) {
	for current := directory; ; current = path.Dir(current) {
		key := current
		if key == "." {
			key = ""
		}
		root, ok := manifests[key]
		if ok && root.Workspace != nil {
			relative := strings.TrimPrefix(strings.TrimPrefix(directory, current), "/")
			if current == directory || root.Workspace.Member(relative) {
				return root, true
			}
		}
		if current == "." || current == "" {
			break
		}
	}
	return project.CargoManifest{}, false
}

func cargoManifestName(directory string) string {
	if directory == "" || directory == "." {
		return "Cargo.toml"
	}
	return path.Join(directory, "Cargo.toml")
}
