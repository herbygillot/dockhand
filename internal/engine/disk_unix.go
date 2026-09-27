//go:build darwin || linux

package engine

import "syscall"

// freeSpace is the space free to this user on the volume a path is on.
func freeSpace(path string) (uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, false
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), true
}
