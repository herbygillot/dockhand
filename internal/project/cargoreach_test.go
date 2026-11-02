package project

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// reachLock is an omnidiff-shaped Cargo.lock: every crate any feature or
// target could build, each edge in one of the lock's forms.
func reachLock(t *testing.T) []CargoPackage {
	sum := strings.Repeat("a", 64)
	registry := "registry+https://github.com/rust-lang/crates.io-index"
	var text strings.Builder
	text.WriteString("version = 4\n")
	pkg := func(name, version string, local bool, dependencies ...string) {
		fmt.Fprintf(&text, "\n[[package]]\nname = %q\nversion = %q\n", name, version)
		if !local {
			fmt.Fprintf(&text, "source = %q\nchecksum = %q\n", registry, sum)
		}
		if len(dependencies) > 0 {
			fmt.Fprintf(&text, "dependencies = [%s]\n", `"`+strings.Join(dependencies, `", "`)+`"`)
		}
	}
	pkg("omnidiff", "0.2.0", true, "git2", "mimalloc", "omnidiff-core", "ratatui", "serde 1.0.219", "syntect")
	pkg("omnidiff-core", "0.2.0", true, "rusqlite", "serde 1.0.219 ("+registry+")")
	pkg("ratatui", "0.29.0", false, "serde 1.0.100")
	pkg("serde", "1.0.100", false)
	pkg("serde", "1.0.219", false)
	pkg("syntect", "5.2.0", false, "onig")
	pkg("onig", "6.4.0", false, "onig_sys")
	pkg("onig_sys", "69.8.1", false)
	pkg("git2", "0.20.2", false, "libgit2-sys")
	pkg("libgit2-sys", "0.18.2+1.9.1", false)
	pkg("mimalloc", "0.1.47", false, "libmimalloc-sys")
	pkg("libmimalloc-sys", "0.1.43", false)
	pkg("rusqlite", "0.37.0", false, "libsqlite3-sys")
	pkg("libsqlite3-sys", "0.35.0", false)
	lock, err := ReadCargoLock([]byte(text.String()))
	require.NoError(t, err)
	return lock
}

// reachManifests are omnidiff-shaped manifests: optional dependencies
// behind features, default's among them, a musl target's table, and a
// workspace member whose only native crate is a test's.
func reachManifests(t *testing.T) map[string]CargoManifest {
	root, err := ReadCargoManifest([]byte(`[package]
name = "omnidiff"

[features]
default = ["tui", "syntax"]
tui = ["dep:ratatui"]
syntax = ["syntect/default-fancy"]
git = ["dep:git2"]

[dependencies]
ratatui = { version = "0.29", optional = true }
syntect = { version = "5", optional = true }
git2 = { version = "0.20", optional = true }
serde = { workspace = true }
omnidiff-core = { path = "crates/omnidiff-core" }

[target.'cfg(target_env = "musl")'.dependencies]
mimalloc = "0.1"

[workspace]
members = ["crates/*"]

[workspace.dependencies]
serde = "1"
`))
	require.NoError(t, err)
	core, err := ReadCargoManifest([]byte("[package]\nname = \"omnidiff-core\"\n\n[dependencies]\nserde = { workspace = true }\n\n[dev-dependencies]\nrusqlite = \"0.37\"\n"))
	require.NoError(t, err)
	return map[string]CargoManifest{"": root, "crates/omnidiff-core": core}
}

// A Cargo.lock is pruned by what the build can reach over its edges, from
// the dependencies the built packages' manifests turn on: omnidiff's
// default build reaches onig_sys, through syntect, and none of
// libgit2-sys, behind a feature off by default, libmimalloc-sys, a musl
// target's, or libsqlite3-sys, a test's (the omnidiff session's note 6).
// The port's features bring one back or drop a default's, and -p builds
// what it names.
func TestACargoLockIsPrunedByWhatTheBuildReaches(t *testing.T) {
	lock, manifests := reachLock(t), reachManifests(t)
	names := func(reach map[string]bool) []string {
		var found []string
		for _, pkg := range lock {
			if reach[pkg.Name] && !slices.Contains(found, pkg.Name) {
				found = append(found, pkg.Name)
			}
		}
		return found
	}
	reach, ok := CargoReach(lock, manifests, CargoFeatures{})
	require.True(t, ok)
	require.Equal(t, []string{"omnidiff", "omnidiff-core", "ratatui", "serde", "syntect", "onig", "onig_sys"}, names(reach))
	// What such a build compiles is every one in the reach.
	for _, compiled := range []string{"serde", "ratatui", "onig_sys", "onig", "syntect", "omnidiff-core", "omnidiff"} {
		require.True(t, reach[compiled], compiled)
	}

	reach, _ = CargoReach(lock, manifests, CargoFeatures{Features: []string{"git"}})
	require.True(t, reach["libgit2-sys"], "--features git brings libgit2-sys back")
	reach, _ = CargoReach(lock, manifests, CargoFeatures{NoDefault: true})
	require.Equal(t, []string{"omnidiff", "omnidiff-core", "serde"}, names(reach), "--no-default-features drops a default's")
	reach, _ = CargoReach(lock, manifests, CargoFeatures{Packages: []string{"omnidiff-core"}})
	require.Equal(t, []string{"omnidiff-core", "serde"}, names(reach))

	_, ok = CargoReach(lock, manifests, CargoFeatures{Packages: []string{"elsewhere"}})
	require.False(t, ok, "a package -p names that isn't here")
	_, ok = CargoReach(lock, map[string]CargoManifest{"crates/omnidiff-core": manifests["crates/omnidiff-core"]}, CargoFeatures{})
	require.False(t, ok, "no root manifest")
}

// A lock's edges are read in each form it writes them, a version telling
// two of a name apart.
func TestACargoLocksEdgesAreRead(t *testing.T) {
	lock := reachLock(t)
	require.Equal(t, []string{"git2", "mimalloc", "omnidiff-core", "ratatui", "serde 1.0.219", "syntect"}, lock[0].Dependencies)
	for edge, want := range map[string][2]string{
		"serde":         {"serde", ""},
		"serde 1.0.219": {"serde", "1.0.219"},
		"serde 1.0.219 (registry+https://github.com/rust-lang/crates.io-index)": {"serde", "1.0.219"},
	} {
		name, version := cargoEdge(edge)
		require.Equal(t, want, [2]string{name, version}, edge)
	}
}
