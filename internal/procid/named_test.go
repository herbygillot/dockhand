package procid

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A program found by its name, from the system's own table: a copy of the
// test binary under another name, running and doing nothing else, is
// listed with its PID and start, and this process never is.
func TestAProcessIsFoundByItsProgramsName(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	child := filepath.Join(t.TempDir(), "procidnamed")
	in, err := os.Open(self)
	require.NoError(t, err)
	defer in.Close()
	out, err := os.OpenFile(child, os.O_CREATE|os.O_WRONLY, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	command := exec.Command(child, "-test.run=^TestNamedChild$")
	command.Env = append(os.Environ(), "PROCID_NAMED_CHILD=1")
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })

	var found []Process
	require.Eventually(t, func() bool {
		found, err = Named("procidnamed")
		return err == nil && len(found) == 1
	}, 10*time.Second, 50*time.Millisecond)
	start, err := Start(command.Process.Pid)
	require.NoError(t, err)
	require.Equal(t, []Process{{PID: command.Process.Pid, Start: start}}, found)

	mine, err := Self()
	require.NoError(t, err)
	all, err := Named(filepath.Base(self))
	require.NoError(t, err)
	require.False(t, slices.Contains(all, mine), "this process is never listed")
}

func TestNamedChild(t *testing.T) {
	if os.Getenv("PROCID_NAMED_CHILD") == "" {
		t.Skip("run by TestAProcessIsFoundByItsProgramsName")
	}
	time.Sleep(time.Minute)
}
