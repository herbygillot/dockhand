package verify

import (
	"context"
	"fmt"
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
func (f *fake) Log(context.Context, Job) (string, error)     { return "", nil }
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

// ONE IDENTITY, TWO IDIOMS. errors.Is is the DECISION — run.Start stops
// submitting, run.Defer refuses to write a backoff, lease.Acquire
// retires the lease it just wrote — and errors.As is the OBSERVATION, so
// a report can say how full the machine was and when. The shipped tree
// carried a Synchronous field on this value instead, stamped by MUTATING
// the error at three call sites: a provider's observation and a caller's
// road on one value, with the decision written in afterwards. Which road
// is standing there is the caller's own fact and no longer travels here.
func TestNoVacancyIsOneIdentityAndOneObservation(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	full := &NoVacancyError{Busy: 2, Limit: 2, AsOf: at}

	require.ErrorIs(t, full, ErrNoVacancy, "the decision branches on identity and never on words")
	var seen *NoVacancyError
	require.ErrorAs(t, fmt.Errorf("submitting: %w", full), &seen)
	assert.Equal(t, 2, seen.Busy)
	assert.Equal(t, 2, seen.Limit)
	assert.Equal(t, at, seen.AsOf)
	assert.Contains(t, full.Error(), "all 2 slots busy")
}

// AN UNKNOWN VACANCY ADMITS NOTHING, which is rule 7 on the one value
// whose zero is most expensive: Free == 0 alone would mean both "the
// machine is full" and "the provider could not be asked", so a provider
// that is DOWN would read as a busy one.
func TestAnUnknownVacancyAdmitsNothing(t *testing.T) {
	assert.False(t, Vacancy{}.Admits(1), "nobody asked, so nothing is admitted")
	assert.False(t, Vacancy{Free: 4, Limit: 4}.Admits(1),
		"a count with no Known behind it is a forged zero, not an answer")
	assert.True(t, Vacancy{Known: true, Free: 2, Limit: 2}.Admits(2))
	assert.False(t, Vacancy{Known: true, Free: 1, Limit: 2}.Admits(2),
		"the n is ENVIRONMENTS, and a cohort is one guest for N members")
	assert.True(t, Vacancy{Known: true, Free: 0, Limit: 2}.Admits(0),
		"a full machine is a legitimate observation and never a refusal to observe")
}
