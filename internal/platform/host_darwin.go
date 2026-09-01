package platform

import "syscall"

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
