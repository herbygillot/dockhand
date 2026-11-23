//go:build !darwin

package local

import (
	"errors"
	"os"
)

// openTerminal is macOS's alone: the probe of a root build's stop is of
// this Mac's sudo.
func openTerminal() (*os.File, *os.File, error) {
	return nil, nil, errors.New("a pseudo-terminal for the root probe is opened on macOS alone")
}
