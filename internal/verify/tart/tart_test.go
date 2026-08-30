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
	c := Provider{Bases: []Base{{VM: "mp-base", Release: seq}}}.Capabilities()
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

// A machine may hold a base per macOS release, and those are several
// platforms of one provider rather than several providers.
func TestCapabilitiesListEveryBase(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	c := Provider{Bases: []Base{
		{VM: "dockhand-base-sequoia", Release: seq},
		{VM: "dockhand-base-sonoma", Release: son},
	}}.Capabilities()

	assert.True(t, c.Supports(seq))
	assert.True(t, c.Supports(son))
	assert.Len(t, c.Platforms, 2)
	assert.Equal(t, 2, c.Concurrent, "two guests total, not two per platform")

	tahoe, _ := platform.ByName("Tahoe")
	assert.False(t, c.Supports(tahoe), "no image, no claim")
}

func TestBaseForPicksTheRequestedRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	p := Provider{Bases: []Base{
		{VM: "first-sequoia", Release: seq},
		{VM: "second-sonoma", Release: son},
	}}

	b, err := p.baseFor(son)
	require.NoError(t, err)
	assert.Equal(t, "second-sonoma", b.VM)

	// No platform named takes the first, which is what a caller who
	// does not care means.
	b, err = p.baseFor(platform.Release{})
	require.NoError(t, err)
	assert.Equal(t, "first-sequoia", b.VM)
}

// A release with no image is refused, never substituted: a build on one
// macOS is not evidence about another.
func TestBaseForRefusesAnUnservedRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	tahoe, _ := platform.ByName("Tahoe")
	_, err := Provider{Bases: []Base{{VM: "s", Release: seq}}}.baseFor(tahoe)
	require.ErrorIs(t, err, verify.ErrUnsupported)
	assert.Contains(t, err.Error(), "Tahoe")
}

func TestBaseForWithNoImagesIsAMachineFact(t *testing.T) {
	_, err := Provider{}.baseFor(platform.Release{})
	require.ErrorIs(t, err, verify.ErrNoEnvironment,
		"no images is a fact about the machine, not about the request")
}

// A golden must not contain its base's name: everything that looks a
// base up by substring would otherwise find two.
func TestGoldenNameDoesNotContainBaseName(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	assert.Equal(t, "dockhand-golden-sequoia", GoldenName(seq))
	assert.NotContains(t, GoldenName(seq), BaseName(seq))

	for _, r := range platform.Releases {
		assert.NotContains(t, GoldenName(r), BaseName(r), "release %s", r.Name)
		assert.NotContains(t, GoldenName(r), " ", "a VM name may not carry the space in Big Sur")
	}
}
