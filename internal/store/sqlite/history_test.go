package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func TestASchemaOneDatabaseIsUpgraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dockhand.db")
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	_, err = db.Exec(schemas[0] + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=1;", applicationID))
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO branches(repository_id, id, name, base, worktree, managed, title, state, created_at) VALUES('repo_1','br_1','dockhand/jq-4k2p','base','/w',1,'','open',1)")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		b, err := r.Branch("br_1")
		require.NoError(t, err, "the branch survives the upgrade")
		require.Equal(t, "dockhand/jq-4k2p", b.Name)
		edits, err := r.Edits("br_1")
		require.NoError(t, err)
		require.Empty(t, edits)
		return nil
	}))
	var version int
	require.NoError(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
}

func TestEditsCheckpointsAndAcceptances(t *testing.T) {
	f := open(t)
	b := f.branch("br_1", "dockhand/jq-4k2p")
	b.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 34901, Head: "ada/macports-ports:dockhand/jq-4k2p", Pushed: "abc", Body: "body", Draft: true,
		Observed: &model.PullRequestObservation{State: "open", Review: "changes-requested", Checks: "failing", Failing: []string{"macOS 15"}, Head: "abc", At: at}}
	edit := model.Edit{ID: "ed_1", Branch: b.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq", Subject: "jq: update to 1.8.1",
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "b1", After: "b2"}}, At: at,
		Release: &model.Release{Version: "1.8.1", Forge: "github", Repository: "jqlang/jq", Tag: "jq-1.8.1", Commit: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"}}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		return tx.AddEdit(edit)
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		bad := edit
		bad.ID, bad.Files = "ed_2", []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "b2", After: "b2"}}
		return tx.AddEdit(bad)
	}), model.ErrInvalid, "an edit that changed nothing is refused")

	checkpoint := model.Checkpoint{Number: 1, Kind: model.CheckpointTidy, State: model.CheckpointPrepared, Branch: b.ID, Before: "old", After: "new", BaseBefore: "m1", BaseAfter: "m1", At: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddCheckpoint(checkpoint) }))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		again := checkpoint
		return tx.AddCheckpoint(again)
	}), store.ErrConflict, "numbers are the next one")
	restored := at.Add(time.Hour)
	unmade := checkpoint
	unmade.RestoredAt = &restored
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.MarkRestored(unmade) }), "a prepared checkpoint's change isn't made, so there's nothing to restore")
	checkpoint.State = model.CheckpointApplied
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.SettleCheckpoint(checkpoint) }))
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.SettleCheckpoint(checkpoint) }), "a checkpoint settles once")
	checkpoint.RestoredAt = &restored
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.MarkRestored(checkpoint) }))
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.MarkRestored(checkpoint) }), "a checkpoint is restored once")
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.AddCheckpoint(model.Checkpoint{Number: 2, Kind: model.CheckpointRebase, State: model.CheckpointApplied, Branch: b.ID, Before: "new", After: "rebased", BaseBefore: "m1", BaseAfter: "m2", At: at})
	}), "kinds share one numbering")
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.AddCheckpoint(model.Checkpoint{Number: 3, Kind: model.CheckpointTidy, State: model.CheckpointPrepared, Branch: b.ID, Before: "rebased", After: "tidied", BaseBefore: "m2", BaseAfter: "m2", At: at})
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.SettleCheckpoint(model.Checkpoint{Number: 3, Kind: model.CheckpointTidy, State: model.CheckpointAbandoned})
	}), "a change that wasn't made is abandoned")
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		return tx.AddCheckpoint(model.Checkpoint{Number: 4, Kind: model.CheckpointTidy, State: model.CheckpointPrepared, Branch: b.ID, Before: "rebased", After: "tidied", BaseBefore: "m2", BaseAfter: "m3", At: at})
	}), model.ErrInvalid, "a tidy doesn't move the base")

	accepted := model.Acceptance{Branch: b.ID, Commit: "c1", Port: "harbor-cli", At: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddAcceptance(accepted); err != nil {
			return err
		}
		return tx.AddAcceptance(accepted)
	}))

	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		got, err := r.Branch(b.ID)
		require.NoError(t, err)
		require.Equal(t, b.PullRequest, got.PullRequest)
		edits, err := r.Edits(b.ID)
		require.NoError(t, err)
		require.Equal(t, []model.Edit{edit}, edits)
		c, err := r.Checkpoint(1)
		require.NoError(t, err)
		require.Equal(t, "tidy-1", c.Name())
		require.Equal(t, restored, *c.RestoredAt)
		all, err := r.Checkpoints(b.ID)
		require.NoError(t, err)
		require.Len(t, all, 3)
		require.Equal(t, "rebase-2", all[1].Name())
		require.Equal(t, []model.CheckpointState{model.CheckpointApplied, model.CheckpointApplied, model.CheckpointAbandoned}, []model.CheckpointState{all[0].State, all[1].State, all[2].State})
		require.Equal(t, [2]model.ObjectID{"m1", "m2"}, [2]model.ObjectID{all[1].BaseBefore, all[1].BaseAfter}, "a checkpoint keeps the base before and after")
		list, err := r.Acceptances(b.ID, "c1")
		require.NoError(t, err)
		require.Equal(t, []model.Acceptance{accepted}, list)
		none, err := r.Acceptances(b.ID, "c2")
		require.NoError(t, err)
		require.Empty(t, none, "an acceptance holds for its commit only")
		return nil
	}))
}

func TestUpgradingKeepsRecordedEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dockhand.db")
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	_, err = db.Exec(schemas[0] + schemas[1] + schemas[2] + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=3;", applicationID))
	require.NoError(t, err)
	for _, statement := range []string{
		"INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0)",
		"INSERT INTO branches(repository_id, id, name, base, worktree, managed, title, state, created_at) VALUES('repo_1','br_1','dockhand/jq-4k2p','base','/w',1,'','open',1)",
		`INSERT INTO edits VALUES('repo_1','ed_1','br_1','update','jq','textproc/jq','jq: update to 1.8.1','[{"Path":"textproc/jq/Portfile","Before":"a","After":"b"}]',1)`,
		"INSERT INTO checkpoints(repository_id, number, branch_id, before_head, after_head, at) VALUES('repo_1',1,'br_1','old','new',1)",
	} {
		_, err = db.Exec(statement)
		require.NoError(t, err, statement)
	}
	require.NoError(t, db.Close())

	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		edits, err := r.Edits("br_1")
		require.NoError(t, err)
		require.Len(t, edits, 1, "the rebuilt table keeps its rows")
		require.Equal(t, "jq: update to 1.8.1", edits[0].Subject)
		checkpoint, err := r.Checkpoint(1)
		require.NoError(t, err)
		require.Equal(t, "tidy-1", checkpoint.Name(), "older checkpoints were tidy's")
		return nil
	}))
}

func TestReviewsAreKeptNewestFirst(t *testing.T) {
	f := open(t)
	first := model.Review{Repository: "macports/macports-ports", Number: 34905, Head: "h1", At: at,
		Findings: []model.ReviewFinding{{Code: "follow-up", Severity: "error", Message: "squash it"}}, Posted: "request-changes"}
	second := model.Review{Repository: "macports/macports-ports", Number: 34905, Head: "h2", At: at.Add(time.Hour)}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.LastReview("macports/macports-ports", 34905)
		require.ErrorIs(t, err, store.ErrNotFound)
		if err := tx.AddReview(first); err != nil {
			return err
		}
		last, err := tx.LastReview("macports/macports-ports", 34905)
		require.NoError(t, err)
		require.Equal(t, first, last)
		if err := tx.AddReview(second); err != nil {
			return err
		}
		last, err = tx.LastReview("macports/macports-ports", 34905)
		require.NoError(t, err)
		require.Equal(t, model.ObjectID("h2"), last.Head)
		require.Empty(t, last.Findings)
		return nil
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		return tx.AddReview(model.Review{Repository: "macports/macports-ports", Number: 1, Head: "h", At: at, Posted: "approve"})
	}), model.ErrInvalid)
}

func TestAnEditKeepsItsUpstreamComparisonAndABranchItsOrigin(t *testing.T) {
	f := open(t)
	b := f.branch("br_1", "dockhand/croc-7hq2")
	b.Origin = model.OriginServe
	compared := &model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}}}
	edit := model.Edit{ID: "ed_1", Branch: b.ID, Kind: model.EditUpdate, Port: "croc", Directory: "net/croc", Subject: "croc: update to 10.2.5",
		Files: []model.EditedFile{{Path: "net/croc/Portfile", Before: "b1", After: "b2"}}, At: at, Upstream: compared}
	plain := edit
	plain.ID, plain.Upstream = "ed_2", nil
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		return tx.AddEdit(plain)
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		stored, err := tx.Branch(b.ID)
		require.NoError(t, err)
		require.Equal(t, model.OriginServe, stored.Origin)
		edits, err := tx.Edits(b.ID)
		require.NoError(t, err)
		require.Equal(t, compared, edits[0].Upstream)
		require.True(t, edits[0].Upstream.Held())
		require.Nil(t, edits[1].Upstream, "an edit that compared nothing says so")
		return nil
	}))
}

// A revision's assessment of a port is kept by its tree, base, and port,
// newest first; recording one again for the same replaces it.
func TestAnAssessmentIsTheRevisions(t *testing.T) {
	f := open(t)
	b := f.branch("br_1", "dockhand/croc-7hq2")
	found := model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true, Rule: "license-changed", Class: model.Introduced}},
		Coverage: []model.Coverage{{Path: "package.json", Relevance: "unknown", Treatment: "set-apart", Policy: "portgroup-scoping"}}}
	first := model.Assessment{Branch: b.ID, Tree: "t1", Base: "b1", Port: "croc", Directory: "net/croc", Comparison: found, Policy: 1, At: at}
	second := first
	second.Tree, second.At = "t2", at.Add(time.Minute)
	again := first
	again.Comparison, again.At = model.UpstreamComparison{Changes: []model.UpstreamChange{}}, at.Add(2*time.Minute)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		for _, a := range []model.Assessment{first, second} {
			if err := tx.RecordAssessment(a); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		assessments, err := tx.Assessments(store.AssessmentFilter{Branch: b.ID})
		require.NoError(t, err)
		require.Equal(t, []model.Assessment{second, first}, assessments)
		require.NoError(t, tx.RecordAssessment(again))
		assessments, err = tx.Assessments(store.AssessmentFilter{Branch: b.ID})
		require.NoError(t, err)
		require.Equal(t, []model.Assessment{again, second}, assessments)
		require.Error(t, tx.RecordAssessment(model.Assessment{Branch: b.ID, Tree: "t1", Base: "b1", Port: "croc", Directory: "net/croc"}), "no policy")
		return nil
	}))
}

// A change record is the revision's, by its tree, base, and directory:
// recorded again it replaces what was, and it reads back as it was kept.
func TestAChangeRecordIsTheRevisions(t *testing.T) {
	f := open(t)
	b := f.branch("br_1", "dockhand/terraform-1.17-7hq2")
	first := model.ChangeRecord{Branch: b.ID, Tree: "t1", Base: "b1", Directory: "sysutils/terraform", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"},
		Ports: []model.SubportChange{
			{Port: "terraform-1.17", Kind: model.SubportAdded},
			{Port: "terraform-1.16", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "version", From: "1.16.0", To: "1.16.1"}}},
			{Port: "terraform-1.15", Kind: model.SubportUnchanged},
		}, Policy: 1, At: at}
	again := first
	again.Ports, again.At = first.Ports[2:], at.Add(time.Minute)
	other := first
	other.Tree = "t2"
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		for _, r := range []model.ChangeRecord{first, other, again} {
			if err := tx.RecordChange(r); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		records, err := tx.ChangeRecords(store.AssessmentFilter{Branch: b.ID, Tree: "t1", Base: "b1"})
		require.NoError(t, err)
		require.Equal(t, []model.ChangeRecord{again}, records)
		require.Empty(t, again.Changed())
		require.Equal(t, []string{"terraform-1.17", "terraform-1.16"}, other.Changed())
		require.Error(t, tx.RecordChange(model.ChangeRecord{Branch: b.ID, Tree: "t1", Base: "b1", Directory: "sysutils/terraform"}), "no policy")
		require.Error(t, tx.RecordChange(model.ChangeRecord{Branch: b.ID, Tree: "t1", Base: "b1", Directory: "sysutils/terraform", Policy: 1, Ports: []model.SubportChange{{Port: "terraform", Kind: "moved"}}}), "no such kind")
		return nil
	}))
}

// A revision's assessments are read by its branch, tree, and base alone,
// newest first: every check read and decoded all its branch had ever
// recorded, to keep its revision's (the SQL review's rescan).
func TestARevisionsAssessmentsAreReadByItsKey(t *testing.T) {
	f := open(t)
	b, other := f.branch("br_1", "dockhand/croc-7hq2"), f.branch("br_2", "dockhand/croc-m4ve")
	croc := model.Assessment{Branch: b.ID, Tree: "t1", Base: "b1", Port: "croc", Directory: "net/croc", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}}, Policy: 1, At: at}
	jq := croc
	jq.Port, jq.Directory, jq.At = "jq", "sysutils/jq", at.Add(time.Minute)
	nextTree, nextBase, otherBranch := croc, croc, croc
	nextTree.Tree, nextBase.Base, otherBranch.Branch = "t2", "b2", other.ID
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for _, branch := range []model.Branch{b, other} {
			if err := tx.AddBranch(branch); err != nil {
				return err
			}
		}
		for _, a := range []model.Assessment{croc, jq, nextTree, nextBase, otherBranch} {
			if err := tx.RecordAssessment(a); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		revision, err := r.Assessments(store.AssessmentFilter{Branch: b.ID, Tree: "t1", Base: "b1"})
		require.NoError(t, err)
		require.Equal(t, []model.Assessment{jq, croc}, revision)
		all, err := r.Assessments(store.AssessmentFilter{})
		require.NoError(t, err)
		require.Len(t, all, 5, "empty fields select all")
		return nil
	}))
}
