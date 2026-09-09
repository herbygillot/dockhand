package platform

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// physicalCores asks the kernel directly. Reached only through
// PhysicalCores' HostIsMac gate; the build tag keeps the syscall out
// of every other platform's compile.
func physicalCores() int {
	n, err := syscall.SysctlUint32("hw.physicalcpu")
	if err != nil {
		return 0
	}
	return int(n)
}

// physicalMemoryMB asks the kernel directly. Reached only through
// PhysicalMemoryMB's HostIsMac gate.
//
// IT USES x/sys AND NOT THE STDLIB, and the reason is a measured wrong
// answer rather than taste. hw.memsize is a 64-bit integer; the
// stdlib's syscall.Sysctl returns a STRING trimmed at its first NUL,
// and the little-endian bytes of any realistic memory size begin with
// zeros — 128 GB is 00 00 00 00 20 00 00 00 — so that form reads every
// large machine as empty. It returned 0 on this host the first time it
// was written that way. syscall.SysctlUint32 cannot hold the value, and
// darwin/arm64 offers neither SysctlRaw nor a usable raw Syscall.
func physicalMemoryMB() int {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return int(n / (1024 * 1024))
}
