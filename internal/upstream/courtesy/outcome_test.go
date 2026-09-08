package courtesy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The policy the outcomes are read against: three consecutive failures
// wall a host, and a wall stands for an hour.
func noteTestPacer() *Pacer {
	return NewPacer(Policy{Ceiling: 1, Backoff: time.Hour, Strikes: 3}, nil)
}

// The whole of D5 in one assertion: a conditional request that got the
// answer it asked for is the cache working, and it clears the streak
// exactly as an ordinary answer does. Struck three times in a row it
// would wall the forge for every port behind it — which is what a tree
// with a warm cache used to do to itself.
func TestNoteNotModifiedIsASuccess(t *testing.T) {
	p := noteTestPacer()
	for range 10 {
		p.Note("api.github.com", NotModified, nil)
	}
	_, up := p.Walled("api.github.com")
	assert.False(t, up, "a revalidated observation must never wall its own host")

	boom := errors.New("dial tcp: no such host")
	p.Note("api.github.com", Unanswered, boom)
	p.Note("api.github.com", Unanswered, boom)
	p.Note("api.github.com", NotModified, nil)
	p.Note("api.github.com", Unanswered, boom)
	p.Note("api.github.com", Unanswered, boom)
	_, up = p.Walled("api.github.com")
	assert.False(t, up, "the streak was broken by a 304, so no three ran consecutively")

	p.Note("api.github.com", Unanswered, boom)
	_, up = p.Walled("api.github.com")
	assert.True(t, up, "three unanswered requests in a row is still a host to leave alone")
}

func TestNoteAppliesEachOutcome(t *testing.T) {
	boom := errors.New("dial tcp: no such host")

	// A refusal walls at once — no streak to accumulate — and every
	// port behind the host is refused with the words that raised it.
	p := noteTestPacer()
	p.Note("h", Refused, errors.New("You have exceeded a secondary rate limit"))
	left, up := p.Walled("h")
	require.True(t, up)
	assert.Positive(t, left)
	err := p.Ask(t.Context(), "h", func(context.Context) error {
		t.Error("the request the wall exists to prevent was made anyway")
		return nil
	})
	require.ErrorIs(t, err, ErrWalled)
	require.ErrorContains(t, err, "secondary rate limit")

	// An answer clears whatever streak was accumulating.
	p = noteTestPacer()
	p.Note("h", Unanswered, boom)
	p.Note("h", Unanswered, boom)
	p.Note("h", Answered, nil)
	p.Note("h", Unanswered, boom)
	p.Note("h", Unanswered, boom)
	_, up = p.Walled("h")
	assert.False(t, up)

	// Dockhand's own clock is neither. A Ctrl-C during a sweep must not
	// wall the tree on the way out.
	p = noteTestPacer()
	for range 10 {
		p.Note("h", Ours, nil)
	}
	_, up = p.Walled("h")
	assert.False(t, up)

	// And the zero value is the conservative reading: a seam that said
	// nothing has reported a request that did not work.
	p = noteTestPacer()
	var unclassified Outcome
	for range 3 {
		p.Note("h", unclassified, boom)
	}
	_, up = p.Walled("h")
	assert.True(t, up, "silence from a classifier must not read as success")
}

func TestOutcomeNames(t *testing.T) {
	for o, want := range map[Outcome]string{
		Unanswered:  "unanswered",
		Answered:    "answered",
		NotModified: "not modified",
		Refused:     "refused",
		Ours:        "ours",
		Outcome(99): "unknown outcome",
	} {
		assert.Equal(t, want, o.String())
	}
}
