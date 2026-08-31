package platform

import "syscall"

// PhysicalCores is the host's physical core count, zero when the host
// will not say. It lives here because it is a fact about the machine
// dockhand is running on — the same kind of fact the release table
// answers about macOS — and every consumer that sizes work to the
// host (VM provisioning, evaluator pools) should measure through one
// door rather than each asking sysctl its own way.
func PhysicalCores() int {
	n, err := syscall.SysctlUint32("hw.physicalcpu")
	if err != nil {
		return 0
	}
	return int(n)
}
