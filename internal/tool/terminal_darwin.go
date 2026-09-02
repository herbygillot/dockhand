package tool

import (
	"syscall"
	"unsafe"
)

// isTerminal asks for the line's termios with TIOCGETA, which only a
// terminal answers. The stdlib syscall package carries the constant
// and the struct, so no new module is needed for one ioctl.
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
