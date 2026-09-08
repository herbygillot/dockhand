package tart

import (
	"errors"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The classification is the whole capability: a purge asks this
// provider what each VM on the machine IS, and everything downstream
// decides from the kind rather than from the name. A golden read as a
// base here is a reference image destroyed by a verb whose contract
// says it keeps them.
func TestHoldingsClassifyEveryRoleDockhandNames(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	stubWorkers(t, strings.Join([]string{
		BaseName(seq),
		GoldenName(seq),
		"dockhand-worker-1",
		"dockhand-probe-abc",
		"unrelated-vm",
		"",
	}, "\n"), nil)

	got, err := Provider{Tools: tools}.Holdings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []verify.Holding{
		{Name: "dockhand-base-sequoia", Kind: verify.HeldDerived},
		{Name: "dockhand-golden-sequoia", Kind: verify.HeldReference},
		{Name: "dockhand-worker-1", Kind: verify.HeldWorker,
			Job: verify.Job{Provider: "tart", ID: "dockhand-worker-1", Request: "1"}},
		{Name: "dockhand-probe-abc", Kind: verify.HeldScratch},
	}, got, "each role classified, and only a worker carries a job")
}

// A VM this provider did not name is not a holding. The listing is the
// whole machine's, and reporting a person's own guest here would put it
// in front of a sweep whose policy is "remove what is removable".
func TestHoldingsOmitVMsDockhandDidNotName(t *testing.T) {
	stubWorkers(t, "unrelated-vm\nsomeone-elses-builder\n", nil)
	got, err := Provider{Tools: tools}.Holdings(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Rule 7 at the listing: a machine that would not answer has told this
// caller nothing, and estate.Survey turns that into ErrNoEstate rather
// than an empty estate.
func TestHoldingsRefuseWhenTheMachineWillNotAnswer(t *testing.T) {
	stubWorkers(t, "", errors.New("tart: command not found"))
	_, err := Provider{Tools: tools}.Holdings(t.Context())
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
}

// Prefix and not substring, on workerNames' own precedent: a name that
// merely CONTAINS a role's prefix is not that role, and a golden must
// never be reachable through the base's prefix.
func TestHoldingKindMatchesAPrefixAndNotASubstring(t *testing.T) {
	for _, tc := range []struct {
		vm   string
		kind verify.HoldingKind
		ours bool
	}{
		{"dockhand-worker-1", verify.HeldWorker, true},
		{"dockhand-probe-1", verify.HeldScratch, true},
		{"dockhand-base-sequoia", verify.HeldDerived, true},
		{"dockhand-golden-sequoia", verify.HeldReference, true},
		{"my-dockhand-base-copy", verify.HoldingUnknown, false},
		{"dockhand-something-else", verify.HoldingUnknown, false},
		{"", verify.HoldingUnknown, false},
	} {
		kind, ours := holdingKind(tc.vm)
		assert.Equal(t, tc.kind, kind, tc.vm)
		assert.Equal(t, tc.ours, ours, tc.vm)
	}
}

// The provider's own refusal, which is not the caller's policy: a
// golden is the one image here that cannot be rebuilt without leaving
// the machine, and the backend that knows that says so before any
// `tart delete` is reached for.
func TestDiscardRefusesAReferenceCopy(t *testing.T) {
	err := Provider{Tools: tools}.Discard(t.Context(),
		verify.Holding{Name: "dockhand-golden-sequoia", Kind: verify.HeldReference})
	require.ErrorIs(t, err, verify.ErrKept)
	require.ErrorContains(t, err, "dockhand-golden-sequoia", "and it names what it would not remove")
}

// The refusing zero reaches the destructive verb too. A holding nobody
// classified is not a removable one, so a value built and not populated
// survives rather than being deleted by a default.
func TestDiscardRefusesAnUnclassifiedHolding(t *testing.T) {
	err := Provider{Tools: tools}.Discard(t.Context(), verify.Holding{Name: "dockhand-base-sequoia"})
	require.ErrorIs(t, err, verify.ErrKept)
}

func TestHoldingKindsSayWhatTheyAre(t *testing.T) {
	assert.Equal(t, "worker", verify.HeldWorker.String())
	assert.Equal(t, "reference image", verify.HeldReference.String())
	assert.Equal(t, "unclassified", verify.HoldingUnknown.String(),
		"a kind nobody set says so rather than printing a number")
}
