package provision

import "github.com/herbygillot/dockhand/internal/platform"

// SizingFor is the resource rule of thumb for one VM on a host with
// the given physical core count: half the cores (never below one),
// and 2 GB of memory per granted core. Both derive from the one
// measured fact, so the sizing scales to whoever's machine this is.
func SizingFor(physical int) (cpus, memMB int) {
	if physical < 1 {
		return 0, 0
	}
	cpus = physical / 2
	if cpus < 1 {
		cpus = 1
	}
	return cpus, cpus * 2048
}

// hostSizing applies the rule to this host. A host that will not say
// its core count sizes nothing, leaving the image defaults.
func hostSizing() (cpus, memMB, physical int) {
	n := platform.PhysicalCores()
	if n == 0 {
		return 0, 0, 0
	}
	cpus, memMB = SizingFor(n)
	return cpus, memMB, n
}
