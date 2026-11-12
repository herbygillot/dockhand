//go:build darwin

package local

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/filelock"
)

// This Mac's sudo, with real processes, as the lifecycle design asks
// before Local's Stop of a root build is written (docs/designs/
// check-lifecycle.md, "Local and Script", rule 4, and "What remains
// unproved"; #193, step 5e). DOCKHAND_TEST_SUDO_PREFIX names a prefix
// whose port-tclsh this user may run as root without a password:
//
//	<you> ALL=(root) NOPASSWD: <prefix>/bin/port-tclsh
//
// For each of a sudo without a terminal and one with a terminal, which a
// sudo set to use_pty runs its command under a pseudo-terminal of its own
// for, it starts the prefix's port-tclsh under sudo -n with the attempt's
// locked file as its standard input, as the starter will, and holds that:
//
//   - the command's standard input is the lock's file, not a pipe or a
//     terminal sudo put in its place;
//   - the lock stays held while the command lives, after the launcher's
//     own copy is closed, and is free once it has ended;
//   - a SIGTERM reaches the command: sent to the sudo by this process
//     where that's permitted, or else by the root stop, port-tclsh run
//     under sudo -n to send it, as the rule names;
//   - and sudo -n refuses a command the rule doesn't name at once, with no
//     prompt, as the root stop's refused case is answered.
//
// What it finds of the rest it says: whether this process may signal a
// root sudo, and whether the command keeps the group and session it was
// started in.
func TestLiveSudoHandsTheLockToItsCommand(t *testing.T) {
	prefix := os.Getenv("DOCKHAND_TEST_SUDO_PREFIX")
	if prefix == "" {
		t.Skip("DOCKHAND_TEST_SUDO_PREFIX names a prefix whose port-tclsh sudo runs without a password")
	}
	tclsh := filepath.Join(prefix, "bin", "port-tclsh")
	dir := t.TempDir()
	write := func(name, script string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(script), 0o644))
		return path
	}
	// The command reads its standard input the guest's one way
	// (guest.StdinIs), by fstat, never a stat of /dev/fd/0, which on
	// macOS gives devfs's device beside the file's number.
	probe := write("probe.tcl", string(guest.StdinIs)+`set out [lindex $argv 0]
set lock [lindex $argv 1]
set f [open $out.tmp w]
puts $f "[pid] [stdin_is $lock] [exec /usr/bin/stat -f %HT]"
close $f
file rename -force $out.tmp $out
after 600000
`)
	stop := write("stop.tcl", "exec /bin/kill -TERM [lindex $argv 0]\n")
	ready := exec.Command("sudo", "-n", tclsh, write("ready.tcl", "exit 0\n"))
	if out, err := ready.CombinedOutput(); err != nil {
		t.Fatalf("sudo -n %s: %v %s; the test needs the sudoers line: %s ALL=(root) NOPASSWD: %s", tclsh, err, out, os.Getenv("USER"), tclsh)
	}

	for _, terminal := range []bool{false, true} {
		name := "without a terminal"
		if terminal {
			name = "with a terminal"
		}
		t.Run(name, func(t *testing.T) {
			lockPath := filepath.Join(dir, name, "launch.lock")
			lock, err := filelock.TryAcquire(lockPath)
			require.NoError(t, err)
			out := filepath.Join(dir, name, "probe")
			sudo := exec.Command("sudo", "-n", tclsh, probe, out, lockPath)
			sudo.Stdin = lock
			sudo.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			var log strings.Builder
			sudo.Stdout, sudo.Stderr = &log, &log
			if terminal {
				ptyFile, tty := openTerminal(t)
				defer ptyFile.Close()
				defer tty.Close()
				sudo.Stdout, sudo.Stderr = tty, tty
				sudo.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1}
			}
			require.NoError(t, sudo.Start())
			ended := make(chan error, 1)
			go func() { ended <- sudo.Wait() }()
			// The launcher's own copy goes: from here only the sudo and
			// its command hold the opening.
			require.NoError(t, lock.Close())

			var said []string
			require.Eventually(t, func() bool {
				data, err := os.ReadFile(out)
				said = strings.Fields(string(data))
				return err == nil && len(said) >= 3
			}, 30*time.Second, 50*time.Millisecond, "the command's report: %s", &log)
			command, err := strconv.Atoi(said[0])
			require.NoError(t, err)
			require.Equal(t, "1", said[1], "the command's standard input is the lock's file, not a %s sudo put there", strings.Join(said[2:], " "))

			_, err = filelock.TryAcquire(lockPath)
			require.ErrorIs(t, err, filelock.ErrBusy, "the lock is held while the command lives")

			group, _ := unix.Getpgid(command)
			session, _ := unix.Getsid(command)
			t.Logf("sudo %d, group %d, session %d; its command %d, group %d, session %d", sudo.Process.Pid, sudo.Process.Pid, sudo.Process.Pid, command, group, session)
			t.Logf("the command keeps the sudo's group: %v; its session: %v", group == sudo.Process.Pid, session == sudo.Process.Pid)

			signalErr := syscall.Kill(sudo.Process.Pid, syscall.SIGTERM)
			t.Logf("this process signals the root sudo: %v", errOrPermitted(signalErr))
			if signalErr != nil {
				root := exec.Command("sudo", "-n", tclsh, stop, strconv.Itoa(sudo.Process.Pid))
				out, err := root.CombinedOutput()
				require.NoError(t, err, "the root stop: %s", out)
				t.Log("the root stop signalled the sudo")
			}
			select {
			case err := <-ended:
				t.Logf("the sudo ended: %v", err)
			case <-time.After(30 * time.Second):
				t.Fatalf("the sudo didn't end after its SIGTERM: %s", &log)
			}
			require.Eventually(t, func() bool { return !alive(command) }, 10*time.Second, 50*time.Millisecond, "the SIGTERM reached the command")
			free, err := filelock.TryAcquire(lockPath)
			require.NoError(t, err, "the lock is free once the command has ended")
			require.NoError(t, free.Close())
		})
	}

	// A command the rule doesn't name, or a user it doesn't, is refused at
	// once: sudo -n asks no one.
	refused := exec.Command("sudo", "-n", "-u", "nobody", tclsh, stop, "0")
	start := time.Now()
	out, err := refused.CombinedOutput()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit), "sudo -n -u nobody is refused: %v %s", err, out)
	require.Less(t, time.Since(start), 10*time.Second, "without waiting for a password")
	t.Logf("refused: %s", strings.TrimSpace(string(out)))
}

// openTerminal is a new pseudo-terminal: its controlling side and the
// terminal a child is given.
func openTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	ptm, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	require.NoError(t, err)
	fd := int(ptm.Fd())
	require.NoError(t, unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0))
	require.NoError(t, unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0))
	name := make([]byte, 128)
	require.NoError(t, ioctlName(fd, name))
	path := string(name[:strings.IndexByte(string(name), 0)])
	tty, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	require.NoError(t, err, "%s", path)
	// What the command writes is read and dropped, so a full terminal
	// never stops it.
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, err := ptm.Read(buffer); err != nil {
				return
			}
		}
	}()
	return ptm, tty
}

// ioctlName reads the pseudo-terminal's name (TIOCPTYGNAME).
func ioctlName(fd int, name []byte) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func errOrPermitted(err error) string {
	if err == nil {
		return "permitted"
	}
	return fmt.Sprintf("refused (%v)", err)
}
