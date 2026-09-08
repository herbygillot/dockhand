package publish

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder every fixture opens with. The tests that touch a
// store drive a REAL statestore over a REAL repository, for the reason
// lease's and run's own tests do: what a mutator proves is an ordering
// of writes against a compare-and-set, and a fake store would prove
// something about the fake.
var tools = tool.NewFinder(nil)

// clock is the one instant every fixture is stamped at, so a test that
// asserts about a window is asserting about the window and not about
// when it ran.
var clock = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *statestore.Store {
	t.Helper()
	return statestore.Open(gittest.PortsTree(t, tools))
}

// facts is a fact set that PASSES the whole ladder for a person: a
// verified single-subject bump, a fresh forge with no pull request of
// its own and no duplicate, no hold, no drift. Every test below is one
// mutation away from it, so what a test is about is the line it changes.
func facts(mut ...func(*Facts)) Facts {
	f := Facts{
		Change: record.Change{
			ID: "chg-1", State: record.ChangeMinted, Branch: "dockhand/jq-1.8",
			Tip: "aaaa", Content: "tree-1", Destination: record.ToPublished,
			Subjects: []record.Subject{{Port: "jq", Portdir: "devel/jq", Intent: "bump", Target: "1.8"}},
			Base:     record.Base{Sha: "base", CommittedAt: clock.Add(-24 * time.Hour)},
		},
		Attempts: []record.Attempt{{
			ID: "att-1", Change: "chg-1", Sha: "aaaa", Content: "tree-1",
			Platform: "Sequoia", Phase: record.Finished, Started: clock.Add(-time.Hour),
			Runs: map[string]record.Run{"jq": {State: record.Passed, Content: "tree-1", At: clock}},
		}},
		Branch: "dockhand/jq-1.8",
		Tip:    "aaaa",
		Own:    []string{"aaaa"},
		Forge: ForgeFacts{
			Upstream: "macports/macports-ports", ForkRemote: "fork", ForkOwner: "me",
			Fresh: true, Asked: true, AsOf: clock,
		},
		Direction: Direction{
			Movement: macports.Movement{Compared: true, Moved: true, Upgrades: true},
			AsOf:     clock,
		},
		Invoker:   record.Human,
		Drift:     change.Drift{Compared: true},
		Spent:     Spent(emptyState(), clock),
		Title:     "jq: update to 1.8",
		Body:      "a body",
		BodyLimit: gh.MaxPRBody,
		AsOf:      clock,
	}
	for _, m := range mut {
		m(&f)
	}
	return f
}

// machine is the same fact set as the unattended road sees it: the
// invoker, the grant the 2026-09-06 ruling gives, and the simplicity
// verdict the caller recomputed over the realized change.
func machine(f *Facts) {
	f.Invoker = record.Machine
	f.Unattended = GrantSimpleBumps
	f.Simplicity = change.Simple
}

func emptyState() statestore.State {
	return statestore.State{
		At:           "state-commit",
		Changes:      map[string]record.Change{},
		Attempts:     map[string]record.Attempt{},
		Leases:       map[string]record.Lease{},
		Publications: map[string]record.Publication{},
	}
}

// openPR is a pull request the forge reports as this branch's own, at
// the sha given.
func openPR(number int, sha string) gh.PullRequest {
	return gh.PullRequest{
		Number: number, Title: "jq: update to 1.8", State: "open",
		HTMLURL: "https://example.invalid/pull/1", Head: gh.PRHead{Ref: "dockhand/jq-1.8", Sha: sha},
	}
}

// A PERMIT NOBODY GRANTED IS NOT A PERMIT, and that is the whole of
// "Apply is uncallable without one". Unexported fields stop a caller
// inventing a permit with steps in it; nothing in Go stops the zero
// composite literal, so the flag Authorize sets is what closes it — and
// Apply refuses before it has looked at a repository, a forge or a
// store, all three of which are nil here.
func TestApplyRefusesAPermitAuthorizeDidNotGrant(t *testing.T) {
	out, err := Apply(t.Context(), Env{}, Permit{})
	require.ErrorIs(t, err, ErrNoPermit)
	assert.Empty(t, out.Completed)
}

// THE ZERO SPEND SAYS "NOBODY COUNTED" AND NOT "NOTHING SPENT". Every
// other fact in the roll call fails closed when nobody filled it in;
// this one would fail OPEN as a bare integer — a forged zero admits the
// whole cap — so the constructor is the only thing that sets counted,
// and the machine road refuses without it.
func TestAZeroSpendIsNotACountOfZero(t *testing.T) {
	var never Spend
	assert.False(t, never.Counted())
	assert.Zero(t, never.Within(6*time.Hour, clock))

	counted := Spent(emptyState(), clock)
	assert.True(t, counted.Counted())
	assert.Equal(t, "state-commit", counted.At())
	assert.Zero(t, counted.Within(6*time.Hour, clock))

	_, _, err := Authorize(facts(machine, func(f *Facts) { f.Spent = never }), DefaultPace)
	require.ErrorIs(t, err, ErrSpendUnknown)
}

// WHAT THE ALLOWANCE IS COUNTED OVER: a machine's OPENING of a pull
// request, whether the forge confirmed it (Finished) or the process died
// before the answer came back (Uncertain, rule 7 — a crash between the
// push and the answer may have opened one).
//
// A REFRESH NEVER COUNTS, and neither does a person's publication. An
// adversarial pass priced the first: a dispatcher refreshing every open
// pull request on every tick spends the whole 20/6h allowance on
// refreshes inside two hours and then refuses real publications for the
// rest of the window.
func TestSpentCountsAMachinesOpeningsAndNothingElse(t *testing.T) {
	at := func(d time.Duration) time.Time { return clock.Add(d) }
	s := emptyState()
	s.Publications = map[string]record.Publication{
		"a": {ID: "a", By: record.Machine, Steps: []record.Step{
			{Kind: record.PushBranch, Phase: record.Finished, At: at(-time.Hour)},
			{Kind: record.OpenPR, Phase: record.Finished, At: at(-time.Hour)},
		}},
		"b": {ID: "b", By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Uncertain, At: at(-2 * time.Hour)},
		}},
		"c": {ID: "c", By: record.Machine, Steps: []record.Step{
			{Kind: record.RefreshPR, Phase: record.Finished, At: at(-time.Minute)},
		}},
		"d": {ID: "d", By: record.Human, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: at(-time.Minute)},
		}},
		"e": {ID: "e", By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: at(-7 * time.Hour)},
		}},
		"f": {ID: "f", By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: at(-30 * time.Hour)},
		}},
	}
	spend := Spent(s, clock)
	// Inside the pace's own six hours: a and b. c is a refresh, d is a
	// person's, e is seven hours old and f is past the floor entirely.
	assert.Equal(t, 2, spend.Within(6*time.Hour, clock))
	// The derivation itself collects everything inside MaxWindow, so a
	// longer window than the pace's finds the older row without a second
	// read — which is why Gather needs no Pace to derive this.
	assert.Equal(t, 3, spend.Within(MaxWindow, clock))
	// And nothing beyond the floor is kept at all: statestore.Compact may
	// drop those rows, so counting them would be counting what the store
	// no longer promises to hold.
	assert.Equal(t, 3, spend.Within(48*time.Hour, clock))
}

// THE ALLOWANCE IS THE DURABLE COUNT, and the refusal it produces is the
// one Cycle breaks its slot on.
func TestTheMachineIsRefusedWhenItsAllowanceIsSpent(t *testing.T) {
	s := emptyState()
	s.Publications = map[string]record.Publication{}
	for i := range DefaultPace.Max {
		id := string(rune('a' + i))
		s.Publications[id] = record.Publication{ID: id, By: record.Machine, Steps: []record.Step{
			{Kind: record.OpenPR, Phase: record.Finished, At: clock.Add(-time.Minute)},
		}}
	}
	_, _, err := Authorize(facts(machine, func(f *Facts) { f.Spent = Spent(s, clock) }), DefaultPace)
	require.ErrorIs(t, err, ErrPaceSpent)

	// A person is never paced: Promote hands in a zero Pace and is never
	// asked, over the very same store.
	_, _, err = Authorize(facts(func(f *Facts) { f.Spent = Spent(s, clock) }), Pace{})
	require.NoError(t, err)
}

// A PACE NOBODY CHOSE IS A WIRING GAP AND NOT A WITHHOLDING (rule 7).
// --no-publish is how a person says "publish nothing" out loud, and
// --publish-max 0 is a usage error cli refuses; a zero Pace reaching
// this road means somebody forgot to wire one.
func TestTheMachineIsRefusedAPaceNobodyChose(t *testing.T) {
	_, _, err := Authorize(facts(machine), Pace{})
	require.ErrorIs(t, err, ErrPaceUnset)
}

// R5'S NUMBERS, SPELLED ONCE.
func TestDefaultPaceIsTwentyPerSixHours(t *testing.T) {
	assert.True(t, DefaultPace.Set)
	assert.Equal(t, 20, DefaultPace.Max)
	assert.Equal(t, 6*time.Hour, DefaultPace.Window)
	// The floor Compact keeps machine rows above has to cover any window a
	// decision can ask for, since cli refuses a longer --publish-every.
	assert.GreaterOrEqual(t, MaxWindow, DefaultPace.Window)
}
