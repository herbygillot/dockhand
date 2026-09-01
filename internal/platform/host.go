package platform

import "runtime"

// HostIsMac reports whether dockhand is running on macOS. Every probe
// of the host machine — core counts, and whatever sw_vers-shaped
// probe comes later — goes through this gate before asking, so that
// none of them ends up running a macOS-only tool on Linux or
// elsewhere. The build tags on the probe implementations already keep
// the binary compiling everywhere; this makes the runtime refusal a
// stated contract rather than a side effect of file naming.
func HostIsMac() bool {
	return runtime.GOOS == "darwin"
}

// PhysicalCores is the host's physical core count, zero when the host
// will not say — including any host that is not macOS at all. It
// lives here because it is a fact about the machine dockhand is
// running on — the same kind of fact the release table answers about
// macOS — and every consumer that sizes work to the host (VM
// provisioning, evaluator pools) should measure through one door
// rather than each asking sysctl its own way.
func PhysicalCores() int {
	if !HostIsMac() {
		return 0
	}
	return physicalCores()
}
