package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// stager is the consumer-owned seam app implements over tempdir, git and
// the evaluator. There is no implementation in the tree until step 10 —
// the drain orders its queue and starts nothing until then — so the
// sequencer's tests stand one in, which is what an interface is for.
type stager struct {
	dirs map[string]string
	pre  map[string]Preflight
	err  error
	// seen records the sha it was asked to stage, because the whole point
	// of the seam is that the queue carries an identity and the staging
	// happens at the moment of starting.
	seen []string
}

func (s *stager) Stage(_ context.Context, sha string, subjects []record.Subject) ([]Member, map[string]Preflight, error) {
	s.seen = append(s.seen, sha)
	if s.err != nil {
		return nil, nil, s.err
	}
	out := make([]Member, 0, len(subjects))
	for _, sub := range subjects {
		dir := s.dirs[sub.Port]
		if dir == "" {
			dir = "/stage/" + sub.Port
		}
		out = append(out, Member{Port: sub.Port, Portdir: dir, Names: sub.Names})
	}
	pre := s.pre
	if pre == nil {
		pre = map[string]Preflight{}
		for _, sub := range subjects {
			pre[sub.Port] = Preflight{Read: true}
		}
	}
	return out, pre, nil
}

func (s *stager) Baseline(_ context.Context, sha string, subjects []record.Subject) ([]string, error) {
	if sha == "" {
		return nil, nil
	}
	out := make([]string, 0, len(subjects))
	for _, sub := range subjects {
		out = append(out, "/baseline/"+sub.Port)
	}
	return out, nil
}

func claimant() lease.Claimant {
	return lease.Claimant{Owner: owner(), Expires: clock.Add(time.Hour), Pass: "pass-1"}
}

func changeOf(id record.ChangeID, ports ...string) record.Change {
	c := minted(id)
	for _, p := range ports {
		c.Subjects = append(c.Subjects, record.Subject{Port: p, Names: []string{p}})
	}
	return c
}

// enqueued plants a change and a queued attempt for it, the way a
// person's bump would in one Amend.
func enqueued(t *testing.T, st *statestore.Store, c record.Change, spec Spec) record.Attempt {
	t.Helper()
	plantChange(t, st, c)
	var a record.Attempt
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		var err error
		a, err = EnqueueIn(tx, Enqueue{
			Change: c.ID, Sha: c.Tip, Content: c.Content, Spec: spec,
			Platform: sequoia, EnqueuedBy: owner(),
		}, clock)
		return err
	}))
	return a
}

// START SUBMITS AND RECORDS, IN ONE AMEND. A record that named a lease
// and no runs, or runs and no lease, is a shape no reader can act on.
func TestStartSubmitsAndAdvancesTheAttemptWithItsLease(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{Evidence: "built in a pristine VM"}
	stg := &stager{}

	got, err := Start(t.Context(), st, fake, stg, a, claimant(), clock)
	require.NoError(t, err)

	assert.Equal(t, []string{c.Tip}, stg.seen,
		"the queue carries an identity, and the re-plan happens at the moment of starting")
	assert.True(t, got.Active())
	assert.Equal(t, record.Running, got.Runs["jq"].State)
	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports)
	assert.Equal(t, []string{"/stage/jq"}, fake.Submitted[0].Portdirs)
	assert.NotEmpty(t, fake.Submitted[0].ID, "lease.Acquire mints the request id before the call")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	l, ok := s.Leases[got.Lease]
	require.True(t, ok, "the attempt names the lease document the store holds")
	assert.Equal(t, record.Active, l.Phase)
	assert.NotEmpty(t, l.ID.ID, "the provider's job identity is written where recovery joins on it")
}

// A FULL MACHINE WRITES NOTHING ON THE ATTEMPT and the sentinel travels
// unwrapped, so the caller stops submitting without reading a sentence.
func TestStartLeavesTheAttemptQueuedOnANoVacancyRefusal(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{SubmitErr: &verify.NoVacancyError{Busy: 2, Limit: 2}}

	got, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.ErrorIs(t, err, verify.ErrNoVacancy)
	assert.True(t, got.Queued())

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	stored := s.Attempts[a.ID]
	assert.True(t, stored.Queued(), "it stays queued: the machine is busy, not the port broken")
	assert.Nil(t, stored.NotBefore, "a full machine is not this attempt's fault, so no backoff is written")
	assert.Zero(t, stored.Tries)
}

// AN ATTEMPT'S OWN FAULT IS DEFER'S: the backoff Order reads has to
// survive the process that learned it, or a permanently broken port
// costs a VM on every nightly pass.
func TestStartDefersAnAttemptThatFailedForItsOwnReasons(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	stg := &stager{err: errors.New("the commit names a portdir that is not in it")}

	_, err := Start(t.Context(), st, &verifytest.Fake{}, stg, a, claimant(), clock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in it", "the cause travels, never the bookkeeping")

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	stored := s.Attempts[a.ID]
	assert.True(t, stored.Queued())
	assert.Equal(t, 1, stored.Tries)
	require.NotNil(t, stored.NotBefore)
	assert.True(t, stored.NotBefore.After(clock), "Order reads this and puts it behind every ready attempt")
}

// A ROSTER THAT ALL DECLINES ENDS THE ATTEMPT HERE, with no lease taken
// and no guest asked for. Left queued it would be retried at full cost
// on every pass forever.
func TestStartFinishesAnAttemptWhoseWholeRosterDeclined(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}
	stg := &stager{pre: map[string]Preflight{"jq": {Read: true, KnownFail: true}}}

	got, err := Start(t.Context(), st, fake, stg, a, claimant(), clock)
	require.NoError(t, err)
	assert.True(t, got.Settled())
	assert.Equal(t, record.Unsupported, got.Runs["jq"].State)
	assert.Empty(t, fake.Submitted, "no VM booted to discover known_fail")

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	assert.Empty(t, s.Leases, "nothing was ever asked of the provider, so nothing is owed")
}

// START PERFORMS NO HOLD CHECK. A crossing's born-hold withholds
// publication and never the build: cli_spec flow 10 shows the attempt
// SUBMITTED, with "the hold is on publication, not on the build".
func TestStartSubmitsAChangeHeldForItsCrossing(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	c.Hold = &record.Hold{Origin: record.HoldCrossing, Reason: "this change takes the port out of stable"}
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}

	got, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)
	assert.True(t, got.Active())
	assert.Len(t, fake.Submitted, 1)
}

// AN ATTEMPT WHOSE CHANGE IS GONE IS REPORTED, NEVER STARTED.
func TestStartRefusesAnAttemptWhoseChangeHasNoRecord(t *testing.T) {
	st := newStore(t)
	plantAttempt(t, st, record.Attempt{ID: "a-orphan", Change: "gone", Sha: "cafe",
		Phase: record.Requested})
	s, err := st.Read(t.Context())
	require.NoError(t, err)

	_, err = Start(t.Context(), st, &verifytest.Fake{}, &stager{}, s.Attempts["a-orphan"], claimant(), clock)
	assert.ErrorIs(t, err, ErrNoChange)
}

// DEFER REFUSES A CAUSE THAT IS NOT THE ATTEMPT'S FAULT, mechanically
// rather than in a sentence: fed a full machine it would back a healthy
// port off hours ahead while the machine sat idle.
func TestDeferRefusesTheMachinesOwnFacts(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe", Phase: record.Requested})

	for _, cause := range []error{
		&verify.NoVacancyError{Busy: 2, Limit: 2},
		verify.ErrNoProvider,
		verify.ErrNoEnvironment,
	} {
		require.ErrorIs(t, Defer(t.Context(), st, "a-1", cause, clock), ErrNotAFault)
	}

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Zero(t, s.Attempts["a-1"].Tries)
	assert.Nil(t, s.Attempts["a-1"].NotBefore)
}

// THE BACKOFF GROWS AND IS CAPPED: five minutes, doubling, an hour.
func TestDeferBacksOffFurtherOnEveryTry(t *testing.T) {
	assert.Equal(t, 5*time.Minute, backoff(1))
	assert.Equal(t, 10*time.Minute, backoff(2))
	assert.Equal(t, time.Hour, backoff(12))
	assert.Equal(t, time.Hour, backoff(40))
}

// FINISH IS THE ONE ORDER: observe, judge, settle-with-the-claim, then
// the provider call OUTSIDE the lock.
func TestFinishSettlesAPassAndHandsTheGuestBackOutsideTheLock(t *testing.T) {
	st, led := newFixture(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{Evidence: "built in a pristine VM"}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)

	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "dockhand-worker-1"}}
	fake.Logs = map[string]string{"fake-1": "0 errors and 1 warning found\n"}

	spec := specOf("jq")
	spec.Roster[0].Portdir = "/stage/jq"
	got, err := Finish(t.Context(), st, led, fake, nil,
		started, spec, nil, claimant(), func() time.Time { return clock })
	require.NoError(t, err)

	assert.True(t, got.Settled())
	assert.Equal(t, record.Passed, got.Runs["jq"].State)
	require.NotNil(t, got.Runs["jq"].Lint)
	assert.Equal(t, "1 warning", *got.Runs["jq"].Lint)
	assert.Equal(t, "built in a pristine VM", got.Runs["jq"].Evidence)
	assert.Equal(t, []string{"fake-1"}, fake.Released, "the release is performed, once, outside the lock")

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	l := s.Leases[got.Lease]
	assert.True(t, l.Returned(), "the claim and the confirmation are both down")
	assert.Equal(t, "dockhand-worker-1", l.Handle)
}

// A FAILURE KEEPS ITS GUEST ON A DEADLINE. The slot comes back on its
// own rather than being held until somebody remembers.
func TestFinishRetainsAKeptGuestUntilADeadline(t *testing.T) {
	st, led := newFixture(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "dockhand-worker-1"}}
	fake.Logs = map[string]string{"fake-1": "Error: Failed to build jq: command execution failed\n"}

	spec := specOf("jq")
	got, err := Finish(t.Context(), st, led, fake, nil,
		started, spec, nil, claimant(), func() time.Time { return clock })
	require.NoError(t, err)

	assert.Equal(t, record.Failed, got.Runs["jq"].State)
	assert.Empty(t, fake.Released, "the environment IS the debug handle")

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	l := s.Leases[got.Lease]
	assert.True(t, l.Held())
	require.NotNil(t, l.Retain)
	assert.Equal(t, clock.Add(lease.KeepFor), *l.Retain)
}

// A JOB STILL RUNNING WRITES NOTHING AT ALL. A caller that loops on this
// is a judge waiting; one that meets it once is status reporting.
func TestFinishOnARunningJobIsAReadAndNotAWrite(t *testing.T) {
	st, led := newFixture(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)

	before, err := st.Read(t.Context())
	require.NoError(t, err)
	got, err := Finish(t.Context(), st, led, fake, nil,
		started, specOf("jq"), nil, claimant(), func() time.Time { return clock })
	require.NoError(t, err)
	assert.False(t, got.Settled())

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before.At, after.At, "an unchanged attempt rewritten is a commit per tick per attempt")
}

// FINISH IS A NO-OP ON AN ATTEMPT ALREADY FINISHED, and that is what
// makes a dispatcher taking the judge's chair mid-wait safe.
func TestFinishIsANoOpOnAnAttemptSomebodyElseAlreadyJudged(t *testing.T) {
	st, led := newFixture(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed}}

	first, err := Finish(t.Context(), st, led, fake, nil, started, specOf("jq"), nil,
		claimant(), func() time.Time { return clock })
	require.NoError(t, err)
	require.True(t, first.Settled())

	second, err := Finish(t.Context(), st, led, fake, nil, started, specOf("jq"), nil,
		claimant(), func() time.Time { return clock })
	require.NoError(t, err)
	assert.True(t, second.Settled())
	assert.Len(t, fake.Released, 1, "the guest is handed back once, by the one judge")
}

// AN INTERRUPT STOPS A RUNNING BUILD BY RELEASING IT, and the verdict is
// the judge's reading of the interrupt — there is no run.Cancel that
// writes a second kind of verdict.
func TestFinishWithAnInterruptStopsARunningBuildThroughTheJudge(t *testing.T) {
	st, led := newFixture(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))
	fake := &verifytest.Fake{}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)

	itr := &record.Interrupt{Why: record.InterruptSuperseded, By: owner(), At: clock,
		Detail: "the branch moved to 1a2b3c4"}
	got, err := Finish(t.Context(), st, led, fake, nil,
		started, specOf("jq"), itr, claimant(), func() time.Time { return clock })
	require.NoError(t, err)

	assert.True(t, got.Settled())
	assert.Equal(t, record.Superseded, got.Runs["jq"].State)
	require.NotNil(t, got.Interrupt)
	assert.Equal(t, record.InterruptSuperseded, got.Interrupt.Why)
	assert.Equal(t, []string{"fake-1"}, fake.Released,
		"under the five-method Verifier there is no Cancel: Release of a running guest is the stop")
}

// ROSTER DRIFT, WHICH IS WHY THE SPECIFICATION IS FROZEN.
//
// Queue one member; add a second to the change; start the old attempt.
// The drain used to rebuild the roster from the change's CURRENT
// subjects, so the provider received both members while the attempt's
// recorded Spec id sat unchanged — a different question under the same
// identity, and nothing able to notice.
func TestStartBuildsTheQueuedRosterAndNotTheChangesCurrentOne(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))

	// The change grows a member after the attempt was queued.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		cur := tx.State().Changes[string(c.ID)]
		cur.Subjects = append(cur.Subjects, record.Subject{Port: "later", Names: []string{"later"}})
		tx.PutChange(cur)
		return nil
	}))

	fake := &verifytest.Fake{}
	started, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports,
		"the question that was queued, not the one the change has since become")
	assert.Equal(t, a.Spec, started.Spec, "and the id still names what actually ran")
	assert.Equal(t, []string{"jq"}, started.Members())
}

// The identity and the thing it identifies are written together, so a
// record whose two halves disagree is refused rather than built. It
// should be unreachable; it exists because the id used to be stored
// alone and re-derived hours later, which produced exactly this
// disagreement silently.
func TestStartRefusesAnAttemptWhoseRosterDoesNotHashToItsSpecID(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq")
	a := enqueued(t, st, c, specOf("jq"))

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		cur := tx.State().Attempts[a.ID]
		cur.Roster.Seats = append(cur.Roster.Seats, record.Seat{Port: "smuggled"})
		tx.PutAttempt(cur)
		return nil
	}))
	tampered, err := st.Read(t.Context())
	require.NoError(t, err)

	fake := &verifytest.Fake{}
	_, err = Start(t.Context(), st, fake, &stager{}, tampered.Attempts[a.ID], claimant(), clock)
	require.ErrorIs(t, err, ErrSpecMismatch)
	assert.Empty(t, fake.Submitted, "nothing was built under a question nobody asked")
}

// The hashed inputs that were not on the record at all. internal/app
// said so out loud — "FromSource and Requires are inside it and are NOT
// re-derivable from the record" — and a spec carrying either could
// therefore never be replayed, nor its id recomputed honestly.
func TestAFrozenRosterCarriesTheInputsNothingCouldDeriveBefore(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "jq", "oniguruma")
	spec := specOf("jq", "oniguruma")
	spec.FromSource = []string{"jq"}
	spec.Requires = [][]string{nil, {"jq"}}
	a := enqueued(t, st, c, spec)

	back := Frozen(a)
	assert.Equal(t, []string{"jq"}, back.FromSource)
	assert.Equal(t, [][]string{nil, {"jq"}}, back.Requires)
	assert.Equal(t, a.Spec, back.ID(), "and the thawed question hashes to the recorded id")

	fake := &verifytest.Fake{}
	_, err := Start(t.Context(), st, fake, &stager{}, a, claimant(), clock)
	require.NoError(t, err)
	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].FromSource, "and they reach the provider")
	assert.Equal(t, [][]string{nil, {"jq"}}, fake.Submitted[0].Requires)
}
