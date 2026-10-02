package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// listingTart is a stand-in tart whose list names one local VM, and which
// records each clone, export, and import it's asked for.
func listingTart(t *testing.T, listed string) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "tart")
	testsupport.WriteExecutable(t, script, `#!/bin/sh
case "$1" in
 list) printf '[{"Name":"`+listed+`","Source":"local","State":"stopped"}]' ;;
 export) touch "$3"; echo "$*" >> `+calls+` ;;
 clone|import) echo "$*" >> `+calls+` ;;
esac
`)
	return Config{Executable: script, Home: dir}, calls
}

// Cloning or importing onto a VM Tart lists is refused, since Tart would
// replace it without a word, in each of setup's three places, and nothing
// is cloned or imported (the test plan's step 2, item 18).
func TestSetupRefusesToOverwriteAListedVM(t *testing.T) {
	config, calls := listingTart(t, "dockhand-base-tahoe-next")
	n := newNative(config, nil)
	err := n.Clone(t.Context(), "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", "dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "tart: refusing to overwrite existing VM dockhand-base-tahoe-next")
	require.NoFileExists(t, calls, "nothing cloned")

	adopting := adoptionMachine{native: n}
	err = adopting.Clone(t.Context(), "dockhand-base-tahoe", "dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "tart: refusing to overwrite existing VM dockhand-base-tahoe-next")
	require.NoFileExists(t, calls, "nothing cloned")

	t.Setenv("TART_HOME", t.TempDir())
	err = n.Import(t.Context(), "my-tahoe", "dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "tart: refusing to overwrite existing VM dockhand-base-tahoe-next")
	recorded, readErr := os.ReadFile(calls)
	require.NoError(t, readErr)
	require.True(t, strings.HasPrefix(string(recorded), "export my-tahoe "), "the person's image was exported")
	require.NotContains(t, "\n"+string(recorded), "\nimport ", "nothing imported over the VM")
}
