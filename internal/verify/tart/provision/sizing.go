package provision

import (
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// HostShare is the most of the host's memory every guest may hold
// BETWEEN THEM. Half: dockhand is a tool running on somebody's working
// machine, not a build farm that owns it, and the other half is the
// browser, the editor and the ports tree checkout the person is using
// while a verification runs.
//
// It is the number that actually needs choosing. A per-guest fraction
// picked on its own is a total nobody has bounded — a third each looks
// modest and is 67% of the machine once Apple's two guests are both
// running, which is not a decision anybody made.
const HostShare = 2

// SizingFor is the resource rule of thumb for one VM on a host with the
// given physical core count and installed memory: half the cores (never
// below one), and an equal share of the guests' whole memory budget,
// floored at 2 GB per granted core.
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
// THE PER-GUEST SHARE IS DERIVED AND NOT CHOSEN. What a sizing rule has
// to bound is what the guests take TOGETHER, so the budget is stated
// once (HostShare) and divided by however many may run at once — which
// is tart.Concurrent, Apple's ceiling, and not a number this package
// gets an opinion about. If that ceiling ever moves, the per-guest share
// moves with it and the total stays where it was put.
//
// THE PER-CORE FLOOR STAYS, and it is what keeps this from being a
// regression anywhere. On a small host the floor wins and the answer is
// exactly what it was — 16 GB and eight cores still sizes 4 cpus and
// 8 GB — so the rule only moves where the old one was wrong.
//
// The floor can EXCEED the share, and where it does the budget above is
// not held. That is the floor doing its job rather than a hole in the
// rule: a machine whose budget cannot give each guest 2 GB per granted
// core is already over-subscribed, and shrinking a guest below a usable
// size would buy nothing but a slower failure. At Apple's actual ceiling
// this needs a host under about 110 MB per core to reach, which is no
// machine that runs a macOS guest at all.
//
// hostMemMB of zero is a host that would not say, and the floor is then
// the whole rule: an unmeasured machine is sized by the fact that was
// measured rather than by a guess about the one that was not.
func SizingFor(physical, hostMemMB, guests int) (cpus, memMB int) {
	if physical < 1 {
		return 0, 0
	}
	cpus = physical / 2
	if cpus < 1 {
		cpus = 1
	}
	memMB = cpus * 2048
	if guests < 1 {
		guests = 1
	}
	if share := hostMemMB / HostShare / guests; share > memMB {
		memMB = share
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
	cpus, memMB = SizingFor(n, platform.PhysicalMemoryMB(), tart.Concurrent)
	return cpus, memMB, n
}
