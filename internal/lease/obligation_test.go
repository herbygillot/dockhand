package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// table scripts this host's process table, which is the only way to
// write a case for "a PID present with a different start time" without
// a second process to point the test at.
func table(t *testing.T, live map[int]time.Time, err error) {
	t.Helper()
	orig := processStart
	processStart = func(_ context.Context, pid int) (time.Time, bool, error) {
		if err != nil {
			return time.Time{}, false, err
		}
		start, ok := live[pid]
		return start, ok, nil
	}
	t.Cleanup(func() { processStart = orig })
}

// alive is the process table this checkout's own identity is live in:
// the kernel's start time is a moment BEFORE the Since dockhand
// recorded, which is what the real pair always looks like.
func alive() map[int]time.Time {
	return map[int]time.Time{100: birth.Add(-300 * time.Millisecond)}
}

func seizure(grace time.Duration) Seizure { return Seizure{Set: true, Grace: grace} }

// A HELD LEASE WITH A RUNNING BUILD IS NEVER AN OBLIGATION, whatever its
// owner's liveness or its pass token. Built the other way, a resident
// dispatcher destroyed its own running VMs on the tick after the grace
// cutoff: every Active lease its own Start wrote in pass N is, at pass
// N+1, owned by this PID under an earlier token, and a build runs for
// hours.
func TestADetachedBuildIsNotAnObligation(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	l := held(t, st, "chg-1")
	fake := &verifytest.Fake{Live: []verify.Worker{{Name: "w-1", Owner: me().Root, Request: l.Request,
		Job: verify.Job{Provider: "fake", ID: "fake-1"}}}}

	// A week later, under a new pass token, from a process that is this
	// one: still not an obligation.
	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-2", now.Add(7*24*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, obs, "the record owns the build, not the process that started it")
}

// THE POPULATION, ALL FOUR KINDS AT ONCE, so that the rule is stated by
// a test and not only by a paragraph.
func TestTheFourKindsAndNothingElse(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "owed", State: record.ChangeMinted})
	plantChange(t, st, record.Change{ID: "req", State: record.ChangeMinted})
	plantChange(t, st, record.Change{ID: "kept", State: record.ChangeMinted})
	plantChange(t, st, record.Change{ID: "merged", State: record.ChangePublished})
	plantChange(t, st, record.Change{ID: "live", State: record.ChangeMinted})

	// Owed: a release claimed here and never confirmed.
	held(t, st, "owed")
	_, took, err := Request(t.Context(), st, "owed", platformName, claimant(me()), now.Add(-time.Hour))
	require.NoError(t, err)
	require.True(t, took)

	// Requested: Acquire wrote the record and the provider never answered.
	stalled := &verifytest.Fake{SubmitErr: errors.New("i/o timeout")}
	_, err = Acquire(t.Context(), st, stalled, "req", request(""), claimant(me()), at(-time.Hour))
	require.Error(t, err)

	// Due: a kept guest whose deadline has passed.
	held(t, st, "kept")
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		RetainIn(tx, "kept", platformName, now.Add(-time.Minute))
		return nil
	}))

	// Due: a guest a merged change left behind, with no deadline at all.
	held(t, st, "merged")

	// Not an obligation: a live detached build.
	held(t, st, "live")

	// Untracked: a worker no lease's request joins.
	fake := &verifytest.Fake{Live: []verify.Worker{{Name: "stray", Request: "nobody-minted-this"}}}

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)

	got := map[record.ChangeID]ObligationKind{}
	var untracked int
	for _, ob := range obs {
		if ob.Kind == Untracked {
			untracked++
			assert.Equal(t, "stray", ob.Worker)
			continue
		}
		got[ob.Change] = ob.Kind
	}
	assert.Equal(t, map[record.ChangeID]ObligationKind{
		"owed": Owed, "req": Requested, "kept": Due, "merged": Due,
	}, got, "exactly the population, and the live build is not in it")
	assert.Equal(t, 1, untracked)
}

// A missing state ref means this pass cannot account for anything on the
// machine, and the caller on the other side DESTROYS provider resources.
// "I could not find out" must not arrive as "there is nothing here".
func TestOutstandingRefusesWhenItCannotReadTheRecord(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	_, err := Outstanding(t.Context(), st, &verifytest.Fake{}, me(), "", now)
	require.ErrorIs(t, err, statestore.ErrNoState)
}

// A provider that cannot be asked yields no untracked obligations and no
// error: the pass has learned nothing about this machine's workers,
// which is different from learning there are none. The lease-backed
// kinds are still answered, because the state ref answers those alone.
func TestAProviderThatCannotBeAskedYieldsSilenceAndNotAnEmptyMachine(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished})
	held(t, st, "chg-1")

	for _, prov := range []verify.Verifier{
		verifytest.Incapable{Fake: &verifytest.Fake{}},                                           // not a WorkerLister
		&verifytest.Fake{WorkersErr: errors.New("tart is not installed")},                        // a listing that failed
		&verifytest.Fake{Live: []verify.Worker{{Name: "stray"}}, WorkersErr: errors.New("boom")}, // both
	} {
		obs, err := Outstanding(t.Context(), st, prov, me(), "", now)
		require.NoError(t, err)
		require.Len(t, obs, 1, "the lease-backed kind is still reported")
		assert.Equal(t, Due, obs[0].Kind)
	}
}

// A worker whose lease this checkout holds is not untracked, whichever
// name it is joined by: the request token the provider echoes, or the
// handle a Status reported.
func TestAJoinedWorkerIsNotUntracked(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	l := held(t, st, "chg-1")
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		HandleIn(tx, "chg-1", platformName, "dockhand-worker-named")
		return nil
	}))

	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "by-token", Request: l.Request},
		{Name: "dockhand-worker-named"},
	}}
	obs, err := Outstanding(t.Context(), st, fake, me(), "", now)
	require.NoError(t, err)
	assert.Empty(t, obs)
}

// STANDING IS THE --reclaim-orphans RULE AS A TYPED VALUE, decided once,
// from the root, the liveness pair and the pass token.
func TestStandingIsDecidedFromRootLivenessAndPass(t *testing.T) {
	ctx := context.Background()
	other := me()
	other.Root = "/elsewhere/ports"
	deadPeer := me()
	deadPeer.PID = 200
	reused := me()
	reused.PID = 100
	reused.Since = birth.Add(-9 * time.Hour) // the number is ours; the process is not
	elsewhere := me()
	elsewhere.Host = "another-mac"
	elsewhere.PID = 300

	table(t, alive(), nil)

	got, root := standingOf(ctx, other, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, ForeignRoot, got)
	assert.Equal(t, "/elsewhere/ports", root, "a report names it rather than saying somebody else's")

	got, _ = standingOf(ctx, me(), &record.Claim{Pass: "pass-1"}, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, Mine, got)

	got, _ = standingOf(ctx, me(), &record.Claim{Pass: "pass-0"}, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, EarlierPass, got, "kept as its own value for the report")

	got, _ = standingOf(ctx, deadPeer, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, DeadElsewhere, got, "the commonest same-checkout case: a dispatcher that restarted")

	table(t, map[int]time.Time{100: alive()[100], 200: deadPeer.Since.Add(-200 * time.Millisecond)}, nil)
	got, _ = standingOf(ctx, deadPeer, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, LiveElsewhere, got, "a peer that is genuinely alive is reported and never seized")

	// A PID that is present with a start time the record does not name is
	// a reused number, and a reused number is not the process.
	table(t, alive(), nil)
	got, _ = standingOf(ctx, reused, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, DeadElsewhere, got)

	// Two cases the table does not name, both toward reporting.
	got, _ = standingOf(ctx, elsewhere, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, LiveElsewhere, got, "no process table here can answer for another host")

	table(t, nil, errors.New("no ps on this machine"))
	got, _ = standingOf(ctx, deadPeer, nil, me(), "pass-1", map[int]liveness{})
	assert.Equal(t, LiveElsewhere, got, "could not find out is not dead")
}

// The seize list is Standing's table and nothing else, and it is a
// method so a new standing is a compile-time visit here rather than a
// missed case in the loop.
func TestSeizableIsTheTable(t *testing.T) {
	for _, s := range []Standing{Mine, EarlierPass, DeadElsewhere, Unattributed} {
		assert.True(t, s.Seizable(), "%d", s)
	}
	for _, s := range []Standing{StandingUnknown, LiveElsewhere, ForeignRoot} {
		assert.False(t, s.Seizable(), "%d", s)
	}
}

// A policy nobody chose seizes nothing, and the obligations come back
// untouched: a zero Grace would take seconds-old work and a zero
// Seizure{} would look chosen.
func TestDischargeRefusesAPolicyNobodyChose(t *testing.T) {
	st := newStore(t)
	obs := []Obligation{{Kind: Owed, Standing: Mine, Change: "chg-1", Platform: platformName}}
	back, err := Discharge(t.Context(), st, &verifytest.Fake{}, obs, Seizure{}, claimant(me()), at(0))
	require.ErrorIs(t, err, ErrSeizureUnset)
	assert.Equal(t, obs, back, "nothing was touched")
}

// The grace cutoff protects the window the liveness check cannot see: a
// live process's Requested lease whose Submit is still in flight.
func TestSecondsOldWorkIsLeftAloneUntilTheCutoff(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	stalled := &verifytest.Fake{SubmitErr: errors.New("i/o timeout")}
	_, err := Acquire(t.Context(), st, stalled, "chg-1", request(""), claimant(me()), at(-30*time.Second))
	require.Error(t, err)

	obs, err := Outstanding(t.Context(), st, stalled, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Requested, obs[0].Kind)
	require.Equal(t, Mine, obs[0].Standing)

	stands, err := Discharge(t.Context(), st, stalled, obs, seizure(5*time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1, "thirty seconds old under a five-minute cutoff")
	assert.True(t, leaseOn(t, st, "chg-1").Held(), "and nothing was claimed")
}

// A REQUESTED LEASE IS RESOLVED THROUGH THE PROVIDER, on the id the
// caller assigned, because it has no handle — that is the whole point of
// writing it before the call. Absent finishes it; Found hands the job to
// Fulfil; Unknown means it stands.
func TestARequestedLeaseIsResolvedThroughTheProvider(t *testing.T) {
	for _, tc := range []struct {
		name     string
		script   verify.RequestObservation
		finished bool
		released bool
		stands   int
	}{
		{"absent finishes it", verify.RequestObservation{State: verify.Absent}, true, false, 0},
		{"found releases it", verify.RequestObservation{State: verify.Found,
			Job: verify.Job{Provider: "fake", ID: "stranded-1"}}, true, true, 0},
		{"unknown leaves it standing", verify.RequestObservation{State: verify.Unknown}, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newStore(t)
			table(t, alive(), nil)
			plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
			fake := &verifytest.Fake{SubmitErr: errors.New("i/o timeout")}
			_, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(-time.Hour))
			require.Error(t, err)
			fake.SubmitErr = nil
			fake.Lookups = map[string]verify.RequestObservation{leaseOn(t, st, "chg-1").Request: tc.script}

			obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
			require.NoError(t, err)
			require.Len(t, obs, 1)

			stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
			require.NoError(t, err)

			l := leaseOn2(t, st, "chg-1")
			assert.Equal(t, tc.finished, l.Returned(), "returned")
			assert.Len(t, stands, tc.stands,
				"an obligation this pass did not CLOSE comes back standing, whatever it did to it")
			if tc.released {
				assert.Equal(t, []string{"stranded-1"}, fake.Released)
			} else {
				assert.Empty(t, fake.Released)
			}
			if !tc.finished {
				assert.True(t, l.Owed(), "the claim is held and the obligation stands for the next pass")
				require.NotNil(t, l.Release.NotBefore, "and it waits its turn rather than retrying every tick")
			}
		})
	}
}

// A failed release leaves an Owed obligation the next pass retries, on a
// backoff, and Discharge skips it until the backoff passes.
func TestAFailedReleaseStandsAndThenWaitsItsTurn(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished})
	held(t, st, "chg-1")
	fake := &verifytest.Fake{ReleaseErr: map[string]error{"fake-1": errors.New("tart is busy")}}

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Due, obs[0].Kind, "a guest a merged change left behind")

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1, "the provider refused, so nothing was closed and the obligation comes back")
	l := leaseOn(t, st, "chg-1")
	require.True(t, l.Owed(), "still owed, and now as the Owed kind")
	require.NotNil(t, l.Release.NotBefore)

	// The next tick, inside the backoff: reported as standing, and the
	// provider is not asked again.
	fake.ReleaseErr = nil
	again, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Equal(t, Owed, again[0].Kind)
	stands, err = Discharge(t.Context(), st, fake, again, seizure(time.Minute), claimant(me()), at(time.Minute))
	require.NoError(t, err)
	assert.Len(t, stands, 1)
	assert.Empty(t, fake.Released, "a refusing provider is not retried every tick")

	// And once the backoff has passed, it is retried and it succeeds.
	later := now.Add(10 * time.Minute)
	again, err = Outstanding(t.Context(), st, fake, me(), "pass-1", later)
	require.NoError(t, err)
	stands, err = Discharge(t.Context(), st, fake, again, seizure(time.Minute), claimant(me()), func() time.Time { return later })
	require.NoError(t, err)
	assert.Empty(t, stands)
	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.True(t, leaseOn2(t, st, "chg-1").Returned())
}

// A RESTARTED DISPATCHER DISCHARGES ITS PREDECESSOR'S LEASES. This is
// the case a draft's enum had no value for, so its Standing fell to the
// zero and Discharge refused it forever.
func TestARestartedDispatcherTakesOverItsPredecessorsObligation(t *testing.T) {
	st := newStore(t)
	dead := me()
	dead.PID = 200
	table(t, alive(), nil) // 200 is not in the table: it died
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	heldBy(t, st, "chg-1", dead)
	_, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(dead), now.Add(-time.Hour))
	require.NoError(t, err)
	require.True(t, took)

	fake := &verifytest.Fake{}
	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-2", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Owed, obs[0].Kind)
	require.Equal(t, DeadElsewhere, obs[0].Standing)

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Empty(t, stands)
	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.True(t, leaseOn2(t, st, "chg-1").Returned())
}

// A DISPATCHER ON ONE CHECKOUT NEVER DESTROYS ANOTHER CHECKOUT'S GUESTS,
// by either road into the decision: a lease whose owner names another
// root, and an untracked worker the provider attributes to one.
func TestAnotherCheckoutsWorkIsReportedAndNeverSeized(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	other := me()
	other.Root = "/elsewhere/ports"
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished})
	heldBy(t, st, "chg-1", other)
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "theirs", Owner: "/elsewhere/ports", Job: verify.Job{Provider: "fake", ID: "theirs"}},
	}}

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 2)
	for _, ob := range obs {
		assert.Equal(t, ForeignRoot, ob.Standing)
		assert.Equal(t, "/elsewhere/ports", ob.Root, "named, so a report can point at it")
	}

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(0), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 2, "returned untouched, with their Standing, for the report")
	assert.Empty(t, fake.Released)
}

// A guest nobody attributed is reported by the machine and seized only
// under a person's typed flag — never by a loop's own policy.
func TestAnUnattributedGuestNeedsAPersonsFlag(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "nobodys", Job: verify.Job{Provider: "fake", ID: "nobodys"}},
	}}
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error { return nil }))

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Unattributed, obs[0].Standing)

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1, "a loop's policy does not take it")
	assert.Empty(t, fake.Released)

	pol := seizure(time.Minute)
	pol.Unattributed = true
	stands, err = Discharge(t.Context(), st, fake, obs, pol, claimant(me()), at(0))
	require.NoError(t, err)
	assert.Empty(t, stands)
	assert.Equal(t, []string{"nobodys"}, fake.Released)
}

// An untracked worker is released through verify.Worker.Job and never
// through a name: that a job's id is the environment's name is one
// backend's fact. A backend that named no job is asked nothing and said
// so.
func TestAnUntrackedWorkerWithNoJobIsAskedNothing(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	fake := &verifytest.Fake{Live: []verify.Worker{{Name: "unreleasable", Owner: me().Root}}}
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error { return nil }))

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Mine, obs[0].Standing)

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1)
	assert.Empty(t, fake.Released)
}

// A CONCURRENT PASS CANNOT DELETE A RUNNING BUILD'S VM. Even where the
// record makes the lease Due — a merged change's kept guest — the claim
// is refused while a subject is still building in it.
func TestADischargeCannotTakeAGuestASubjectIsStillBuildingIn(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished})
	held(t, st, "chg-1")
	plantAttempt(t, st, record.Attempt{ID: "att-1", Change: "chg-1", Platform: platformName,
		Sha: "deadbeef", Phase: record.Active, Lease: "held"})
	fake := &verifytest.Fake{}

	obs, err := Outstanding(t.Context(), st, fake, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)

	stands, err := Discharge(t.Context(), st, fake, obs, seizure(0), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1)
	assert.Empty(t, fake.Released, "the build is live; the guest is not free")
}

// An obligation whose kind nobody set is refused rather than guessed at,
// which is what a refusing zero is for when the act on the other side
// destroys a virtual machine.
func TestAnObligationWithNoKindIsRefused(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	fake := &verifytest.Fake{}
	obs := []Obligation{{Standing: Mine, Since: now.Add(-time.Hour), Worker: "w",
		Job: verify.Job{Provider: "fake", ID: "w"}}}
	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1)
	assert.Empty(t, fake.Released)
}

// An obligation whose age this pass could not establish is reported and
// never seized: an unknown age must not read as an old one.
func TestAnObligationOfUnknownAgeIsNeverSeized(t *testing.T) {
	assert.False(t, mayTake(Obligation{Kind: Owed, Standing: Mine}, seizure(time.Minute), now))
	assert.True(t, mayTake(Obligation{Kind: Owed, Standing: Mine, Since: now.Add(-time.Hour)}, seizure(time.Minute), now))
	assert.True(t, mayTake(Obligation{Kind: Untracked, Standing: Mine}, seizure(time.Minute), now),
		"an untracked worker has no age to measure, and the read order is what protects it")
}

// A change with no record at all is not a closed change. A compacted or
// hand-deleted record is a missing fact, and treating a missing fact as
// permission to destroy an environment is D22's shape.
func TestALeaseForAChangeWithNoRecordIsNotDue(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	held(t, st, "chg-1") // no change record planted
	obs, err := Outstanding(t.Context(), st, &verifytest.Fake{}, me(), "pass-1", now)
	require.NoError(t, err)
	assert.Empty(t, obs)
}

// Discharge reads the record before it acts, and a record it cannot read
// stops it for Outstanding's reason: this pass cannot account for
// anything on the machine, and what it was about to do is destroy
// provider resources.
func TestDischargeRefusesWhenItCannotReadTheRecord(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{}
	obs := []Obligation{{Kind: Owed, Standing: Mine, Change: "chg-1", Platform: platformName,
		Since: now.Add(-time.Hour)}}
	stands, err := Discharge(t.Context(), st, fake, obs, seizure(time.Minute), claimant(me()), at(0))
	require.ErrorIs(t, err, statestore.ErrNoState)
	assert.Equal(t, obs, stands, "nothing was touched")
	assert.Empty(t, fake.Released)
}

// A backend that cannot be asked what became of a request id cannot
// offer unattended verification, and the obligation stands for a reason
// a person can act on rather than being silently retried forever.
func TestARequestedLeaseStandsWhenTheBackendCannotBeAsked(t *testing.T) {
	st := newStore(t)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})
	fake := &verifytest.Fake{SubmitErr: errors.New("i/o timeout")}
	_, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(-time.Hour))
	require.Error(t, err)

	obs, err := Outstanding(t.Context(), st, verifytest.Incapable{Fake: fake}, me(), "pass-1", now)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	require.Equal(t, Requested, obs[0].Kind)

	stands, err := Discharge(t.Context(), st, verifytest.Incapable{Fake: fake}, obs,
		seizure(time.Minute), claimant(me()), at(0))
	require.NoError(t, err)
	assert.Len(t, stands, 1)
	assert.True(t, leaseOn(t, st, "chg-1").Held(), "nothing was claimed and nothing was destroyed")
}
