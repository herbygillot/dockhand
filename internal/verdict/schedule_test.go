package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

var (
	sequoia = platform.Release{Name: "Sequoia", Product: "15", Darwin: 24}
	sonoma  = platform.Release{Name: "Sonoma", Product: "14", Darwin: 23}
)

func TestResolveRelease(t *testing.T) {
	// A named release is taken as given, whatever the provider offers
	// first: the caller asked.
	got, err := ResolveRelease(sonoma, []platform.Release{sequoia, sonoma})
	require.NoError(t, err)
	assert.Equal(t, sonoma, got)

	// The zero release resolves to the provider's first base, which the
	// VM provider orders newest first. It must resolve before anything
	// is recorded, because a run is keyed by release name and "the
	// default" is not a key.
	got, err = ResolveRelease(platform.Release{}, []platform.Release{sequoia, sonoma})
	require.NoError(t, err)
	assert.Equal(t, sequoia, got)

	// A provider with no bases at all is the no-environment refusal, not
	// an index past the end of an empty list.
	_, err = ResolveRelease(platform.Release{}, nil)
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.Equal(t, "verify: no environment available: no base images", err.Error())

	// Even then, a named release stands: the guard is for the default.
	got, err = ResolveRelease(sonoma, nil)
	require.NoError(t, err)
	assert.Equal(t, sonoma, got)
}

func TestSchedulePlatforms(t *testing.T) {
	t.Run("a plain build", func(t *testing.T) {
		got := SchedulePlatforms("jq", []platform.Release{sequoia}, nil)
		require.Len(t, got, 1)
		assert.Equal(t, sequoia, got[0].Release)
		assert.Nil(t, got[0].Declined, "no preflight answer is not a refusal")
		assert.False(t, got[0].NeedsXcode)
		assert.Empty(t, got[0].Message)
	})

	t.Run("a port that needs a full Xcode says so before the boot", func(t *testing.T) {
		got := SchedulePlatforms("php56-apcu", []platform.Release{sequoia},
			map[string]Preflight{"Sequoia": {UseXcode: true}})
		require.Len(t, got, 1)
		assert.True(t, got[0].NeedsXcode)
		assert.Nil(t, got[0].Declined)
	})

	// mpbb's list-time exclusion: a known_fail port is recorded
	// unsupported before any VM boots. Recording rather than skipping is
	// the point — a platform a port refuses is a real verdict about that
	// platform.
	t.Run("a port that declines the platform is recorded, not built", func(t *testing.T) {
		got := SchedulePlatforms("jq", []platform.Release{sequoia},
			map[string]Preflight{"Sequoia": {KnownFail: true}})
		require.Len(t, got, 1)
		require.NotNil(t, got[0].Declined)
		assert.Equal(t, record.Unsupported, got[0].Declined.State)
		assert.Equal(t, "declares known_fail on Sequoia", got[0].Declined.Detail)
		assert.Equal(t, "jq declares known_fail on Sequoia; recorded unsupported — no build attempted",
			got[0].Message)
	})

	// The answers are per release, so a port refusing one platform is
	// still built on the others — which is the whole reason a verdict
	// set has more than one run in it.
	t.Run("one refusal does not speak for the others", func(t *testing.T) {
		got := SchedulePlatforms("jq", []platform.Release{sequoia, sonoma},
			map[string]Preflight{"Sonoma": {KnownFail: true}})
		require.Len(t, got, 2)
		assert.Nil(t, got[0].Declined)
		require.NotNil(t, got[1].Declined)
		assert.Equal(t, "declares known_fail on Sonoma", got[1].Declined.Detail)
	})

	assert.Empty(t, SchedulePlatforms("jq", nil, nil))
}
