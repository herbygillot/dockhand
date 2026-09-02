package tool

import (
	"syscall"
	"unsafe"
)

// isTerminal asks for the line's termios with TCGETS, Linux's name for
// the same request darwin calls TIOCGETA; only a terminal answers.
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
