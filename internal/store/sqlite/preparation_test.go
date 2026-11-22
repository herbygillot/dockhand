package sqlite

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/stretchr/testify/require"
)

// An edit keeps its result and the time its files were seen written, and
// each file both modes (#194, R1).
func TestAnEditKeepsItsResultAndWrittenTime(t *testing.T) {
	t.Parallel()
	f := open(t)
	b := f.branch("br_1", "dockhand/jq-4k2p")
	result := &model.EditResult{Before: "1.8.0_0", After: "1.8.1_0", Patches: &model.PatchCheck{Scope: model.PatchScope("2.12.7", "8.6"), Declared: []model.PatchResult{{Name: "fix.diff", State: model.PatchRejected}}},
		Findings: []model.Finding{{Kind: model.FindingPatchRejected, Subject: "fix.diff", Detail: []string{"Hunk #1 FAILED"}}}}
	edit := model.Edit{ID: "ed_1", Branch: b.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq", Subject: "jq: update to 1.8.1", At: at, Written: at.Add(time.Second),
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "b1", After: "b2", BeforeMode: "100644", AfterMode: "100755"}}, Result: result}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		return tx.AddEdit(edit)
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		got, found, err := tx.Edit("ed_1")
		require.True(t, found)
		require.Equal(t, edit, got)
		return err
	}))
}

// Edits returns written edits alone; an unwritten one is read by its ID or
// with the branch's unwritten ones, written once, and removed only while
// unwritten. AddEdit refuses an edit with no written time, and AddUnwritten
// one with it (#194, R3 to R6).
func TestEditsReturnsWrittenEditsOnly(t *testing.T) {
	t.Parallel()
	f := open(t)
	b := f.branch("br_1", "dockhand/jq-4k2p")
	pending := model.Edit{ID: "ed_1", Branch: b.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq", Subject: "jq: update to 1.8.1", At: at,
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "b1", After: "b2"}}}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		require.Error(t, tx.AddEdit(pending), "AddEdit refuses an edit with no written time")
		written := pending
		written.ID, written.Written = "ed_0", at
		require.Error(t, tx.AddUnwritten(written), "AddUnwritten refuses a written edit")
		return tx.AddUnwritten(pending)
	}))
	read := func() (edits, unwritten []model.Edit) {
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			var err error
			if edits, err = tx.Edits(b.ID); err != nil {
				return err
			}
			unwritten, err = tx.UnwrittenEdits(b.ID)
			return err
		}))
		return edits, unwritten
	}
	edits, unwritten := read()
	require.Empty(t, edits, "an unwritten edit is the settler's alone")
	require.Equal(t, []model.Edit{pending}, unwritten)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.SetWritten("ed_1", at.Add(time.Minute)) }))
	edits, unwritten = read()
	require.Len(t, edits, 1)
	require.Equal(t, at.Add(time.Minute), edits[0].Written)
	require.Empty(t, unwritten)
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.SetWritten("ed_1", at.Add(time.Hour)) }), "written once")
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.RemoveUnwritten("ed_1") }), "a written edit is never removed by it")
	edits, _ = read()
	require.Len(t, edits, 1)
}

// The upgrade to schema 65 against the database before it: every edit
// was written at its own time; an edit's hold becomes a patch-rejected
// finding, left, with no patch named and its held lines as detail; no
// hold is stored after it; a branch keeps its records with no request and
// no rename under way (#194, X1, X2, P11).
func TestTheUpgradeAgainstTheDatabaseBeforeIt(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, 64, `
INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);
INSERT INTO branches(repository_id, id, name, base, worktree, managed, title, state, created_at, origin) VALUES('repo_1','br_1','dockhand/jq-4k2p','base','/w',1,'','open',1,'serve');
INSERT INTO edits(repository_id, id, branch_id, kind, port, directory, subject, files, at, held) VALUES
 ('repo_1','ed_1','br_1','update','jq','textproc/jq','jq: update to 1.8.1','[{"Path":"textproc/jq/Portfile","Before":"a","After":"b"}]',1794336000000,'["fix.diff: 1 out of 1 hunk FAILED"]'),
 ('repo_1','ed_2','br_1','update','jq-doc','textproc/jq','jq-doc: update to 1.0.2','[{"Path":"textproc/jq/Portfile","Before":"b","After":"c"}]',1794336100000,'');`)
	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	var columns int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM pragma_table_info('edits') WHERE name='held'").Scan(&columns))
	require.Zero(t, columns, "no hold is stored after the upgrade")
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		edits, err := r.Edits("br_1")
		require.Len(t, edits, 2, "every edit from before was written")
		require.Equal(t, fromMillis(1794336000000), edits[0].Written)
		require.Equal(t, &model.EditResult{Findings: []model.Finding{{Kind: model.FindingPatchRejected, Detail: []string{"fix.diff: 1 out of 1 hunk FAILED"}}}}, edits[0].Result)
		require.Nil(t, edits[1].Result, "an edit with no hold has no result")
		branch, err2 := r.Branch("br_1")
		require.Nil(t, branch.Request, "a branch from before has no request")
		require.Empty(t, branch.RenameTo)
		require.NoError(t, err2)
		return err
	}))
}

// A branch keeps what was asked of it and the name a rename is moving it
// to (#194).
func TestABranchKeepsItsRequestAndRenameIntent(t *testing.T) {
	t.Parallel()
	f := open(t)
	b := f.branch("br_1", "dockhand/jq-4k2p")
	b.Request = &model.ChangeRequest{ID: "rq_1", Origin: "ship", Targets: []model.RequestTarget{{Port: "jq", Action: "update", Release: &model.Release{Version: "1.8.1"},
		Outcomes: []model.ActionOutcome{{Action: "version", Result: model.ActionDone}}}}}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(b) }))
	b.RenameTo = "dockhand/jq-1.8.1"
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		got, err := tx.Branch(b.ID)
		require.Equal(t, b.Request, got.Request)
		require.Equal(t, "dockhand/jq-1.8.1", got.RenameTo)
		return err
	}))
}
