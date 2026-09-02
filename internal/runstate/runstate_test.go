package runstate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// The provider seam is resolved once per run, whatever the answer.
// Composing a provider lists the machine's base images; status used to
// pay for that once per release per branch, and a run's answer about
// its own machine cannot change under it.
func TestVerifyProviderResolvesOncePerRun(t *testing.T) {
	var calls int
	fake := &verifytest.Fake{}
	rc := &Context{Verifier: func(context.Context) (verify.Verifier, error) {
		calls++
		return fake, nil
	}}

	for range 3 {
		got, err := rc.VerifyProvider(t.Context())
		require.NoError(t, err)
		assert.Same(t, fake, got)
	}
	assert.Equal(t, 1, calls, "the seam is invoked once per run")
}

// A refusal is memoized too: a machine with no provider says so once,
// and every later asker gets the same sentence without a second probe.
func TestVerifyProviderRemembersItsRefusal(t *testing.T) {
	var calls int
	boom := errors.New("no base images")
	rc := &Context{Verifier: func(context.Context) (verify.Verifier, error) {
		calls++
		return nil, boom
	}}

	for range 3 {
		_, err := rc.VerifyProvider(t.Context())
		require.ErrorIs(t, err, boom)
	}
	assert.Equal(t, 1, calls, "the seam is invoked once per run")
}

// A nil seam is a wiring bug, not a machine state, and is not something
// to memoize: it never reaches the provider at all.
func TestVerifyProviderRefusesAnUnwiredRun(t *testing.T) {
	rc := &Context{}
	_, err := rc.VerifyProvider(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no verify provider wired")
}

// The tree is opened once, and a run that names no tree gets the
// refusal its caller turns into that command's own usage question.
func TestTreeRefusesWhenNoTreeIsNamed(t *testing.T) {
	rc := &Context{}
	_, err := rc.Tree()
	require.Error(t, err)
	_, again := rc.Tree()
	assert.Equal(t, err, again, "the answer is remembered, refusal included")
}
