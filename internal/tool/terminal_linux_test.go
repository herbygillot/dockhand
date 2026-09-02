package tool

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openPTY allocates a pseudo-terminal and returns its terminal side:
// open the multiplexer, ask it which /dev/pts entry it made, unlock
// the pair, open the entry. The ioctl numbers are the stdlib's. A
// machine that will not hand out a pty skips rather than fails.
func openPTY(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty to hand out: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := master.Fd()
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Skipf("no pty to hand out: TIOCGPTN: %v", errno)
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Skipf("no pty to hand out: TIOCSPTLCK: %v", errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty to hand out: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return slave
}
