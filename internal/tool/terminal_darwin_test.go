package tool

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openPTY allocates a pseudo-terminal and returns its terminal side,
// the one that answers TIOCGETA. The master side, /dev/ptmx, does not
// on darwin, which is why the test has to go the whole way: grant,
// unlock, ask the master for the slave's name, and open that. The
// ioctl numbers are the stdlib's. A machine that will not hand out a
// pty skips rather than fails.
func openPTY(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty to hand out: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := master.Fd()
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCPTYGRANT, 0); errno != 0 {
		t.Skipf("no pty to hand out: TIOCPTYGRANT: %v", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCPTYUNLK, 0); errno != 0 {
		t.Skipf("no pty to hand out: TIOCPTYUNLK: %v", errno)
	}
	// TIOCPTYGNAME writes up to 128 bytes, its own encoded size.
	var name [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Skipf("no pty to hand out: TIOCPTYGNAME: %v", errno)
	}
	end := bytes.IndexByte(name[:], 0)
	if end < 0 {
		end = len(name)
	}
	slave, err := os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty to hand out: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return slave
}
