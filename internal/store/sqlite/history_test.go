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
		Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "b1", After: "b2"}}, At: at}
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

	checkpoint := model.Checkpoint{Number: 1, Kind: model.CheckpointTidy, Branch: b.ID, Before: "old", After: "new", At: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddCheckpoint(checkpoint) }))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		again := checkpoint
		return tx.AddCheckpoint(again)
	}), store.ErrConflict, "numbers are the next one")
	restored := at.Add(time.Hour)
	checkpoint.RestoredAt = &restored
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.MarkRestored(checkpoint) }))
	require.Error(t, f.update(t, func(tx store.Tx) error { return tx.MarkRestored(checkpoint) }), "a checkpoint is restored once")
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.AddCheckpoint(model.Checkpoint{Number: 2, Kind: model.CheckpointRebase, Branch: b.ID, Before: "new", After: "rebased", At: at})
	}), "kinds share one numbering")

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
		require.Len(t, all, 2)
		require.Equal(t, "rebase-2", all[1].Name())
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
