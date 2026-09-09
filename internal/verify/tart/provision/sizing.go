package provision

import "github.com/herbygillot/dockhand/internal/platform"

// SizingFor is the resource rule of thumb for one VM on a host with the
// given physical core count and installed memory: half the cores (never
// below one), and a THIRD OF THE HOST'S MEMORY, floored at 2 GB per
// granted core.
//
// MEMORY IS A FUNCTION OF MEMORY. It used to be 2 GB per granted core
// and nothing else — "both derive from the one measured fact", said of a
// rule where one of the two facts was never measured. A ratio to the
// core count is not a measurement of memory, and on a large host it is
// not even a good guess: 128 GB of RAM and eighteen cores produced a
// nine-core guest with 18 GB, leaving 110 GB idle. That guest then died
// partway through a parallel Skia compile, which is 2 GB per concurrent
// clang on C++ that wants more.
//
// A THIRD AND NOT A HALF because Apple's virtualisation ceiling is two
// guests: at a half, two running guests would claim the whole machine
// and leave the host nothing. A third leaves a third.
//
// THE PER-CORE FLOOR STAYS, and it is what keeps this from being a
// regression anywhere. On a small host the floor wins and the answer is
// exactly what it was — 16 GB and eight cores still sizes 4 cpus and
// 8 GB — so the rule only moves where the old one was wrong.
//
// hostMemMB of zero is a host that would not say, and the floor is then
// the whole rule: an unmeasured machine is sized by the fact that was
// measured rather than by a guess about the one that was not.
func SizingFor(physical, hostMemMB int) (cpus, memMB int) {
	if physical < 1 {
		return 0, 0
	}
	cpus = physical / 2
	if cpus < 1 {
		cpus = 1
	}
	memMB = cpus * 2048
	if third := hostMemMB / 3; third > memMB {
		memMB = third
	}
	return cpus, memMB
}

// hostSizing applies the rule to this host. A host that will not say
// its core count sizes nothing, leaving the image defaults; one that
// will not say its memory is sized by the per-core floor.
func hostSizing() (cpus, memMB, physical int) {
	n := platform.PhysicalCores()
	if n == 0 {
		return 0, 0, 0
	}
	cpus, memMB = SizingFor(n, platform.PhysicalMemoryMB())
	return cpus, memMB, n
}
