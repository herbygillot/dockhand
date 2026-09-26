package tart

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// hostDarwin is this Mac's Darwin major version, from the kernel.
func hostDarwin() (int, error) {
	release, err := syscall.Sysctl("kern.osrelease")
	if err != nil {
		return 0, fmt.Errorf("reading this Mac's macOS release: %w", err)
	}
	major, _, _ := strings.Cut(release, ".")
	return strconv.Atoi(major)
}
