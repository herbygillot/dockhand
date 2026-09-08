package run

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// A WAITING CLIENT WATCHES THE STORE, NEVER THE PROVIDER. Under a
// resident dispatcher the judge is somebody else, and a client that
// polled the job would be a second observer of a build it is not
// judging.
func TestAwaitRecordWatchesTheStoreAndNotTheProvider(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe",
		Phase: record.Active, Lease: "req-1"})

	done := make(chan record.Attempt, 1)
	go func() {
		got, err := AwaitRecord(t.Context(), st, "a-1", 20*time.Millisecond)
		assert.NoError(t, err)
		done <- got
	}()

	// The dispatcher judges it, in the record, with no provider involved.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		a := tx.State().Attempts["a-1"]
		a.Phase = record.Finished
		a.Runs = map[string]record.Run{"jq": {State: record.Passed}}
		tx.PutAttempt(a)
		return nil
	}))

	select {
	case got := <-done:
		assert.True(t, got.Settled())
		assert.Equal(t, record.Passed, got.Runs["jq"].State)
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher never saw the verdict the record already carried")
	}
}

// IT READS BEFORE IT WAITS, so a caller that starts watching an attempt
// somebody has already judged returns at once — the ordinary case under
// a resident dispatcher.
func TestAwaitRecordReturnsAtOnceForAnAttemptAlreadyJudged(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe",
		Phase: record.Finished})

	got, err := AwaitRecord(t.Context(), st, "a-1", time.Hour)
	require.NoError(t, err)
	assert.True(t, got.Settled())
}

// EXPIRY DETACHES AND NEVER FAILS. A timeout is the caller giving up on
// watching, never a verdict — the build outlives us by design.
func TestAwaitRecordExpiresWithoutAVerdictAndWithoutAnError(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe",
		Phase: record.Active, Lease: "req-1"})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	got, err := AwaitRecord(ctx, st, "a-1", 10*time.Millisecond)
	require.NoError(t, err, "a detach is not a failure")
	assert.False(t, got.Settled())
	assert.Equal(t, "a-1", got.ID, "what it read last is what comes back")
}

// AN ATTEMPT THAT VANISHES UNDER A WATCHER ENDS THE WATCH. A record
// dropped by a discard is a real end to the thing being watched, and
// waiting out the caller's whole duration would report a still-running
// build.
func TestAwaitRecordEndsWhenTheAttemptIsDroppedUnderIt(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe",
		Phase: record.Active, Lease: "req-1"})

	done := make(chan error, 1)
	go func() {
		_, err := AwaitRecord(t.Context(), st, "a-1", 20*time.Millisecond)
		done <- err
	}()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.Drop("attempt-a-1.json")
		return nil
	}))

	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrNoChange)
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher kept waiting for a record nothing would ever write")
	}
}

// streamer is a provider that can hand its live log over. The core
// Verifier stays five methods, so this is a NINTH optional interface and
// a backend without it is still a perfectly good verifier.
type streamer struct {
	*verifytest.Fake
	out string
}

func (s *streamer) Stream(_ context.Context, _ verify.Job, w io.Writer) error {
	_, err := io.WriteString(w, s.out)
	return err
}

// FOLLOW COPIES BYTES AND JUDGES NOTHING. The verdict still arrives
// through the record, which is how a --trace client and R10 are both
// true at once.
func TestFollowCopiesTheProvidersBytesAndJudgesNothing(t *testing.T) {
	var buf bytes.Buffer
	prov := &streamer{Fake: &verifytest.Fake{}, out: "---> Building jq\n"}
	l := record.Lease{Request: "req-1", ID: record.LeaseID{Provider: "fake", ID: "fake-1"}}

	require.NoError(t, Follow(t.Context(), prov, l, &buf))
	assert.Equal(t, "---> Building jq\n", buf.String())
}

// A PROVIDER THAT CANNOT STREAM SAYS SO BY NAME, and the caller falls
// back to the finished log once the record settles.
func TestFollowRefusesAProviderThatCannotStream(t *testing.T) {
	err := Follow(t.Context(), &verifytest.Fake{}, record.Lease{}, io.Discard)
	assert.ErrorIs(t, err, verify.ErrUnsupported)
}
