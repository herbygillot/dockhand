package tart

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestImagesNormalizesBothRunningRepresentations(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
[ "$*" = "list --format json" ] || exit 2
printf '%s' '[{"Name":"one","Source":"local","Running":true},{"Name":"two","Source":"local","State":"running"},{"Name":"base","Source":"oci","State":"stopped"}]'
`)
	images, err := (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "one", Source: "local", Running: true}, {Name: "two", Source: "local", Running: true}, {Name: "base", Source: "oci"}}, images)
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\necho invalid\n")
	_, err = (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "invalid image listing")
}

// The shapes Tart 2.37.0 prints, captured 2026-09-24. An entry without the
// fields dockhand reads is an error, not a VM that silently does not run.
func TestListingAndDescriptionReadTart237Output(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
case "$*" in
 "list --format json") printf '%s' '[{"Running":false,"State":"stopped","Size":28,"Name":"dockhand-base-tahoe","Source":"local","Disk":100,"Accessed":"2026-09-24T16:58:34Z"},{"Running":true,"State":"running","Size":23,"Name":"scratch","Source":"local","Disk":100,"Accessed":"2026-09-24T17:00:00Z"}]' ;;
 "get scratch --format json") printf '%s' '{"Size":"23.162","CPU":4,"Display":"1024x768","Running":true,"DiskFormat":"raw","State":"running","Disk":100,"Memory":8192,"OS":"darwin"}' ;;
 "get changed --format json") printf '%s' '{"State":"running","Format":"raw"}' ;;
 "list --source local --format json") printf '%s' '[{"VM":"renamed","Source":"local","State":"stopped"}]' ;;
 *) exit 9 ;;
esac
`)
	client := Client{Executable: executable}
	images, err := client.Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "dockhand-base-tahoe", Source: "local"}, {Name: "scratch", Source: "local", Running: true}}, images)
	vm, err := client.Get(t.Context(), RunOptions{}, "scratch")
	require.NoError(t, err)
	require.Equal(t, VM{Running: true, DiskFormat: "raw"}, vm)
	_, err = client.Get(t.Context(), RunOptions{}, "changed")
	require.ErrorContains(t, err, "lacks DiskFormat")

	testsupport.WriteExecutable(t, executable, `#!/bin/sh
printf '%s' '[{"VM":"renamed","Source":"local","State":"stopped"}]'
`)
	_, err = client.Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "lacks Name, Source, or Running and State")
}

// The failures dockhand acts on are named from Tart 2.37.0's own words: the
// listing a running ASIF VM blocks (openai/tart#1344), a missing VM, and a
// stop of a stopped one. A delete's "does not exist" is left unnamed, since
// Tart says it of a running VM it leaves in place (openai/tart#1345).
func TestTartFailuresAreClassifiedByWhatTartSays(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
case "$1" in
 list) echo '"image info --plist /Users/u/.tart/vms/gg/disk.img" failed with exit code 1: Error: Failed to retrieve info for disk image: The operation couldn’t be completed. Resource temporarily unavailable' >&2; exit 1 ;;
 get|delete) echo "the specified VM \"$2\" does not exist" >&2; exit 2 ;;
 stop) echo "VM \"$2\" is not running" >&2; exit 2 ;;
esac
`)
	client := Client{Executable: executable}
	_, err := client.Images(t.Context(), RunOptions{})
	require.ErrorIs(t, err, ErrListingBlocked)
	require.ErrorContains(t, err, "openai/tart#1344")
	_, err = client.Get(t.Context(), RunOptions{}, "gone")
	require.ErrorIs(t, err, ErrVMMissing)
	_, err = client.Run(t.Context(), RunOptions{}, "stop", "idle")
	require.ErrorIs(t, err, ErrVMStopped)
	_, err = client.Run(t.Context(), RunOptions{Combined: true}, "stop", "idle")
	require.ErrorIs(t, err, ErrVMStopped, "setup runs Tart with combined output")
	_, err = client.Run(t.Context(), RunOptions{}, "delete", "running")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrVMMissing))
}
