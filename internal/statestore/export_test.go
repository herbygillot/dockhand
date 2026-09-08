package statestore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
)

// THE NOTE IS A DERIVED EXPORT. Everything on it is a copy of what the
// store already holds, it is written through the ledger rather than by a
// second note-writer, and nothing reads it back to decide anything —
// which is why it may lag and may not disagree.
func TestExportProjectsTheStoreOntoTheCommit(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	tip := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")
	at := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-1", State: record.ChangeMinted, Tip: tip, Branch: "dockhand/jq-1.8", Content: "sha256:abc"})
		tx.PutAttempt(record.Attempt{ID: "att-1", Change: "chg-1", Sha: tip, Platform: "sequoia",
			Started: at.Add(-time.Hour), Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: at}}})
		tx.PutLease(record.Lease{Request: "req-1", Change: "chg-1", Platform: "sequoia", Phase: record.Finished})
		tx.PutPublication(record.Publication{ID: "pub-1", Change: "chg-1", By: record.Human, Number: 42,
			URL: "https://example.invalid/42", Outcome: record.Open,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: at}}})
		return nil
	}))

	require.NoError(t, store.Export(ctx, ledger.Open(repo), tip))

	rec, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, tip, rec.Sha)
	assert.Equal(t, record.ChangeID("chg-1"), rec.Change.ID)
	assert.NotEmpty(t, rec.Tree, "the note carries the content identity of the commit it sits on")

	require.Contains(t, rec.Runs, record.RunKey{Port: "jq", Platform: "sequoia"},
		"both halves of the key come off the attempt itself")
	assert.Equal(t, record.Passed, rec.Runs[record.RunKey{Port: "jq", Platform: "sequoia"}].State)
	assert.Contains(t, rec.Leases, "sequoia", "the note's leases are keyed by platform")
	require.NotNil(t, rec.Publication)
	assert.Equal(t, 42, rec.Publication.Number)
	assert.Equal(t, record.Human, rec.Publication.PublishedBy)
	assert.Zero(t, rec.Publication.Unproven, "everything that was published built")
}

// THE COLLAPSE IS MANY-TO-ONE, and the rule is stated on record.Runs: for
// each (member, platform) keep the most recently STARTED attempt that
// reached a terminal verdict. A retry replaces its predecessor in the
// note while both survive in the store, which is the right way round.
func TestExportKeepsTheLatestTerminalVerdictPerMemberAndPlatform(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	tip := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")
	early := time.Now().Add(-3 * time.Hour)
	late := time.Now().Add(-time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-1", State: record.ChangeMinted, Tip: tip})
		tx.PutAttempt(record.Attempt{ID: "att-first", Change: "chg-1", Sha: tip, Platform: "sequoia",
			Started: early, Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Failed, At: early}}})
		tx.PutAttempt(record.Attempt{ID: "att-retry", Change: "chg-1", Sha: tip, Platform: "sequoia",
			Started: late, Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: late}}})
		// Another platform is another key, never a collision.
		tx.PutAttempt(record.Attempt{ID: "att-sonoma", Change: "chg-1", Sha: tip, Platform: "sonoma",
			Started: late, Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Blocked, At: late}}})
		// A queued attempt has no runs of its own; what a person sees for
		// it is that a run is waiting.
		tx.PutAttempt(record.Attempt{ID: "att-queued", Change: "chg-1", Sha: tip, Platform: "sequoia",
			Started: late, Phase: record.Requested, Members: []string{"libwidget"}})
		// An attempt against another commit is another commit's note.
		tx.PutAttempt(record.Attempt{ID: "att-elsewhere", Change: "chg-1", Sha: "deadbeef", Platform: "sequoia",
			Started: late, Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Errored, At: late}}})
		tx.PutPublication(record.Publication{ID: "pub-1", Change: "chg-1", By: record.Machine, Outcome: record.Open,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: late}}})
		return nil
	}))
	require.NoError(t, store.Export(ctx, ledger.Open(repo), tip))

	rec, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, rec.Runs[record.RunKey{Port: "jq", Platform: "sequoia"}].State,
		"the retry replaces its predecessor")
	assert.Equal(t, record.Blocked, rec.Runs[record.RunKey{Port: "jq", Platform: "sonoma"}].State)
	assert.Equal(t, record.Queued, rec.Runs[record.RunKey{Port: "libwidget", Platform: "sequoia"}].State)
	assert.Len(t, rec.Runs, 3, "an attempt against another commit is not on this commit's note")

	require.NotNil(t, rec.Publication)
	assert.Equal(t, 1, rec.Publication.Unproven, "the blocked member was published without a pass")
}

// A commit no change is bound to is a typed refusal, because the pass
// that re-exports notes meets it legitimately — a note whose change was
// compacted away — and must step over it rather than abandon the pass.
func TestExportRefusesACommitNoChangeIsBoundTo(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	tip := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-1", State: record.ChangeMinted, Tip: "deadbeef"})
		return nil
	}))

	err := store.Export(ctx, ledger.Open(repo), tip)
	require.ErrorIs(t, err, ErrNoChangeAt)
	assert.ErrorContains(t, err, tip)
}

// The export reads the store, so a repository with no state ref has
// nothing to project — and it says so with the one error that keeps
// "dockhand has never run here" apart from "nothing is in flight".
func TestExportRefusesWhenThereIsNoState(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	tip := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")

	require.ErrorIs(t, store.Export(ctx, ledger.Open(repo), tip), ErrNoState)
}
