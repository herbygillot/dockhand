//go:build !linux && !darwin

package command

// isTerminal is false where dockhand cannot ask the terminal driver, so
// commands there never prompt.
func isTerminal(uintptr) bool { return false }
