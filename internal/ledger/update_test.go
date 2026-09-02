package ledger

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
)

func TestLoadOrStartBeginsARecordOnlyOnAbsence(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	r, err := l.LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	assert.Equal(t, sha, r.Sha)
	assert.Equal(t, "jq", r.Port)
	assert.Equal(t, record.Schema, r.Schema)
	assert.NotNil(t, r.Runs, "the caller assigns a run into this map next")
	assert.Empty(t, r.Runs)
	tree, err := repo.RevParse(ctx, sha+"^{tree}")
	require.NoError(t, err)
	assert.Equal(t, tree, r.Tree, "content identity is resolved when the record starts")

	// And a note that cannot be read is never mistaken for one that is
	// not there — the field bug this guard was written for.
	gittest.Note(t, repo, sha, "{not json")
	_, err = l.LoadOrStart(ctx, sha, "jq")
	require.Error(t, err, "a malformed note must not be treated as absence")
	assert.NotErrorIs(t, err, git.ErrNoNote)
}

func TestUpdateHandsTheClosureWhatIsOnDisk(t *testing.T) {
	// The re-read, stated directly: whatever the caller last saw, the
	// closure is handed the record git holds right now.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))

	seen := record.Record{}
	require.NoError(t, l.Update(ctx, sha, "jq", func(r *record.Record) error {
		seen = *r
		r.Runs["Oldos"] = record.Run{State: record.Deferred, Detail: "slot full"}
		return nil
	}))
	assert.Equal(t, record.Passed, seen.Runs["Testos"].State, "the closure read the stored record")

	after, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, after.Runs, 2)
	assert.Equal(t, record.Deferred, after.Runs["Oldos"].State)
}

func TestConcurrentRecordRunsAllSurvive(t *testing.T) {
	// The lost update the flock and the re-read exist to prevent: a
	// note is a whole JSON document, so two dockhands recording
	// different platforms at once would each write over what the other
	// had just added if either acted on a copy read outside the lock.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	plats := []string{"Testos", "Oldos", "Ancientos", "Futureos"}

	var wg sync.WaitGroup
	errs := make([]error, len(plats))
	for i, plat := range plats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = l.RecordRun(ctx, sha, "jq", plat,
				record.Run{State: record.Deferred, Detail: fmt.Sprintf("queued for %s", plat)})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, plats[i])
	}

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, n.Runs, len(plats), "every concurrent record survives the others")
	for _, plat := range plats {
		assert.Equal(t, record.Deferred, n.Runs[plat].State, plat)
	}
}

func TestUpdateUnchangedWritesNothingAtAll(t *testing.T) {
	// A settle that polls three running jobs and learns nothing has
	// nothing to write. The proof is the notes ref itself: every write
	// is a commit on it, so a ref that has not moved is a note that was
	// never rewritten.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))
	before := notesRef(t, repo)

	require.NoError(t, l.Update(ctx, sha, "jq", func(r *record.Record) error {
		r.Runs["Oldos"] = record.Run{State: record.Running}
		return ErrUnchanged
	}), "ErrUnchanged is an outcome, not a failure")

	assert.Equal(t, before, notesRef(t, repo), "the notes ref did not move")
	after, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, after.Runs, 1, "the abandoned mutation reached nothing")
}

func TestUpdateClosureErrorAbandonsTheWrite(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))
	before := notesRef(t, repo)

	err := l.Update(ctx, sha, "jq", func(r *record.Record) error {
		r.Runs["Oldos"] = record.Run{State: record.Running}
		return fmt.Errorf("polling: %w", errClosure)
	})
	require.ErrorIs(t, err, errClosure, "the caller's refusal comes back as its own")
	assert.Equal(t, before, notesRef(t, repo), "a failed closure writes nothing")
}

func TestRecordRunLeavesTheOtherPlatformsAlone(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Oldos",
		record.Run{State: record.Unsupported, Detail: "declares known_fail on Oldos"}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, n.Runs["Testos"].State)
	assert.Equal(t, "clean", n.Runs["Testos"].Lint, "the untouched run keeps its evidence")
	assert.Equal(t, record.Unsupported, n.Runs["Oldos"].State)
}

func TestRecordRunStartsTheRecordWhenTheCommitHasNone(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Testos",
		record.Run{State: record.Deferred, Detail: "all 2 verification slots are busy"}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "jq", n.Port, "the port the run named identifies the fresh record")
	assert.Equal(t, record.Deferred, n.Runs["Testos"].State)
}

// notesRef reads the verification notes ref, whose commit moves on
// every write and stands still when nothing was written.
func notesRef(t *testing.T, repo *git.Repo) string {
	t.Helper()
	sha, err := repo.RevParse(context.Background(), "refs/notes/"+git.VerifyNotesRef)
	require.NoError(t, err)
	return sha
}
