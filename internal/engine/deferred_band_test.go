package engine

// The bands a deferred verification exits in. "The run did not start"
// is four different answers — a full machine that will free on its
// own, a release nobody has provisioned, a request the provider cannot
// meet, and a submit that broke after the branch was minted — and they
// all used to be one. A user waiting on a queue was told to go and fix
// something; a user whose provider refused the request was told to
// wait.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestDeferralBandsReadItsCause(t *testing.T) {
	const branch = "dockhand/jq-1.8"
	later := func(cause error) *VerifyDeferredError {
		return &VerifyDeferredError{Branch: branch, Reason: cause.Error(), Cause: cause}
	}
	for _, tc := range []struct {
		name   string
		cause  error
		want   int
		reason string
	}{
		{"a full machine is pending, and status comes back for it",
			&verify.CapacityError{Busy: 2, Cap: 2}, exitcode.VerifyQueued, "verify-queued"},
		{"no environment is queued against the provisioning that frees it",
			fmt.Errorf("%w: no base images", verify.ErrNoEnvironment), exitcode.VerifyAwaitingSlot, "verify-awaiting-slot"},
		{"a capability refusal is a verdict; nothing will free it",
			fmt.Errorf("%w: not a portdir", verify.ErrUnsupported), exitcode.VerifyUnsupported, "verification-unsupported"},
		{"anything else broke after the mint, and half the work stands",
			errors.New("the agent never answered"), exitcode.MintedSubmitErrored, "minted-submit-errored"},
		{"the multi-release summary carries no cause and every run it counts is recorded",
			nil, exitcode.VerifyQueued, "verify-queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := later(errors.New("placeholder"))
			e.Cause = tc.cause
			assert.Equal(t, tc.want, e.DockhandExit())
			assert.Equal(t, tc.reason, e.Code())
		})
	}

	// A deferral is by definition nobody standing there, so it never
	// answers the synchronous refusal even when the cause could carry
	// it: a run was recorded, and status will come back for it.
	sync := later(&verify.CapacityError{Busy: 2, Cap: 2, Synchronous: true})
	assert.Equal(t, exitcode.VerifyQueued, sync.DockhandExit(),
		"a deferred submit is asynchronous whatever the cause was stamped with")
	assert.Equal(t, "verify-queued", sync.Code(),
		"and the reason says the same thing the band does")
}

func TestSubmitWithoutAProviderNarrowsTheContract(t *testing.T) {
	// Ruling 8, pinned: the implicit submit inside a write intent meets
	// a machine with no tart and exits ZERO with its note. The branch
	// is unverified and may be promoted as it is, which is the contract
	// narrowing rather than failing — and the explicit verbs asking for
	// the same provider exit in the machine band, which the cmd exit
	// table holds.
	repo, sha := engineRepo(t)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, nil, &out, &errb)
	eng.Verifier = func(context.Context) (verify.Verifier, error) {
		return nil, verify.NoProvider("tart is not installed (`port install tart`); --no-verify skips verification")
	}
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	err := eng.submit(context.Background(), m, submission{
		Port: "jq", Release: platform.Release{Name: "Testos", Darwin: 99}})

	require.NoError(t, err, "no provider, no contract: the branch stands and the run succeeded")
	assert.Contains(t, errb.String(), "no verification possible")
	assert.Contains(t, errb.String(), "you may promote it as is")
}

func TestADeferredSubmitRecordsWhatStatusWillRetry(t *testing.T) {
	// The note is what makes a deferral recoverable: status finds the
	// recorded run and starts it when the obstacle clears. A deferral
	// that wrote nothing left the tip reading as bare "unverified",
	// with the reason only in scrollback — field-measured.
	repo, sha := engineRepo(t)
	ctx := context.Background()
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{
		SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2},
	}, &out, &errb)
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	err := eng.submit(ctx, m, submission{
		Port: "jq", Release: platform.Release{Name: "Testos", Darwin: 99}})

	var deferred *VerifyDeferredError
	require.ErrorAs(t, err, &deferred)
	assert.Equal(t, exitcode.VerifyQueued, deferred.DockhandExit())
	n, rerr := eng.Ledger(repo).Read(ctx, sha)
	require.NoError(t, rerr)
	assert.Equal(t, record.Queued, runOf(n, "Testos").State,
		"status retries what it finds recorded, so the reason has to be on the note")
	assert.Empty(t, n.Jobs, "nothing was submitted, so no guest is claimed")
}
