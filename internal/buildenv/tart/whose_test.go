package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/guestssh"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// Slot counting reads dockhand's running VMs from its own Tart home and
// counts the person's from theirs, read only: none where they have no
// home or it's dockhand's, and one where a running ASIF VM blocks Tart's
// listing of theirs, where no test reached it (the test analysis's step
// b).
func TestSlotCountingCountsThePersonsRunningVMs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	own, personal := filepath.Join(dir, "own"), filepath.Join(dir, "personal")
	for _, home := range []string{own, filepath.Join(personal, "vms")} {
		require.NoError(t, os.MkdirAll(home, 0o755))
	}
	fake := filepath.Join(dir, "tart")
	testsupport.WriteExecutable(t, fake, `#!/bin/sh
case "$TART_HOME" in
*/own) echo '[{"Name":"dockhand-check-1","Source":"local","Running":true},{"Name":"dockhand-base","Source":"local","Running":false}]' ;;
*)
	if [ -f "$TART_HOME/blocked" ]; then echo 'Error: image info: Resource temporarily unavailable' >&2; exit 1; fi
	echo '[{"Name":"mine","Source":"local","Running":true},{"Name":"also-mine","Source":"local","State":"running"},{"Name":"off","Source":"local","Running":false}]' ;;
esac
`)
	client := tartvm.Client{Executable: fake, Home: own, Env: os.Environ()}
	ours, theirs, err := newNative(client, guestssh.Keys{}, personal).Whose(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"dockhand-check-1"}, ours)
	require.Equal(t, 2, theirs)

	require.NoError(t, os.WriteFile(filepath.Join(personal, "blocked"), nil, 0o644))
	_, theirs, err = newNative(client, guestssh.Keys{}, personal).Whose(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, theirs, "a listing a running ASIF VM blocks is that one VM")

	_, theirs, err = newNative(client, guestssh.Keys{}, own).Whose(t.Context())
	require.NoError(t, err)
	require.Zero(t, theirs, "the person's home is dockhand's")
	_, theirs, err = newNative(client, guestssh.Keys{}, filepath.Join(dir, "none")).Whose(t.Context())
	require.NoError(t, err)
	require.Zero(t, theirs, "no home of their own")
}

// RemoveLeftover refuses any name but a check clone's, so no image a check
// clones from is ever touched.
func TestRemoveLeftoverRefusesAnythingButACheckClone(t *testing.T) {
	t.Parallel()
	mac := &fakeMac{images: []string{"dockhand-base-tahoe"}}
	err := testProvider(mac).RemoveLeftover(t.Context(), "dockhand-base-tahoe")
	require.EqualError(t, err, "dockhand-base-tahoe is not a temporary build VM; only temporary build VMs can be removed")
}
