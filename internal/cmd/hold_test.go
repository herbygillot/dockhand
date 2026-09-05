package cmd

// The hold verbs and the superseded sweep at the command layer: the
// wiring, the flag, and the two refusals a script branches on.
//
// What a hold MEANS, and every road that obeys one, is proven in
// internal/engine. What is proven here is that the verbs reach it — a
// gate nothing invokes is a gate nobody has.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// holdState is a lifecycle repo and a run over it, with both streams in
// hand.
func holdState(t *testing.T) (*git.Repo, string, *runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	repo, sha := lifecycleRepo(t)
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: out, Err: errb}
	return repo, sha, rs, out, errb
}

func TestHoldAndUnholdThroughTheVerbs(t *testing.T) {
	ctx := context.Background()
	repo, sha, rs, out, errb := holdState(t)

	require.NoError(t, holdAction{target: "jq", reason: "waiting on upstream"}.Execute(ctx, rs))
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	require.NotNil(t, n.Hold)
	assert.Equal(t, "waiting on upstream", n.Hold.Reason)
	assert.False(t, n.Hold.At.IsZero(), "the verb reads the clock; record never does")
	assert.Contains(t, out.String(), "held dockhand/jq-1.8")
	assert.Contains(t, errb.String(), "nothing will publish, verify or retire it")

	require.NoError(t, unholdAction{target: "jq"}.Execute(ctx, rs))
	n, err = ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Nil(t, n.Hold)
}

// The two refusals, as a script sees them: 23 for a hold standing in the
// way, 10 for a release with nothing to release.
func TestTheHoldVerbsExitInTheirBands(t *testing.T) {
	ctx := context.Background()
	_, _, rs, _, _ := holdState(t)

	err := unholdAction{target: "jq"}.Execute(ctx, rs)
	var not *engine.NotHeldError
	require.ErrorAs(t, err, &not)
	assert.Equal(t, exitcode.PlanDeclined, exitcode.TwinOf(err).Code)

	require.NoError(t, holdAction{target: "jq", reason: "first"}.Execute(ctx, rs))
	err = holdAction{target: "jq", reason: "second"}.Execute(ctx, rs)
	var held *engine.HeldError
	require.ErrorAs(t, err, &held)
	twin := exitcode.TwinOf(err)
	assert.Equal(t, exitcode.Held, twin.Code)
	assert.Equal(t, "refused", twin.Family)
	assert.Equal(t, "held", twin.Reason)
}

// The flag exists, defaults off, and takes no argument. A `--superseded`
// that had drifted into taking a value would be a usage error at every
// call site that spells it the documented way. It came to `cycle` from
// `clean` (D27) with its shape intact.
func TestCycleDeclaresTheSupersededFlag(t *testing.T) {
	f := Cycle().Flags().Lookup("superseded")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Equal(t, "bool", f.Value.Type())
}

// Without the flag, `cycle` leaves a superseded branch alone; with it,
// the branch goes. That pairing is the ruling: nothing else in the tool
// removes a branch for having been superseded.
func TestCycleRemovesSupersededOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	repo, sha, rs, out, _ := holdState(t)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	rs.Gh = func(context.Context, ...string) (string, error) { return "[]", nil }

	// Superseded by a sibling that does not have to exist as a branch for
	// the field to mean what it says: the record names the newer one.
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq"}}
	n.SupersededBy = "dockhand/jq-1.9"
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	require.NoError(t, cycleAction{}.Execute(ctx, rs))
	_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err, "the plain pass asks about merges and nothing else")

	out.Reset()
	require.NoError(t, cycleAction{superseded: true}.Execute(ctx, rs))
	assert.Contains(t, out.String(), "superseded by dockhand/jq-1.9",
		"the sweep says what it removed and why")
	_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.Error(t, err, "and with the flag it is gone")
}
