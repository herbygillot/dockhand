package screen

import (
	"bytes"
	"io"
	"os"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/term"
)

// pty opens a pseudo-terminal, its controlling side and the terminal a
// program reads; the test is skipped where none can be had.
func pty(t *testing.T) (control, terminal *os.File) {
	t.Helper()
	control, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { control.Close() })
	fd := int(control.Fd())
	require.NoError(t, unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0))
	require.NoError(t, unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0))
	name := make([]byte, 128)
	// x/sys has no call for TIOCPTYGNAME, which writes the name into a
	// buffer, so it's asked of the kernel directly, in this test alone.
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 { //nolint:staticcheck // no libSystem wrapper is exported for it
		t.Skipf("no pseudo-terminal's name: %v", errno)
	}
	terminal, err = os.OpenFile(string(name[:bytes.IndexByte(name, 0)]), os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { terminal.Close() })
	return control, terminal
}

// On a terminal, the screen is raw while it's open, and while it's
// suspended the terminal is as it was, as it is again once it closes.
func TestRawModeIsEnteredAndGivenBack(t *testing.T) {
	t.Parallel()
	control, terminal := pty(t)
	go func() { _, _ = io.Copy(io.Discard, control) }()
	fd := int(terminal.Fd())
	before, err := unix.IoctlGetTermios(fd, getModes)
	require.NoError(t, err)
	require.NotZero(t, before.Lflag&unix.ICANON)

	s, err := Open(term.Streams{In: terminal, Out: terminal})
	require.NoError(t, err)
	opened, err := unix.IoctlGetTermios(fd, getModes)
	require.NoError(t, err)
	require.Zero(t, opened.Lflag&(unix.ICANON|unix.ECHO|unix.ISIG), "raw: read a byte at a time, unechoed, Ctrl-C a key")

	_, err = control.WriteString("\x1b[5~")
	require.NoError(t, err)
	require.Equal(t, Key{Code: PageUp}, <-s.Keys())

	var during *unix.Termios
	require.NoError(t, s.Suspend(t.Context(), func() { during, _ = unix.IoctlGetTermios(fd, getModes) }, ""))
	require.Equal(t, before.Lflag, during.Lflag, "a command runs on the terminal as it was")
	again, err := unix.IoctlGetTermios(fd, getModes)
	require.NoError(t, err)
	require.Zero(t, again.Lflag&unix.ICANON, "and the screen is raw again after it")

	require.NoError(t, s.Close())
	after, err := unix.IoctlGetTermios(fd, getModes)
	require.NoError(t, err)
	require.Equal(t, before, after, "the terminal is given back as it was")
}
