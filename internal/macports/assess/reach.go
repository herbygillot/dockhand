package assess

import (
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// NativeLibraryReach is the coverage policy setting apart a crate that
// links a native library where the port's build doesn't reach it.
const NativeLibraryReach = "native-library-reach"

// lockReach is what a Cargo.lock's port build reaches (project.CargoReach),
// with the arguments it was read under; known is false where it couldn't
// be read, and every crate said as before.
type lockReach struct {
	crates map[string]bool
	build  project.CargoFeatures
	known  bool
}

// basis is the build a reach is read for, in a finding's words.
func (r lockReach) basis() string {
	if len(r.build.Features) == 0 && !r.build.NoDefault {
		return "a build with default features on macOS"
	}
	return "a build with the Portfile's features on macOS"
}

// reach is what the port's build reaches of the Cargo.lock at file in the
// pair's new version, read from the lock's edges, the project's
// manifests beside and below it, and the port's cargo arguments
// (macports.CargoBuildArgs), at update, before any check (the omnidiff
// session's note 6).
func (a *assessment) reach(pair Pair, port macports.PortInfo, file string) lockReach {
	key := pair.Archive + "\x00" + file
	if found, ok := a.reaches[key]; ok {
		return found
	}
	found := a.readReach(pair, port, file)
	if a.reaches == nil {
		a.reaches = map[string]lockReach{}
	}
	a.reaches[key] = found
	return found
}

// readReach is reach, read.
func (a *assessment) readReach(pair Pair, port macports.PortInfo, file string) lockReach {
	found := lockReach{build: macports.CargoBuildArgs(port.Options)}
	lock, ok := pair.After.Files[file]
	if !ok {
		file = path.Join(pair.After.Root, file)
		lock, ok = pair.After.Files[file]
	}
	if !ok || lock.Truncated {
		return found
	}
	packages, err := project.ReadCargoLock(lock.Data)
	if err != nil {
		return found
	}
	top := path.Dir(file)
	manifests := map[string]project.CargoManifest{}
	for name, data := range pair.After.Files {
		if path.Base(name) != "Cargo.toml" || data.Truncated {
			continue
		}
		dir := path.Dir(name)
		switch rest, below := strings.CutPrefix(dir, top+"/"); {
		case dir == top:
			dir = ""
		case top == ".":
		case below:
			dir = rest
		default:
			continue
		}
		manifest, err := project.ReadCargoManifest(data.Data)
		if err != nil {
			return found
		}
		manifests[dir] = manifest
	}
	if found.build.All {
		return found
	}
	found.crates, found.known = project.CargoReach(packages, manifests, found.build)
	return found
}

// reached is a native crate as the port's build reaches it: false, set
// apart in coverage, where the build doesn't, "pinned for features or
// targets the port's build on macOS doesn't take"; the finding's words
// saying the build it was read for where it does; and, for a build
// passing --all-features, which prunes nothing, said once.
func (a *assessment) reached(change sourcecompare.Change, found *model.UpstreamChange, pair Pair, port macports.PortInfo) bool {
	reach := a.reach(pair, port, change.Path)
	switch {
	case reach.build.All:
		a.cover(model.Coverage{Path: change.Path, Relevance: "unknown", Treatment: "inspected",
			Reason: "every -sys crate Cargo.lock pins is said, since the port's build passes --all-features"})
	case !reach.known:
	case !reach.crates[change.Name]:
		a.cover(model.Coverage{Path: change.Path, Relevance: "unknown", Treatment: "set-apart", Policy: NativeLibraryReach, Reason: change.Name})
		return false
	default:
		found.Message = strings.Replace(found.Message, ", which links the native library", ", pinned for "+reach.basis()+", which links the native library", 1)
	}
	return true
}
