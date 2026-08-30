package tart

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// A worker is named for its role, not for the port it happens to be
// testing: port names may carry characters a VM name may not, and a
// verdict environment is interchangeable anyway.
func TestWorkerNamesAreRoleNamed(t *testing.T) {
	assert.True(t, strings.HasPrefix(workerPrefix+stamp(), "dockhand-worker-"))
	assert.NotContains(t, workerPrefix, "verify-", "workers are not named per port")
}

func TestStampIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		s := stamp()
		require.False(t, seen[s], "collision on %s", s)
		seen[s] = true
	}
}

// The provider must not claim what it cannot answer: a base image with
// MacPorts already installed cannot testify to declaration
// completeness, whatever else it can do.
func TestCapabilitiesClaimOnlyViability(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	c := Provider{Platform: seq}.Capabilities()
	assert.True(t, c.Answers(verify.PortViability))
	assert.False(t, c.Answers(verify.DeclarationCompleteness))
	assert.False(t, c.Answers(verify.EditFidelity))
	assert.Equal(t, 2, c.Concurrent, "Apple's licence limit, not the machine's")
	assert.True(t, c.Pristine)
	assert.True(t, c.Interactive, "a failed run leaves the guest as its handle")
}

// A job from another provider is not this provider's to poll.
func TestPollRejectsAForeignJob(t *testing.T) {
	_, err := Provider{}.Poll(t.Context(), verify.Job{Provider: "github", ID: "123"})
	require.ErrorIs(t, err, verify.ErrUnknownJob)
}
