package lease

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// loseTheStateRefOnce makes the NEXT `git update-ref` batch this
// repository runs lose its race, exactly once, and leaves the state ref
// at to.
//
// It is git's own reference-transaction hook and not a goroutine,
// because what is being proven here is an ordering — the peer's write
// lands between the closure's read and the batch's compare-and-set — and
// a sleeping goroutine would prove it on the runs where it happened to
// win. The hook refuses the batch in its `prepared` phase and moves the
// ref in the `aborted` phase that follows, by which point git has
// released the ref's lock (measured on git 2.55); statestore's
// UpdateRefs then classifies the refusal by RE-READING, finds the state
// ref somewhere else, and returns it as the retryable *git.RefMoved that
// Amend runs the closure again over. That is the same lost race
// statestore's own TestAmendRetriesWhenTheStateRefLostItsRace stages
// from inside its closure — staged from outside here, because this
// package's closures are not the test's to write.
//
// Exactly once: the marker file the hook writes before it moves anything
// makes every later transaction, its own move included, a no-op.
func loseTheStateRefOnce(t *testing.T, root, to string) {
	t.Helper()
	gitBin := testenv.Tool(t, "git")
	hooks := filepath.Join(root, ".git", "dockhand-test-hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	marker := filepath.Join(hooks, "fired")
	script := "#!/bin/sh\n" +
		"[ -e " + marker + " ] && exit 0\n" +
		"case \"$1\" in\n" +
		"prepared) exit 1 ;;\n" +
		"aborted) : > " + marker + "\n" +
		"  " + gitBin + " -C " + root + " update-ref " + statestore.Ref + " " + to + " ;;\n" +
		"esac\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(script), 0o755))
	// Named explicitly so a machine whose global config points hooks
	// somewhere else still runs this one.
	cmd := exec.Command(gitBin, "-C", root, "config", "core.hooksPath", hooks)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

// A LOST RACE MUST NOT LEAK THE DISCARDED RUN'S CLAIM. statestore.Amend
// runs its closure again over a fresh read when a writer that never took
// the flock gets to the state ref first, so the closure is a function of
// the state it is handed and of nothing else. seize's was not: it
// assigned its captured lease only on the path where a live lease
// existed, so the run that found one and lost, followed by a run that
// found none and committed an empty change, returned the FIRST run's
// lease with took true.
//
// What that costs is take()'s next line: fulfil calls the provider's
// Release on a lease no committed transaction claimed — destroying an
// environment — and Discharge then drops the obligation from the
// standing list, so the pass reports a release the store has no claim,
// no Release.Done and no lease for.
func TestSeizeClaimsNothingAcrossALostRace(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	st := statestore.Open(repo)
	table(t, alive(), nil)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangeMinted})

	// The state as it stood before any lease existed. The peer's write is
	// this commit put back: a state with the change and no lease at all,
	// which is what a recreated ref or an older build leaves behind.
	before, err := repo.RevParse(t.Context(), statestore.Ref)
	require.NoError(t, err)

	l := held(t, st, "chg-1")
	ob := Obligation{
		Kind: Owed, Standing: Mine, Change: "chg-1", Platform: platformName,
		ID: l.ID, Request: l.Request, Since: now.Add(-time.Hour),
	}

	loseTheStateRefOnce(t, repo.Root, before)

	got, took, err := seize(t.Context(), st, ob, claimant(me()), now)
	require.NoError(t, err, "a lost race is retried, not refused")

	// The committed run found no lease on the slot, so it claimed
	// nothing, and that is the answer seize owes its caller.
	assert.False(t, took, "the discarded run's claim must not survive the retry")
	assert.Equal(t, record.Lease{}, got, "and neither may its lease")

	// The store agrees: the peer's state stands and nobody holds a claim.
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, s.Leases, "the committed state is the peer's, which has no lease")
}
