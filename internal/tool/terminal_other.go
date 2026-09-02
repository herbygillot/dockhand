//go:build !darwin && !linux

package tool

// isTerminal on a platform without a termios ioctl answers no. dockhand
// runs on macOS; this stub exists so the package builds anywhere Go
// does, which is the hermetic-CI rule the rest of the tree keeps.
func isTerminal(uintptr) bool { return false }
