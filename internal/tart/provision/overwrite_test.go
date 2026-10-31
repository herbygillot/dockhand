package provision

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// listingTart is a stand-in tart whose list names one local VM, and which
// records each clone it's asked for.
func listingTart(t *testing.T, listed string) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "tart")
	testsupport.WriteExecutable(t, script, `#!/bin/sh
case "$1" in
 list) printf '[{"Name":"`+listed+`","Source":"local","State":"stopped"}]' ;;
 clone) echo "$*" >> `+calls+` ;;
esac
`)
	return Config{Env: os.Environ(), Executable: script, Home: dir}, calls
}

// Cloning onto a VM Tart lists is refused, since Tart would replace it
// without a word, in each of setup's places, and nothing is cloned (the
// test plan's step 2, item 18).
func TestSetupRefusesToOverwriteAListedVM(t *testing.T) {
	t.Parallel()
	config, calls := listingTart(t, "dockhand-base-tahoe-next")
	n := newNative(config, nil)
	err := n.Clone(t.Context(), "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", "dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "tart: refusing to overwrite existing VM dockhand-base-tahoe-next")
	require.NoFileExists(t, calls, "nothing cloned")

	adopting := adoptionMachine{native: n}
	err = adopting.Clone(t.Context(), "dockhand-base-tahoe", "dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "tart: refusing to overwrite existing VM dockhand-base-tahoe-next")
	require.NoFileExists(t, calls, "nothing cloned")
}
