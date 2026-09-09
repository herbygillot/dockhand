package provision

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// THE FLOOR IS THE OLD RULE, and it still answers wherever it used to.
// Half the physical cores, never below one, 2 GB per granted core — on a
// host whose memory is small enough that a third of it is less than
// that, which is every host the old rule was written on.
//
// hostMemMB of zero is a host that would not say, and the floor is then
// the whole rule: an unmeasured machine is sized by the fact that WAS
// measured rather than by a guess about the one that was not.
func TestSizingFallsBackToThePerCoreFloor(t *testing.T) {
	for _, c := range []struct {
		physical, hostMemMB int
		cpus, mem           int
	}{
		{18, 0, 9, 18432}, // memory unknown: the old answer exactly
		{8, 16384, 4, 8192},
		{4, 8192, 2, 4096},
		{2, 4096, 1, 2048},
		{1, 2048, 1, 2048},
		{0, 131072, 0, 0}, // no cores, no sizing, whatever the memory
	} {
		cpus, mem := SizingFor(c.physical, c.hostMemMB, 2)
		assert.Equal(t, c.cpus, cpus, "cpus for %d cores", c.physical)
		assert.Equal(t, c.mem, mem, "memMB for %d cores, %d MB host", c.physical, c.hostMemMB)
	}
}

// MEMORY IS A FUNCTION OF MEMORY. The rule was 2 GB per granted core and
// nothing else — "both derive from the one measured fact", said of a
// rule where one of the two facts was never measured.
//
// Measured on the host this was found on: 128 GB and eighteen cores gave
// a nine-core guest 18 GB and left 110 GB idle, and that guest died
// partway through a parallel Skia compile — 2 GB per concurrent clang on
// C++ that wants more.
func TestSizingGivesALargeHostsMemoryToTheGuest(t *testing.T) {
	cpus, mem := SizingFor(18, 131072, 2) // 128 GB, 18 cores, Apple's two guests
	assert.Equal(t, 9, cpus)
	assert.Equal(t, 131072/2/2, mem, "an equal share of half the host, not 2 GB a core")
	assert.Greater(t, mem, 18432, "the old rule's answer was the problem")
}

// WHAT IS BOUNDED IS WHAT THE GUESTS TAKE TOGETHER, which is the whole
// point of deriving the per-guest share instead of choosing one. A
// fraction picked per guest is a total nobody decided: a third each
// looks modest and is 67% of the machine once both of Apple's guests are
// running.
//
// The bound holds wherever the share is the answer. Where the FLOOR is
// the answer it does not, and that is the floor doing its job: a budget
// that cannot give each guest 2 GB per core is a machine already
// over-subscribed, and shrinking the guest below a usable size would buy
// nothing but a slower failure.
func TestEveryGuestTogetherNeverExceedsTheHostShare(t *testing.T) {
	const host, cores = 131072, 18
	floor := cores / 2 * 2048
	for _, guests := range []int{1, 2, 3, 4, 8} {
		_, mem := SizingFor(cores, host, guests)
		if mem == floor {
			continue // the floor answered; see above
		}
		assert.LessOrEqual(t, guests*mem, host/HostShare,
			"%d guests at %d MB each must not exceed half the host", guests, mem)
	}
}

// AT APPLE'S ACTUAL CEILING THE TOTAL IS EXACTLY THE BUDGET, which is
// the case that matters: two guests, half the machine, half left for the
// person using it.
func TestAtTheRealCeilingTheGuestsHoldHalfTheHost(t *testing.T) {
	const host = 131072
	_, mem := SizingFor(18, host, 2)
	assert.Equal(t, host/2, 2*mem)
	assert.Equal(t, host/2, mem*2, "and the other half is the browser and the checkout")
}

// AND THE SHARE FOLLOWS THE CEILING while the share is what answers. If
// Apple allowed a third guest, each would get less and the total would
// stay where it was put.
func TestThePerGuestShareShrinksAsTheCeilingRises(t *testing.T) {
	const host = 131072
	_, two := SizingFor(18, host, 2)
	_, three := SizingFor(18, host, 3)
	assert.Greater(t, two, three, "more guests, smaller share")
	assert.LessOrEqual(t, 3*three, host/HostShare, "and the total stays inside the budget")
}
