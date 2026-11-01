package screen

import "golang.org/x/sys/unix"

// The terminal driver's requests for reading and setting its modes.
const (
	getModes = unix.TCGETS
	setModes = unix.TCSETS
)
