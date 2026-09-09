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
		cpus, mem := SizingFor(c.physical, c.hostMemMB)
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
	cpus, mem := SizingFor(18, 131072) // 128 GB, 18 cores
	assert.Equal(t, 9, cpus)
	assert.Equal(t, 131072/3, mem, "a third of the host, not 2 GB a core")
	assert.Greater(t, mem, 18432, "the old rule's answer was the problem")
}

// A THIRD AND NOT A HALF, because Apple's virtualisation ceiling is two
// guests: at a half, two running guests would claim the whole machine
// and leave the host nothing.
func TestSizingLeavesTheHostAThirdWithTwoGuestsRunning(t *testing.T) {
	const host = 131072
	_, mem := SizingFor(18, host)
	assert.LessOrEqual(t, 2*mem, host*2/3+1, "two guests must not claim the machine")
}
