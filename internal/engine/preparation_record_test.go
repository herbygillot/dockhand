package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/stretchr/testify/require"
)

// cutAtStepFive is an update whose files are written and whose step 5's
// record didn't land, an unwritten edit left for the next command (#194).
func cutAtStepFive(t *testing.T) (*Engine, model.Branch, store.Store) {
	t.Helper()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	kept := e.Store
	e.Store = &uncertainStore{Store: kept, when: func(tx *watchedTx) bool { return tx.written }, landed: false}
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.ErrorIs(t, err, store.ErrUncertain)
	e.Store = kept
	return e, branch, kept
}

func unwrittenOf(t *testing.T, e *Engine, branch model.Branch) (written, unwritten []model.Edit) {
	t.Helper()
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		if written, err = r.Edits(branch.ID); err != nil {
			return err
		}
		unwritten, err = r.UnwrittenEdits(branch.ID)
		return err
	}))
	return written, unwritten
}

// Every edit an update records is seen written, at a time, once its files
// are (#194, R7 and E1).
func TestAnUpdatesEditIsWrittenOnceItsFilesAre(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	written, unwritten := unwrittenOf(t, e, branch)
	require.Len(t, written, 1)
	require.False(t, written[0].Written.IsZero())
	require.NotNil(t, written[0].Result, "its result is kept with it")
	require.Equal(t, "100644", written[0].Files[0].AfterMode)
	require.Empty(t, unwritten)
}

// A plan over an unwritten edit says what settling would do, and settles
// nothing (#194, T2).
func TestAPlanOverAnUnwrittenEditSettlesNothing(t *testing.T) {
	t.Parallel()
	e, branch, _ := cutAtStepFive(t)
	var said []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { said = append(said, u.Message) })
	_, err := e.Update(ctx, UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Plan: true})
	require.NoError(t, err)
	require.Contains(t, said, "jq: update to 1.8.1 was recorded before its files were written: its files are written, so its record would be finished (not done in a plan)")
	written, unwritten := unwrittenOf(t, e, branch)
	require.Empty(t, written, "a plan sets no written time")
	require.Len(t, unwritten, 1)
}

// An unwritten edit whose file a person changed since stops the next
// update, naming the file, and nothing is written; tidy with the person
// there commits it as theirs and removes the edit (#194, T1).
func TestAnEditWhoseFileChangedSinceStopsTheNextCommand(t *testing.T) {
	t.Parallel()
	e, branch, _ := cutAtStepFive(t)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	data, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(data, []byte("# mine\n")...), 0o644))
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.ErrorContains(t, err, "textproc/jq/Portfile changed since; nothing was written")
	require.NotNil(t, refusal.Of(err))
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	plan.Attended = true
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	_, unwritten := unwrittenOf(t, e, branch)
	require.Empty(t, unwritten, "taken as the person's, the edit goes")
}

// With nobody there, an unfinished update is never checked or submitted:
// the gate stops, naming the command that finishes it; a person is told
// and goes on (#194, T3).
func TestAnUnfinishedUpdateIsNeverCheckedOrSubmittedUnattended(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	worktree, err := e.worktree(t.Context(), branch)
	require.NoError(t, err)
	before, err := worktree.WorkingFile(t.Context(), "textproc/jq/Portfile")
	require.NoError(t, err)
	after, err := worktree.WriteBlob(t.Context(), []byte("name jq\nversion 1.8.1\n"))
	require.NoError(t, err)
	// Recorded and never written: none of its files is as after.
	edit := model.Edit{ID: "ed_pending", Branch: branch.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq", Subject: "jq: update to 1.8.1", At: e.now(),
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: model.ObjectID(before.Blob), After: model.ObjectID(after), BeforeMode: "100644", AfterMode: "100644"}}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.AddUnwritten(edit) }))
	err = gateErr(e.UnfinishedGate(t.Context(), branch, true))
	require.Error(t, err)
	require.Contains(t, refusal.WordsOf(err), "dockhand ship -b jq-update finishes it")
	var said []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { said = append(said, u.Message) })
	require.NoError(t, gateErr(e.UnfinishedGate(ctx, branch, false)))
	require.NotEmpty(t, said, "a person is told")
}

// A version the person names replaces an unwritten edit none of whose
// files is written, and says so (#194, T5).
func TestANamedVersionOverAnUnwrittenEdit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	worktree, err := e.worktree(t.Context(), branch)
	require.NoError(t, err)
	before, err := worktree.WorkingFile(t.Context(), "textproc/jq/Portfile")
	require.NoError(t, err)
	after, err := worktree.WriteBlob(t.Context(), []byte("name jq\nversion 1.8.0\n"))
	require.NoError(t, err)
	edit := model.Edit{ID: "ed_pending", Branch: branch.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq", Subject: "jq: update to 1.8.0", At: e.now(),
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: model.ObjectID(before.Blob), After: model.ObjectID(after), BeforeMode: "100644", AfterMode: "100644"}}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.AddUnwritten(edit) }))
	var said []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { said = append(said, u.Message) })
	_, err = e.Update(ctx, UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Version: "1.8.1"})
	require.NoError(t, err)
	require.Contains(t, said, "jq: update to 1.8.0 was never written; the version you named replaces it")
	written, unwritten := unwrittenOf(t, e, branch)
	require.Empty(t, unwritten)
	require.Len(t, written, 1)
	require.Equal(t, "jq: update to 1.8.1", written[0].Subject)
}

// Settling's own record of an edit written has the uncertain outcome's
// witness: landed, the edit stands written; not landed, the command says
// the record isn't complete and the edit waits to be settled again (the
// sweep's owed recordWritten site; Codex's read of 2c).
func TestASettlementsUncertainRecordIsReadBack(t *testing.T) {
	t.Parallel()
	for _, landed := range []bool{true, false} {
		e, branch, kept := cutAtStepFive(t)
		e.Store = &uncertainStore{Store: kept, when: func(tx *watchedTx) bool { return tx.written }, landed: landed}
		_, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
		e.Store = kept
		written, unwritten := unwrittenOf(t, e, branch)
		if landed {
			require.NoError(t, err, "the settlement's record landed, so it stands")
			require.Len(t, written, 1)
			continue
		}
		require.ErrorIs(t, err, store.ErrUncertain)
		require.ErrorContains(t, err, "its record isn't complete")
		require.Len(t, unwritten, 1, "it waits to be settled again")
	}
}

// gateErr is UnfinishedGate's error alone.
func gateErr(_ bool, err error) error { return err }
