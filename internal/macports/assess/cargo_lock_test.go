package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

func cargoLock(version string, members ...string) string {
	lock := "version = 4\n\n[[package]]\nname = \"demo\"\nversion = \"" + version + "\"\n"
	for _, member := range members {
		lock += "\n[[package]]\nname = \"" + member + "\"\nversion = \"1.0.0\"\n"
	}
	return lock
}

func cargoLockChanges(t *testing.T, files map[string]string) []model.UpstreamChange {
	t.Helper()
	reading := read(t, "src", files, project.Spec{})
	comparison := Assess(Input{Pairs: []Pair{{After: reading}}})
	return comparison.Changes
}

func cargoLockFindings(changes []model.UpstreamChange) []model.UpstreamChange {
	var findings []model.UpstreamChange
	for _, change := range changes {
		if change.Rule == CargoLockStale || change.Rule == CargoLockPatched {
			findings = append(findings, change)
		}
	}
	return findings
}

func TestCargoLockStalePackageVersionHolds(t *testing.T) {
	changes := cargoLockChanges(t, map[string]string{
		"Cargo.lock": cargoLock("1.0.0"),
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	})
	require.Len(t, changes, 1)
	require.Equal(t, "cargo-lock-stale", changes[0].Rule)
	require.Equal(t, "Cargo.lock", changes[0].Path)
	require.True(t, changes[0].Hold)
	require.Contains(t, changes[0].Message, "demo")
	require.Contains(t, changes[0].Message, "1.0.0")
	require.Contains(t, changes[0].Message, "1.1.0")
	require.Contains(t, changes[0].Message, "--frozen")
	require.Contains(t, changes[0].Message, "a patch that refreshes Cargo.lock")
}

func TestCargoLockCurrentPackageDoesNotHold(t *testing.T) {
	require.Empty(t, cargoLockChanges(t, map[string]string{
		"Cargo.lock": cargoLock("1.1.0"),
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	}))
}

func TestCargoLockWorkspaceStaleMemberHoldsAndInheritedVersionResolves(t *testing.T) {
	changes := cargoLockChanges(t, map[string]string{
		"Cargo.lock":                  cargoLock("1.0.0", "fresh", "inherited"),
		"Cargo.toml":                  "[workspace]\nmembers = [\"crates/*\"]\n\n[workspace.package]\nversion = \"2.0.0\"\n",
		"crates/fresh/Cargo.toml":     "[package]\nname = \"fresh\"\nversion = \"1.0.0\"\n",
		"crates/inherited/Cargo.toml": "[package]\nname = \"inherited\"\nversion.workspace = true\n",
	})
	require.Len(t, changes, 1)
	require.Equal(t, "cargo-lock-stale", changes[0].Rule)
	require.Contains(t, changes[0].Message, "inherited")
	require.Contains(t, changes[0].Message, "2.0.0")
}

func TestCargoLockSkipsLocalPackageWithoutManifest(t *testing.T) {
	require.Empty(t, cargoLockChanges(t, map[string]string{
		"Cargo.lock": cargoLock("1.0.0", "path-only"),
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.0.0\"\n",
	}))
}

func TestCargoLockSkipsManifestPackageAbsentFromLock(t *testing.T) {
	require.Empty(t, cargoLockChanges(t, map[string]string{
		"Cargo.lock": cargoLock("1.0.0"),
		"Cargo.toml": "[package]\nname = \"path-dependency\"\nversion = \"2.0.0\"\n",
	}))
}

func TestCargoLockMatchingWorkspaceManifestClearsSameNamedFixtureVersion(t *testing.T) {
	require.Empty(t, cargoLockFindings(cargoLockChanges(t, map[string]string{
		"Cargo.lock":                     cargoLock("1.1.0"),
		"Cargo.toml":                     "[workspace]\nmembers = [\"crates/demo\"]\n",
		"crates/demo/Cargo.toml":         "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
		"tests/fixtures/demo/Cargo.toml": "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n",
	})))
}

func TestCargoLockReportsOneStaleSameNamedManifestNearestLock(t *testing.T) {
	changes := cargoLockFindings(cargoLockChanges(t, map[string]string{
		"Cargo.lock":                     cargoLock("1.0.0"),
		"Cargo.toml":                     "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
		"tests/fixtures/demo/Cargo.toml": "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n",
	}))
	require.Len(t, changes, 1)
	require.Contains(t, changes[0].Message, "Cargo.toml says 1.1.0")
	require.NotContains(t, changes[0].Message, "tests/fixtures/demo/Cargo.toml")
}

func TestCargoLockIgnoresNonlocalPackageWithSameName(t *testing.T) {
	lock := "version = 4\n\n[[package]]\nname = \"demo\"\nversion = \"1.0.0\"\nsource = \"git+https://example.org/demo#abc\"\n"
	require.Empty(t, cargoLockFindings(cargoLockChanges(t, map[string]string{
		"Cargo.lock": lock,
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	})))
}

func TestCargoLockSkipsUnreadableManifest(t *testing.T) {
	reading := project.Reading{Files: map[string]project.File{
		"Cargo.lock": {Data: []byte(cargoLock("1.0.0"))},
		"Cargo.toml": {Data: []byte("[package\nname = 'demo'\n")},
	}}
	require.Empty(t, cargoLockFindings(Assess(Input{Pairs: []Pair{{After: reading}}}).Changes))
}

func TestCargoLockMatchingLocalVersionClearsSameNameMismatch(t *testing.T) {
	lock := "version = 4\n\n[[package]]\nname = \"demo\"\nversion = \"1.0.0\"\n\n[[package]]\nname = \"demo\"\nversion = \"1.1.0\"\n"
	require.Empty(t, cargoLockChanges(t, map[string]string{
		"Cargo.lock": lock,
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	}))
}

func TestCargoLockStaleCheckRunsWithAllFeatures(t *testing.T) {
	reading := read(t, "src", map[string]string{
		"Cargo.lock": cargoLock("1.0.0"),
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	}, project.Spec{})
	changes := Assess(Input{Port: macports.PortInfo{Options: map[string]string{"cargo.features": "all"}}, Pairs: []Pair{{After: reading}}}).Changes
	require.Len(t, changes, 1)
	require.Equal(t, "cargo-lock-stale", changes[0].Rule)
}

func TestCargoLockPatchSuppressesHoldAndSaysLockIsPatched(t *testing.T) {
	reading := read(t, "src", map[string]string{
		"Cargo.lock": cargoLock("1.0.0"),
		"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	}, project.Spec{})
	comparison := Assess(Input{Portfile: []byte("patchfiles fix.diff\n"), Patches: []Patch{{Result: patchResult("fix.diff"), Content: []byte("diff --git a/Cargo.lock b/Cargo.lock\n--- a/Cargo.lock\n+++ b/Cargo.lock\n"), ContentKnown: true}},
		Pairs: []Pair{{After: reading}}})
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, CargoLockPatched, comparison.Changes[0].Rule)
	require.False(t, comparison.Changes[0].Hold)
	require.Contains(t, comparison.Changes[0].Message, "Cargo.lock is patched")
}

func TestCargoLockSkipsManifestsOutsideLockSubtree(t *testing.T) {
	require.Empty(t, cargoLockChanges(t, map[string]string{
		"project/Cargo.lock": cargoLock("1.0.0"),
		"outside/Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	}))
}

func TestCargoLockAboveWorkDirectoryIncludesNestedManifests(t *testing.T) {
	changes := cargoLockChanges(t, map[string]string{
		"Cargo.lock":                 cargoLock("1.0.0"),
		"workspace/crate/Cargo.toml": "[package]\nname = \"demo\"\nversion = \"1.1.0\"\n",
	})
	require.Len(t, changes, 1)
	require.Equal(t, "cargo-lock-stale", changes[0].Rule)
}

func patchResult(name string) patchcheck.Result {
	return patchcheck.Result{Name: name, Checked: true, Applies: true}
}
