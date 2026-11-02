package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// omnidiffManifest is omnidiff's shape: a default feature set, features
// turning optional dependencies on through dep:x and x/y, and dev-tool
// [[bin]]s gated on features default doesn't turn on.
const omnidiffManifest = `[package]
name = "omnidiff"

[features]
default = ["tui", "syntax"]
tui = ["dep:ratatui"]
syntax = ["syntect/default-fancy"]
git = ["dep:git2", "git2?/vendored-libgit2"]
stats = []
test-fixtures = ["stats"]

[dependencies]
ratatui = { version = "0.29", optional = true }
syntect = { version = "5", optional = true }
git2 = { version = "0.20", optional = true }
mimalloc = { version = "0.1", optional = true }
serde = "1"

[[bin]]
name = "omnidiff"
path = "src/main.rs"

[[bin]]
name = "gen-fixtures"
required-features = ["test-fixtures"]

[[bin]]
name = "diffstats"
required-features = ["stats"]

[[bin]]
name = "omnidiff-tui"
required-features = ["tui"]
`

// The features a build turns on are read by the Cargo Book's rules:
// default and what it names, dep:x and x/y turning a dependency on, x?/y
// not, an optional dependency's implicit feature, a member's own
// pkg/feature, --no-default-features, and --all-features turning on
// everything.
func TestACargoBuildsFeaturesAreReadByTheCargoBook(t *testing.T) {
	manifest, err := ReadCargoManifest([]byte(omnidiffManifest))
	require.NoError(t, err)
	features, dependencies := manifest.Enabled(CargoFeatures{})
	require.Equal(t, map[string]bool{"default": true, "tui": true, "syntax": true, "syntect": true}, features)
	require.Equal(t, map[string]bool{"ratatui": true, "syntect": true}, dependencies)

	features, dependencies = manifest.Enabled(CargoFeatures{Features: []string{"git", "omnidiff/test-fixtures", "mimalloc"}, NoDefault: true})
	require.Equal(t, map[string]bool{"git": true, "test-fixtures": true, "stats": true, "mimalloc": true}, features, "mimalloc is an optional dependency's implicit feature")
	require.Equal(t, map[string]bool{"git2": true, "mimalloc": true}, dependencies, "git2?/vendored-libgit2 alone wouldn't turn git2 on")

	_, dependencies = manifest.Enabled(CargoFeatures{Features: []string{"other/stats"}, NoDefault: true})
	require.Empty(t, dependencies, "another member's feature isn't this package's")

	features, dependencies = manifest.Enabled(CargoFeatures{All: true})
	require.Len(t, features, 6)
	require.Len(t, dependencies, 4)
}

// A [[bin]] whose required-features the build doesn't turn on isn't one
// of its programs: omnidiff's 21 dev tools, which create installed, and
// its destroot failed (the omnidiff session's note 1). The port's own
// features bring one back.
func TestABinBehindFeaturesTheBuildLeavesOffIsntAProgram(t *testing.T) {
	manifest, err := ReadCargoManifest([]byte(omnidiffManifest))
	require.NoError(t, err)
	require.Equal(t, []string{"omnidiff", "omnidiff-tui"}, manifest.Programs(true, CargoFeatures{}))
	require.Equal(t, []string{"omnidiff", "diffstats", "omnidiff-tui"}, manifest.Programs(true, CargoFeatures{Features: []string{"stats"}}))
	require.Equal(t, []string{"omnidiff", "gen-fixtures", "diffstats"}, manifest.Programs(true, CargoFeatures{Features: []string{"test-fixtures"}, NoDefault: true}))
	require.Equal(t, []string{"omnidiff", "gen-fixtures", "diffstats", "omnidiff-tui"}, manifest.Programs(true, CargoFeatures{All: true}))

	files := map[string][]byte{"Cargo.toml": []byte(omnidiffManifest)}
	require.Equal(t, []string{"omnidiff", "omnidiff-tui"}, CargoPrograms(files, func(string) bool { return true }, CargoFeatures{}))
}
