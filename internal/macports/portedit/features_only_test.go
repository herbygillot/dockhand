package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A patch asking other features of a Cargo dependency leaves the lock, and
// the crates, as they are; one that adds a dependency, or touches the
// lock, doesn't (field testing, batch 12: jgenesis's sdl3 patch).
func TestAFeaturesOnlyCargoPatchLeavesTheCrates(t *testing.T) {
	t.Parallel()
	features := `--- jgenesis-gui/Cargo.toml.orig
+++ jgenesis-gui/Cargo.toml
@@ -10,7 +10,7 @@
 [dependencies]
-sdl3 = { workspace = true, features = ["build-from-source-static"] }
+sdl3 = { workspace = true, features = ["use-pkg-config"] }
 serde = "1"
`
	require.True(t, featuresOnly([]byte(features)))
	added := features + "@@ -20,0 +20,1 @@\n+pkg-config = \"0.3\"\n"
	require.False(t, featuresOnly([]byte(added)), "a new dependency")
	lock := "--- Cargo.lock.orig\n+++ Cargo.lock\n@@ -1 +1 @@\n-x\n+y\n"
	require.False(t, featuresOnly([]byte(features+lock)), "the lock touched")
	version := "--- Cargo.toml.orig\n+++ Cargo.toml\n@@ -1 +1 @@\n-sdl3 = \"0.14\"\n+sdl3 = \"0.15\"\n"
	require.False(t, featuresOnly([]byte(version)), "another version")
}
