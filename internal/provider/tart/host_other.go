//go:build !darwin

package tart

import "errors"

// hostDarwin has no answer off a Mac: Tart runs macOS guests on macOS only.
func hostDarwin() (int, error) {
	return 0, errors.New("Tart builds on a Mac; this isn't one")
}
