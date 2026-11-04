package provision

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tart/guestssh"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// Setup's readiness probe asks the guest over SSH, as a check does: a
// guest whose sshd doesn't answer yet is waited for until the bound, one
// that answers is ready, and one whose VM exited stops the wait; one not
// reached over SSH is refused.
func TestReadinessAsksTheGuestOverSSH(t *testing.T) {
	t.Parallel()
	answers := filepath.Join(t.TempDir(), "answers")
	ssh := filepath.Join(t.TempDir(), "ssh")
	testsupport.WriteExecutable(t, ssh, "#!/bin/sh\nif [ -f "+answers+" ]; then exit 0; fi\necho 'ssh: connect to host guest port 22: Connection refused' >&2\nexit 255\n")
	n := newNative(Config{}, nil)
	n.guests["candidate"] = &guestssh.Guest{Env: os.Environ(), Address: "guest", Image: "fixture", Keys: guestssh.Keys{Directory: t.TempDir()}, Executable: ssh}
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	err := n.ReadyAgent(ctx, "candidate")
	require.ErrorContains(t, err, "setup: candidate didn't answer over SSH")
	require.NoError(t, os.WriteFile(answers, nil, 0o644))
	require.NoError(t, n.ReadyAgent(t.Context(), "candidate"))
	require.ErrorContains(t, n.ReadyAgent(t.Context(), "unreached"), "unreached has not been reached over SSH")

	require.NoError(t, os.Remove(answers))
	script := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\n[ \"$1\" != run ] || echo 'fixture VM exited' >&2\nexit 1\n")
	exited := newNative(Config{Env: os.Environ(), Executable: script}, nil)
	run, err := exited.vm().StartForeground("candidate")
	require.NoError(t, err)
	<-run.Done()
	exited.runs["candidate"], exited.guests["candidate"] = run, n.guests["candidate"]
	require.ErrorContains(t, exited.ReadyAgent(t.Context(), "candidate"), "the VM stopped before it accepted SSH")
}

// Setup's guest commands go over SSH, with the command's output shown as
// it arrives and its exit status kept.
func TestProvisioningGuestTransportPreservesStreamingAndFailure(t *testing.T) {
	t.Parallel()
	ssh := filepath.Join(t.TempDir(), "ssh")
	testsupport.WriteExecutable(t, ssh, `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in -F|-o|-O) shift 2 ;; *) break ;; esac
done
shift
exec /bin/sh -c "$1"
`)
	var progress bytes.Buffer
	n := newNative(Config{}, &progress)
	n.guests["candidate"] = &guestssh.Guest{Env: os.Environ(), Address: "guest", Image: "fixture", Keys: guestssh.Keys{Directory: t.TempDir()}, Executable: ssh}
	output, err := n.guestStream(t.Context(), "candidate", strings.NewReader("payload\n"), "/bin/sh", "-c", `
cat
printf 'diagnostic\n' >&2
exit 7
`)
	require.ErrorContains(t, err, "exit status 7")
	require.Equal(t, "payload\ndiagnostic\n", string(output))
	require.Contains(t, progress.String(), "payload")
	require.Contains(t, progress.String(), "diagnostic")
	_, err = n.guest(t.Context(), "unreached", nil, "/usr/bin/true")
	require.ErrorContains(t, err, "unreached has not been reached over SSH")
}

// Configure gives an ASIF disk, Golden Gate's, more room than a raw one and
// leaves it to Tart's own resize, which moves the recovery partition; it
// edits nothing on the host. A disk it doesn't know is refused before
// anything is set.
func TestConfigureSizesAnASIFDiskAndEditsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "tart")
	format := filepath.Join(dir, "format")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\necho \"$*\" >> "+calls+"\nif [ \"$1\" = get ]; then printf '{\"Running\":false,\"State\":\"stopped\",\"DiskFormat\":\"%s\"}' \"$(cat "+format+")\"; fi\n")
	require.NoError(t, os.WriteFile(format, []byte("asif"), 0o644))
	n := newNative(Config{Env: os.Environ(), Executable: script, Home: dir}, nil)
	require.NoError(t, n.Configure(t.Context(), "dockhand-base-golden-gate-next"))
	recorded, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Contains(t, string(recorded), "--disk-size 125\n")
	require.NoFileExists(t, filepath.Join(dir, "vms", "dockhand-base-golden-gate-next", "disk.img"), "nothing on the host is touched")

	require.NoError(t, os.WriteFile(format, []byte("qcow"), 0o644))
	require.NoError(t, os.Remove(calls))
	require.ErrorContains(t, n.Configure(t.Context(), "odd-next"), "has a qcow disk, which dockhand doesn't prepare")
	recorded, err = os.ReadFile(calls)
	require.NoError(t, err)
	require.NotContains(t, string(recorded), "set ", "nothing is set on a disk it refuses")
}

// A listing an ASIF VM blocks is waited on, said once, for listingBound in
// all, then given up with what to do, where it held setup as long as the
// VM ran (the limits sweep, batch 26).
func TestABlockedListingIsWaitedOnlySoLong(t *testing.T) {
	t.Parallel()
	retry, bound := listingRetry, listingBound
	listingRetry, listingBound = time.Millisecond, 3*time.Millisecond
	t.Cleanup(func() { listingRetry, listingBound = retry, bound })
	var said bytes.Buffer
	n := newNative(Config{}, &said)
	for range 3 {
		require.NoError(t, n.waitListing(t.Context()))
	}
	err := n.waitListing(t.Context())
	require.ErrorContains(t, err, "for 3ms: a running VM with an ASIF disk keeps Tart from listing its VMs (openai/tart#1344); stop it, and run setup again")
	require.Equal(t, 1, strings.Count(said.String(), "waiting for it to stop"))
}
