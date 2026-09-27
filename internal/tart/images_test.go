package tart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	require.Equal(t, []Image{
		{Name: "dockhand-base-tahoe", Source: "local", Accessed: time.Date(2026, 9, 24, 16, 58, 34, 0, time.UTC)},
		{Name: "scratch", Source: "local", Running: true, Accessed: time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC)},
	}, images)
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

// Golden Gate's ASIF images need a Tart that lists its VMs while one runs.
func TestHandlesASIFFromTheVersionTartPrints(t *testing.T) {
	for version, handles := range map[string]bool{
		"2.39.0": true, "2.39.0\n": true, "2.39.1": true, "2.40.0": true, "3.0.0": true, "2.39.0-3-g27d3e2c": true,
		"2.38.0": false, "2.37.0": false, "1.99.9": false, "2.39": false, "": false, "unknown": false,
	} {
		require.Equal(t, handles, HandlesASIF(version), version)
	}
}

// Tart 2.39.0 lists a running ASIF VM, Golden Gate's, with its disk
// capacity null, and describes it with its format; neither is refused.
func TestListingReadsTart239WhileAnASIFVMRuns(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
case "$*" in
 "list --format json") printf '%s' '[{"State":"running","Disk":null,"Accessed":"2026-09-27T05:54:21Z","Size":35,"Name":"gg","Source":"local","Running":true}]' ;;
 "get gg --format json") printf '%s' '{"Display":"1024x768","Memory":8192,"State":"running","OS":"darwin","Running":true,"Size":"35.805","CPU":4,"Disk":null,"DiskFormat":"asif"}' ;;
 *) exit 9 ;;
esac
`)
	client := Client{Executable: executable}
	images, err := client.Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "gg", Source: "local", Running: true, Accessed: time.Date(2026, 9, 27, 5, 54, 21, 0, time.UTC)}}, images, "with when Tart last opened it")
	vm, err := client.Get(t.Context(), RunOptions{}, "gg")
	require.NoError(t, err)
	require.Equal(t, VM{Running: true, DiskFormat: "asif"}, vm)
}

// A listing that raced a delete is listed again, and answers once the
// delete is done; one that never answers says why.
func TestARacedListingIsListedAgain(t *testing.T) {
	pause := listingPause
	t.Cleanup(func() { listingPause = pause })
	listingPause = time.Millisecond
	dir := t.TempDir()
	executable, count, races := filepath.Join(dir, "tart"), filepath.Join(dir, "count"), filepath.Join(dir, "races")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
echo x >> `+count+`
if [ "$(wc -l < `+count+`)" -le "$(cat `+races+`)" ]; then
  echo 'Error: The file “config.json” couldn’t be opened because there is no such file.' >&2; exit 1
fi
printf '%s' '[{"Name":"dockhand-base-tahoe","Source":"local","Running":false}]'
`)
	require.NoError(t, os.WriteFile(races, []byte("2"), 0o644))
	client := Client{Executable: executable}
	images, err := client.Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "dockhand-base-tahoe", Source: "local"}}, images)
	calls, err := os.ReadFile(count)
	require.NoError(t, err)
	require.Equal(t, 3, strings.Count(string(calls), "x"), "two raced listings, then the answer")

	require.NoError(t, os.Remove(count))
	require.NoError(t, os.WriteFile(races, []byte("99"), 0o644))
	_, err = client.Images(t.Context(), RunOptions{})
	require.ErrorIs(t, err, ErrListingRaced)
	calls, err = os.ReadFile(count)
	require.NoError(t, err)
	require.Equal(t, listingAttempts, strings.Count(string(calls), "x"))
}
