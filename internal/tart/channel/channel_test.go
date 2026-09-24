package channel

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// fakeGuest is a Guest whose ssh runs the command on this machine, after
// ssh's options and the address, without sudo, so transfers and scripts
// run against local paths. FAKE_DAMAGE names a file holding how many
// more reads by cat to damage by one extra byte; the address unreachable
// fails as ssh does, with status 255.
func fakeGuest(t *testing.T) *Guest {
	t.Helper()
	for _, tool := range []string{"/usr/bin/openssl", "/usr/bin/stat", "/usr/bin/cut", "/usr/bin/head", "/usr/bin/tail"} {
		if _, err := os.Stat(tool); err != nil {
			t.Skipf("%s is required", tool)
		}
	}
	directory := t.TempDir()
	ssh := filepath.Join(directory, "ssh")
	testsupport.WriteExecutable(t, ssh, `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -F|-o|-O) shift 2 ;;
    *) break ;;
  esac
done
address=$1
shift
if [ "$address" = unreachable ]; then echo "ssh: connect to host unreachable port 22: No route to host" >&2; exit 255; fi
command=$(printf '%s' "$1" | sed "s#^'/usr/bin/sudo' '-n' ##")
case "$command" in
  "'/bin/cat' "*)
    if [ -n "$FAKE_DAMAGE" ] && [ "$(cat "$FAKE_DAMAGE")" -gt 0 ]; then
      echo $(( $(cat "$FAKE_DAMAGE") - 1 )) > "$FAKE_DAMAGE"
      /bin/sh -c "$command"
      printf x
      exit 0
    fi ;;
esac
exec /bin/sh -c "$command"
`)
	return &Guest{Address: "guest", Image: "dockhand-base-fixture", Keys: Keys{Directory: filepath.Join(directory, "keys")}, Executable: ssh}
}

func TestCommandsQuoteTheirArgumentsForTheGuestShell(t *testing.T) {
	g := fakeGuest(t)
	for _, arg := range []string{"plain", "two words", "it's", `$HOME`, "`date`", "a\nb", ""} {
		output, err := g.Command(t.Context(), nil, "/usr/bin/printf", "%s", arg)
		require.NoError(t, err)
		require.Equal(t, arg, string(output))
	}
	output, err := g.Script(t.Context(), strings.NewReader("input"), `cat; printf ' %s' "$1"; exit 7`, "argument one")
	require.Equal(t, "input argument one", string(output))
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	require.False(t, errors.Is(err, ErrTransport), "a command's own failure is not the connection's")
}

func TestTransfersArriveWholeAndAreChecked(t *testing.T) {
	g := fakeGuest(t)
	root := t.TempDir()
	local := filepath.Join(root, "local.bin")
	payload := bytes.Repeat([]byte("dockhand\x00\xff"), 100000)
	require.NoError(t, os.WriteFile(local, payload, 0600))
	remote := filepath.Join(root, "guest.bin")
	require.NoError(t, g.Upload(t.Context(), local, remote, true))
	sent, err := os.ReadFile(remote)
	require.NoError(t, err)
	require.Equal(t, payload, sent)

	back := filepath.Join(root, "back.bin")
	require.NoError(t, g.Download(t.Context(), remote, back, false))
	received, err := os.ReadFile(back)
	require.NoError(t, err)
	require.Equal(t, payload, received)
	small, err := g.Read(t.Context(), remote, false)
	require.NoError(t, err)
	require.Equal(t, payload, small)

	chunk, err := g.Range(t.Context(), remote, 5, 20, false)
	require.NoError(t, err)
	require.Equal(t, payload[5:25], chunk)
	tail, err := g.Range(t.Context(), remote, int64(len(payload))-3, 20, false)
	require.NoError(t, err)
	require.Equal(t, payload[len(payload)-3:], tail)
	past, err := g.Range(t.Context(), remote, int64(len(payload)), 20, false)
	require.NoError(t, err)
	require.Empty(t, past)
}

// A read damaged in transit is tried again, and one that keeps arriving
// damaged is reported rather than kept.
func TestDamagedReadsAreRetriedThenRefused(t *testing.T) {
	g := fakeGuest(t)
	root := t.TempDir()
	remote := filepath.Join(root, "guest.txt")
	require.NoError(t, os.WriteFile(remote, []byte("result"), 0600))
	damage := filepath.Join(root, "damage")
	t.Setenv("FAKE_DAMAGE", damage)

	require.NoError(t, os.WriteFile(damage, []byte("1"), 0600))
	data, err := g.Read(t.Context(), remote, false)
	require.NoError(t, err)
	require.Equal(t, "result", string(data))

	require.NoError(t, os.WriteFile(damage, []byte("9"), 0600))
	_, err = g.Read(t.Context(), remote, false)
	require.ErrorIs(t, err, ErrTransfer)
	local := filepath.Join(root, "local.txt")
	require.NoError(t, os.WriteFile(damage, []byte("9"), 0600))
	require.ErrorIs(t, g.Download(t.Context(), remote, local, false), ErrTransfer)
	require.NoFileExists(t, local, "a damaged download leaves nothing behind")
}

func TestMissingFilesAndLostConnectionsAreNamed(t *testing.T) {
	g := fakeGuest(t)
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := g.Read(t.Context(), missing, false)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorIs(t, g.Download(t.Context(), missing, filepath.Join(t.TempDir(), "local"), false), os.ErrNotExist)
	_, err = g.Range(t.Context(), missing, 0, 10, false)
	require.ErrorIs(t, err, os.ErrNotExist)

	g.Address = "unreachable"
	_, err = g.Command(t.Context(), nil, "/usr/bin/true")
	require.ErrorIs(t, err, ErrTransport)
	var exit *exec.ExitError
	require.False(t, errors.As(err, &exit), "ssh's own status is not the command's")
	_, err = g.Read(t.Context(), missing, false)
	require.ErrorIs(t, err, ErrTransport)
}

// ssh reads none of the person's configuration, holds the guest to the
// image's recorded host keys, and uses dockhand's key over a shared
// connection, except at bootstrap, where the image's password is given
// through SSH_ASKPASS and the presented host keys are recorded.
func TestInvocationUsesTheImagesHostKeysAndTheRightCredential(t *testing.T) {
	g := &Guest{Address: "192.168.64.9", Image: "dockhand-base-tahoe", Keys: Keys{Directory: t.TempDir()}}
	args, _, cleanup, err := g.invocation()
	require.NoError(t, err)
	cleanup()
	joined := strings.Join(args, " ")
	require.Contains(t, joined, "-F /dev/null")
	require.Contains(t, joined, "HostKeyAlias=dockhand-base-tahoe")
	require.Contains(t, joined, "UserKnownHostsFile="+g.Keys.HostKeys("dockhand-base-tahoe"))
	require.Contains(t, joined, "StrictHostKeyChecking=yes")
	require.Contains(t, joined, "IdentityFile="+g.Keys.Private())
	require.Contains(t, joined, "BatchMode=yes")
	require.Contains(t, joined, "ControlMaster=auto")

	g.Bootstrap = true
	args, environment, cleanup, err := g.invocation()
	require.NoError(t, err)
	joined = strings.Join(args, " ")
	require.Contains(t, joined, "StrictHostKeyChecking=accept-new")
	require.Contains(t, joined, "PubkeyAuthentication=no")
	require.NotContains(t, joined, "BatchMode=yes")
	var askpass string
	for _, value := range environment {
		if program, ok := strings.CutPrefix(value, "SSH_ASKPASS="); ok {
			askpass = program
		}
	}
	require.Contains(t, environment, "SSH_ASKPASS_REQUIRE=force")
	output, err := exec.Command(askpass).Output()
	require.NoError(t, err)
	require.Equal(t, Password+"\n", string(output))
	cleanup()
	require.NoFileExists(t, askpass, "the password helper does not outlive the call")
}

func TestKeysAreMadeOnceAndHostKeysRecordedUnderTheImage(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ssh-keygen"); err != nil {
		t.Skip("/usr/bin/ssh-keygen is required")
	}
	keys := Keys{Directory: filepath.Join(t.TempDir(), "ssh")}
	first, err := keys.Public(t.Context())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(first, "ssh-ed25519 "))
	again, err := keys.Public(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, again)
	info, err := os.Stat(keys.Private())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	require.NoError(t, os.MkdirAll(filepath.Dir(keys.HostKeys("x")), 0700))
	require.NoError(t, os.WriteFile(keys.HostKeys("dockhand-base-tahoe-next"), []byte("dockhand-base-tahoe-next ssh-ed25519 AAAAfixture\ndockhand-base-tahoe-next ecdsa-sha2-nistp256 AAAAother\n"), 0600))
	require.NoError(t, keys.Record("dockhand-base-tahoe-next", "dockhand-base-tahoe"))
	recorded, err := os.ReadFile(keys.HostKeys("dockhand-base-tahoe"))
	require.NoError(t, err)
	require.Equal(t, "dockhand-base-tahoe ssh-ed25519 AAAAfixture\ndockhand-base-tahoe ecdsa-sha2-nistp256 AAAAother\n", string(recorded))
	require.NoError(t, keys.Forget("dockhand-base-tahoe"))
	require.NoFileExists(t, keys.HostKeys("dockhand-base-tahoe"))
	require.NoError(t, keys.Forget("dockhand-base-tahoe"), "forgetting nothing is not an error")
}
