package verify

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
)

// fake is a provider that returns a scripted sequence of states.
type fake struct {
	states []State
	polls  int
	caps   Capabilities
}

func (f *fake) Capabilities() Capabilities                   { return f.caps }
func (f *fake) Submit(context.Context, Request) (Job, error) { return Job{Provider: "fake"}, nil }
func (f *fake) Release(context.Context, Job) error           { return nil }
func (f *fake) Poll(context.Context, Job) (Status, error) {
	s := f.states[min(f.polls, len(f.states)-1)]
	f.polls++
	return Status{State: s}, nil
}

func TestAwaitReturnsOnTheFirstTerminalState(t *testing.T) {
	f := &fake{states: []State{Running, Running, Passed, Failed}}
	st, err := Await(t.Context(), f, Job{}, time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, Passed, st.State)
	assert.Equal(t, 3, f.polls, "must stop at the first terminal state, not keep polling")
}

// A job outlives the process, so giving up on it is the caller's
// deadline expiring rather than the job ending.
func TestAwaitHonoursTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := Await(ctx, &fake{states: []State{Running}}, Job{}, time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTerminalStates(t *testing.T) {
	assert.False(t, Running.Terminal())
	for _, s := range []State{Passed, Failed, Errored} {
		assert.True(t, s.Terminal(), "%s must be terminal", s)
	}
}

// Errored is a fact about the machine and Failed is a finding about the
// port; conflating them would report a broken VM as a broken port.
func TestErroredIsNotFailed(t *testing.T) {
	assert.NotEqual(t, Failed, Errored)
	assert.Equal(t, "errored", Errored.String())
	assert.Equal(t, "failed", Failed.String())
}

func TestCapabilitiesAnswerOnlyWhatTheyClaim(t *testing.T) {
	c := Capabilities{Propositions: []Proposition{PortViability}}
	assert.True(t, c.Answers(PortViability))
	assert.False(t, c.Answers(DeclarationCompleteness))
	assert.False(t, Capabilities{}.Answers(PortViability), "claiming nothing answers nothing")
}

func TestSupportsIsPerRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	c := Capabilities{Platforms: []platform.Release{seq}}
	assert.True(t, c.Supports(seq))
	assert.False(t, c.Supports(son))
	assert.True(t, c.Supports(platform.Release{}), "the zero release asks for the default")
	assert.False(t, Capabilities{}.Supports(platform.Release{}),
		"a provider with no platforms has no default either")
}
