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

// PhysicalMemoryMB is the host's installed memory in megabytes, zero
// when the host will not say — including any host that is not macOS.
//
// It sits beside PhysicalCores for that function's own reason: every
// consumer that sizes work to the host should measure through one door.
// It exists at all because a sizing rule was deriving MEMORY FROM CORES
// — "2 GB per granted core", from "the one measured fact" — and a ratio
// is not a measurement. On a 128 GB host that granted a nine-core guest
// 18 GB and left 110 GB idle, and the guest died in a parallel C++
// compile.
func PhysicalMemoryMB() int {
	if !HostIsMac() {
		return 0
	}
	return physicalMemoryMB()
}

// HostArch is this machine's architecture in base's own os.arch
// vocabulary — the hardware family, "arm" or "i386", and not the ABI
// name Go uses.
//
// It exists for the one caller that builds an evaluation frame for a
// VERIFICATION ENVIRONMENT rather than for a hypothetical: a tart guest
// runs the architecture of the Mac hosting it, so the frame a preflight
// evaluates against is this machine's, not a choice.
//
// It was a literal "arm" at that call site, which cost nothing while the
// frame did not simulate architecture at all and costs something now
// that it does: on an Intel Mac the preflight would have decided
// known_fail and use_xcode for a guest that does not exist.
func HostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm"
	}
	return "i386"
}
