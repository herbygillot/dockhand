package tart

import (
	"os"
	"path/filepath"
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
	images, err := (Client{Env: os.Environ(), Executable: executable}).Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "one", Source: "local", Running: true}, {Name: "two", Source: "local", Running: true}, {Name: "base", Source: "oci"}}, images)
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\necho invalid\n")
	_, err = (Client{Env: os.Environ(), Executable: executable}).Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "invalid image listing")
}

// A Tart home that isn't there has no images, and listing them doesn't
// run Tart, which would make the home (the M1 shakedown, 2026-10-03).
func TestAMissingHomeHasNoImagesAndIsNotMade(t *testing.T) {
	home := filepath.Join(t.TempDir(), "tart")
	images, err := (Client{Env: os.Environ(), Executable: filepath.Join(t.TempDir(), "absent"), Home: home}).Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Empty(t, images)
	require.NoDirExists(t, home)
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
	client := Client{Env: os.Environ(), Executable: executable}
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
	require.ErrorContains(t, err, "expected DiskFormat")

	testsupport.WriteExecutable(t, executable, `#!/bin/sh
printf '%s' '[{"VM":"renamed","Source":"local","State":"stopped"}]'
`)
	_, err = client.Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "each entry must include Name, Source, and either Running or State")
}

// The failures dockhand acts on are named from Tart's own words: the
// listing a running ASIF VM blocks (openai/tart#1344), a missing VM, a
// delete's included since Tart says it truly from 2.40.0 (openai/tart#1345,
// the oldest Tart taken being newer), and a stop of a stopped one.
func TestTartFailuresAreClassifiedByWhatTartSays(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
case "$1" in
 list) echo '"image info --plist /Users/u/.tart/vms/gg/disk.img" failed with exit code 1: Error: Failed to retrieve info for disk image: The operation couldn’t be completed. Resource temporarily unavailable' >&2; exit 1 ;;
 get|delete) echo "the specified VM \"$2\" does not exist" >&2; exit 2 ;;
 stop) echo "VM \"$2\" is not running" >&2; exit 2 ;;
esac
`)
	client := Client{Env: os.Environ(), Executable: executable}
	_, err := client.Images(t.Context(), RunOptions{})
	require.ErrorIs(t, err, ErrListingBlocked)
	require.ErrorContains(t, err, "openai/tart#1344")
	_, err = client.Get(t.Context(), RunOptions{}, "gone")
	require.ErrorIs(t, err, ErrVMMissing)
	_, err = client.Run(t.Context(), RunOptions{}, "stop", "idle")
	require.ErrorIs(t, err, ErrVMStopped)
	_, err = client.Run(t.Context(), RunOptions{Combined: true}, "stop", "idle")
	require.ErrorIs(t, err, ErrVMStopped, "setup runs Tart with combined output")
	_, err = client.Run(t.Context(), RunOptions{}, "delete", "gone")
	require.ErrorIs(t, err, ErrVMMissing)
}

// The oldest Tart taken is 2.41.1, read from the version Tart prints (#38).
func TestTheTartFloorIsReadFromTheVersionTartPrints(t *testing.T) {
	for version, enough := range map[string]bool{
		"2.41.1": true, "2.41.1\n": true, "2.41.2": true, "2.42.0": true, "3.0.0": true, "2.41.1-3-g27d3e2c": true,
		"2.41.0": false, "2.40.1": false, "2.39.0": false, "1.99.9": false, "2.41": false, "": false, "unknown": false,
	} {
		require.Equal(t, enough, NewEnough(version), version)
		require.Equal(t, enough, TooOld(version) == nil, version)
	}
	// One refusal, setup's and a check's alike (the architecture review of
	// 5897a82, R3-18).
	require.EqualError(t, TooOld("2.40.1\n"), "Tart 2.40.1 is installed; Dockhand requires 2.41.1 or later, which lists running VMs with ASIF disks, says when a delete left a VM, and lists VMs while one is deleted. Upgrade with sudo port upgrade tart")
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
	client := Client{Env: os.Environ(), Executable: executable}
	images, err := client.Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "gg", Source: "local", Running: true, Accessed: time.Date(2026, 9, 27, 5, 54, 21, 0, time.UTC)}}, images, "with when Tart last opened it")
	vm, err := client.Get(t.Context(), RunOptions{}, "gg")
	require.NoError(t, err)
	require.Equal(t, VM{Running: true, DiskFormat: "asif"}, vm)
}

// A listing Tart doesn't answer in time is said, where status waited on it
// for 22 minutes behind a disk diskutil couldn't answer for (the rc8 full
// stage).
func TestAListingTartDoesntAnswerIsBounded(t *testing.T) {
	wait := listingWait
	t.Cleanup(func() { listingWait = wait })
	listingWait = 200 * time.Millisecond
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\nexec sleep 30\n")
	start := time.Now()
	_, err := Client{Env: os.Environ(), Executable: executable}.Images(t.Context(), RunOptions{})
	require.ErrorIs(t, err, ErrListingTimedOut)
	require.ErrorContains(t, err, "timed out after 200ms; VM status is unknown")
	require.Less(t, time.Since(start), 20*time.Second, "not the sleep's thirty")
}
