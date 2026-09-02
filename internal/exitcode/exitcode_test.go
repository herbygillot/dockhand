package exitcode

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every code in the contract, with the family it belongs to. The table
// is written out rather than computed so that a constant moved into
// the wrong decade fails here, where the contract is stated, instead of
// being classified by the same arithmetic that put it there.
func TestFamilyNamesEveryDeclaredCode(t *testing.T) {
	for _, tc := range []struct {
		code   int
		family string
	}{
		{OK, "success"},
		{Failure, "failure"},
		{Usage, "usage"},
		{PlanDeclined, "declined"},
		{BranchInFlight, "declined"},
		{AlreadyCurrent, "declined"},
		{Ambiguous, "declined"},
		{DuplicatePR, "refused"},
		{PRMerged, "refused"},
		{Superseded, "refused"},
		{Held, "refused"},
		{MachineGate, "refused"},
		{NoMacPorts, "environment"},
		{EvalStartup, "environment"},
		{RootRefused, "environment"},
		{ToolMissing, "environment"},
		{NoVerifyEnv, "environment"},
		{ProvisionFailed, "environment"},
		{VerifierBusy, "environment"},
		{NotPortsTree, "tree"},
		{PortNotFound, "tree"},
		{NotARepo, "tree"},
		{Drift, "tree"},
		{BranchNotFound, "tree"},
		{FetchFailed, "upstream"},
		{WitnessUnreachable, "upstream"},
		{WitnessAPI, "upstream"},
		{LatestUnresolved, "upstream"},
		{VerifyQueued, "pending"},
		{VerifyAwaitingSlot, "pending"},
		{PromotionPending, "pending"},
		{VerifyFailed, "verdict"},
		{VerifyBlocked, "verdict"},
		{VerifyUnsupported, "verdict"},
		{VerifyErrored, "verdict"},
		{MintedSubmitErrored, "partial"},
		{PushedPRFailed, "partial"},
		{PRRefreshFailed, "partial"},
		{SweepHardErrors, "partial"},
	} {
		t.Run(fmt.Sprintf("%d", tc.code), func(t *testing.T) {
			assert.Equal(t, tc.family, Family(tc.code))
		})
	}
}

// The decade is the family, which is what makes `case $?/10` a script
// someone writes once: a code added to a band later is classified
// before anyone has heard of it.
func TestFamilyIsTheDecade(t *testing.T) {
	assert.Equal(t, "declined", Family(19), "an unassigned 1x is still a decline")
	assert.Equal(t, "refused", Family(29))
	assert.Equal(t, "environment", Family(39))
	assert.Equal(t, "tree", Family(49))
	assert.Equal(t, "upstream", Family(59))
	assert.Equal(t, "pending", Family(69))
	assert.Equal(t, "verdict", Family(79))
	assert.Equal(t, "partial", Family(89))
}

// Outside the contract there is no family, and saying so with an empty
// string beats guessing at the nearest band.
func TestFamilyOutsideTheContract(t *testing.T) {
	for _, code := range []int{3, 4, 5, 6, 7, 8, 9, 90, 99, 255} {
		assert.Empty(t, Family(code), "code %d", code)
	}
}

// coded owns a band and names itself: the shape every typed refusal in
// dockhand has after S6.
type coded struct {
	code   int
	reason string
}

func (e *coded) Error() string     { return "coded" }
func (e *coded) DockhandExit() int { return e.code }
func (e *coded) Code() string      { return e.reason }

// banded owns a band and does not name itself — a typed error that has
// not been given a reason yet.
type banded struct{ code int }

func (e *banded) Error() string     { return "banded" }
func (e *banded) DockhandExit() int { return e.code }

func TestOfDerivesTheFamily(t *testing.T) {
	tw := Of(DuplicatePR, "duplicate-pr")
	assert.Equal(t, Twin{Code: 20, Family: "refused", Reason: "duplicate-pr"}, tw)
}

func TestTwinOf(t *testing.T) {
	assert.Equal(t, Twin{Code: 0, Family: "success"}, TwinOf(nil))

	assert.Equal(t, Twin{Code: 1, Family: "failure"}, TwinOf(errors.New("boom")),
		"an error with no band is the band of last resort; cmd's sentinel table answers the rest")

	assert.Equal(t, Twin{Code: 10, Family: "declined", Reason: "already-current"},
		TwinOf(&coded{code: PlanDeclined, reason: "already-current"}))

	assert.Equal(t, Twin{Code: 70, Family: "verdict"}, TwinOf(&banded{code: VerifyFailed}),
		"a band with no reason is a twin with no reason, never an invented one")
}

// The twin is read through the wrap chain, because the error a verb
// returns is rarely the error a package raised.
func TestTwinOfReadsThroughWrapping(t *testing.T) {
	err := fmt.Errorf("promote: %w", &coded{code: PRMerged, reason: "pr-merged"})
	assert.Equal(t, Twin{Code: 21, Family: "refused", Reason: "pr-merged"}, TwinOf(err))
}

// The twin and the status are the same answer, so the code TwinOf
// reports is the code the Coder would have exited with — never a
// second opinion computed beside it.
func TestTwinOfAgreesWithTheCoder(t *testing.T) {
	err := &coded{code: VerifierBusy, reason: "verifier-busy"}
	var coder Coder
	require.ErrorAs(t, error(err), &coder)
	assert.Equal(t, coder.DockhandExit(), TwinOf(err).Code)
}
