package provision

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestAgentReadinessReportsCancellationAndLastProbe(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\necho 'fixture agent unavailable' >&2\nexit 1\n")
	n := newNative(Config{Executable: script}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := n.ReadyAgent(ctx, "candidate")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "last probe")
}

func TestAgentReadinessDetectsVMExit(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\n[ \"$1\" != run ] || echo 'fixture VM exited' >&2\nexit 1\n")
	n := newNative(Config{Executable: script}, nil)
	run, err := n.vm().StartForeground("candidate")
	require.NoError(t, err)
	<-run.Done()
	n.runs["candidate"] = run
	require.ErrorContains(t, n.ReadyAgent(t.Context(), "candidate"), "fixture VM exited")
}

// Setup's guest commands go over SSH, with the command's output shown as
// it arrives and its exit status kept.
func TestProvisioningGuestTransportPreservesStreamingAndFailure(t *testing.T) {
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
	n.guests["candidate"] = &channel.Guest{Address: "guest", Image: "fixture", Keys: channel.Keys{Directory: t.TempDir()}, Executable: ssh}
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
