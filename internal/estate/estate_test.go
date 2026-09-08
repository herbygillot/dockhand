package estate

import (
	"context"
	"errors"
	"testing"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func held(name string, kind verify.HoldingKind) verify.Holding {
	return verify.Holding{Name: name, Kind: kind, Job: verify.Job{Provider: "tart", ID: name}}
}

// notAKeeper is a Verifier that does not implement verify.Keeper — a
// backend that keeps nothing it can enumerate, which is a real shape
// (a hosted CI provider) and not a broken one.
type notAKeeper struct{ verify.Verifier }

func TestSurveySortsSoTwoReadingsAgree(t *testing.T) {
	f := &verifytest.Fake{Held: []verify.Holding{
		held("dockhand-worker-b", verify.HeldWorker),
		held("dockhand-base-sequoia", verify.HeldDerived),
		held("dockhand-worker-a", verify.HeldWorker),
	}}
	got, err := Survey(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-base-sequoia", "dockhand-worker-a", "dockhand-worker-b"}, Names(got))
}

// Rule 7, and the reason this package exists rather than a bare type
// assertion at each call site: "I could not look" and "there is nothing
// here" justify opposite next steps, and the caller is about to destroy
// what the listing names.
func TestSurveySaysWhenItCouldNotAsk(t *testing.T) {
	for name, prov := range map[string]verify.Verifier{
		"no provider at all": nil,
		"not a keeper":       notAKeeper{},
		"listing failed":     &verifytest.Fake{HoldingsErr: errors.New("tart: command not found")},
	} {
		_, err := Survey(context.Background(), prov)
		require.ErrorIsf(t, err, ErrNoEstate, "%s", name)
	}
}

func TestAProviderThatCanListAndHoldsNothingIsADifferentAnswer(t *testing.T) {
	got, err := Survey(context.Background(), &verifytest.Fake{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSweepRemovesWhatIsRemovableAndKeepsTheRest(t *testing.T) {
	f := &verifytest.Fake{}
	swept, err := Sweep(context.Background(), f, []verify.Holding{
		held("dockhand-worker-a", verify.HeldWorker),
		held("dockhand-probe-1", verify.HeldScratch),
		held("dockhand-base-sequoia", verify.HeldDerived),
		held("dockhand-golden-sequoia", verify.HeldReference),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-worker-a", "dockhand-probe-1", "dockhand-base-sequoia"}, swept.Removed)
	assert.Equal(t, []string{"dockhand-golden-sequoia"}, swept.Kept)
	assert.Equal(t, []string{"dockhand-worker-a", "dockhand-probe-1", "dockhand-base-sequoia"}, f.Discarded,
		"and the reference copy was never even offered to the provider")
}

// The refusing zero at the boundary where the act is irreversible: a
// holding nobody classified is kept, so every mistake here fails toward
// a resource that survives rather than one destroyed by a default.
func TestSweepKeepsAnUnclassifiedHolding(t *testing.T) {
	f := &verifytest.Fake{}
	swept, err := Sweep(context.Background(), f, []verify.Holding{{Name: "mystery"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"mystery"}, swept.Kept)
	assert.Empty(t, f.Discarded)
}

// A listing that straddled somebody else's removal is the ordinary case
// rather than a fault: the job here is that the named resources are
// gone when this returns, not that this call is what removed them.
func TestSweepTreatsAnAlreadyGoneHoldingAsGone(t *testing.T) {
	f := &verifytest.Fake{DiscardErr: map[string]error{"dockhand-worker-a": verify.ErrUnknownJob}}
	swept, err := Sweep(context.Background(), f, []verify.Holding{held("dockhand-worker-a", verify.HeldWorker)})
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-worker-a"}, swept.Removed)
}

// A provider refusing to destroy a reference copy has AGREED with the
// policy, so it belongs in Kept beside the ones the policy withheld —
// never in the failures.
func TestSweepCountsAProvidersOwnRefusalAsKept(t *testing.T) {
	f := &verifytest.Fake{DiscardErr: map[string]error{"dockhand-base-sequoia": verify.ErrKept}}
	swept, err := Sweep(context.Background(), f, []verify.Holding{held("dockhand-base-sequoia", verify.HeldDerived)})
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-base-sequoia"}, swept.Kept)
	assert.Empty(t, swept.Removed)
}

// One stuck guest must not hide the nine that went, and the nine are
// still reported in the value beside the error.
func TestSweepCollectsFailuresAndStillReportsWhatWent(t *testing.T) {
	f := &verifytest.Fake{DiscardErr: map[string]error{"dockhand-worker-b": errors.New("still running")}}
	swept, err := Sweep(context.Background(), f, []verify.Holding{
		held("dockhand-worker-a", verify.HeldWorker),
		held("dockhand-worker-b", verify.HeldWorker),
		held("dockhand-worker-c", verify.HeldWorker),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "dockhand-worker-b")
	assert.Equal(t, []string{"dockhand-worker-a", "dockhand-worker-c"}, swept.Removed)
}

func TestSweepRefusesAProviderItCannotAsk(t *testing.T) {
	_, err := Sweep(context.Background(), notAKeeper{}, nil)
	require.ErrorIs(t, err, ErrNoEstate)
}

func TestRemovableIsTheSweepsPopulationWithoutSweeping(t *testing.T) {
	all := []verify.Holding{
		held("dockhand-worker-a", verify.HeldWorker),
		held("dockhand-golden-sequoia", verify.HeldReference),
	}
	assert.Equal(t, []string{"dockhand-worker-a"}, Names(Removable(all)))
}
