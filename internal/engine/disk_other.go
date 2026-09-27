//go:build !darwin && !linux

package engine

// freeSpace can't be read here, and cleanup waits for its day.
func freeSpace(string) (uint64, bool) { return 0, false }
