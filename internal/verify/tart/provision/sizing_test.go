package provision

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The rule: half the physical cores, never below one, memory following
// at 2 GB per granted core — both from the one measured fact.
func TestSizingScalesWithTheHost(t *testing.T) {
	for physical, want := range map[int][2]int{
		18: {9, 18432},
		8:  {4, 8192},
		4:  {2, 4096},
		2:  {1, 2048},
		1:  {1, 2048},
		0:  {0, 0},
	} {
		cpus, mem := SizingFor(physical)
		assert.Equal(t, want[0], cpus, "cpus for %d", physical)
		assert.Equal(t, want[1], mem, "memMB for %d", physical)
	}
}
