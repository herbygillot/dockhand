//go:build darwin

package local

import (
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openTerminal is a new pseudo-terminal: its controlling side, read and
// dropped so a full terminal never stops its writer, and the terminal a
// child is given.
func openTerminal() (*os.File, *os.File, error) {
	ptm, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(ptm.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		ptm.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		ptm.Close()
		return nil, nil, err
	}
	name := make([]byte, 128)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		ptm.Close()
		return nil, nil, errno
	}
	path := string(name[:strings.IndexByte(string(name), 0)])
	tty, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptm.Close()
		return nil, nil, err
	}
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, err := ptm.Read(buffer); err != nil {
				return
			}
		}
	}()
	return ptm, tty, nil
}
