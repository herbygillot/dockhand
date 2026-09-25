package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// at is millisecond-precise, since stored times are.
var at = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

var tahoe = model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}

type fixture struct {
	store *Store
	repo  model.RepositoryID
	path  string
}

func open(t *testing.T) fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dockhand.db")
	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	repo, err := s.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)
	return fixture{store: s, repo: repo, path: path}
}

func (f fixture) update(t *testing.T, fn func(store.Tx) error) error {
	t.Helper()
	return f.store.Update(t.Context(), f.repo, fn)
}

func (f fixture) branch(id model.BranchID, name string) model.Branch {
	return model.Branch{ID: id, Repository: f.repo, Name: name, Base: "base", Worktree: "/w/" + name, Managed: true, State: model.BranchOpen, CreatedAt: at}
}

// seed records a branch, a snapshot revision, and a plan with two targets,
// libharbor before harbor-cli.
func (f fixture) seed(t *testing.T) (model.Branch, model.Revision, model.Plan) {
	t.Helper()
	b := f.branch("br_1", "dockhand/libharbor-2")
	r := model.Revision{ID: "rev_1", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 1, Source: model.Source{Tree: "tree", Base: "base"}, Head: "head", CreatedAt: at}
	p := model.Plan{ID: "plan_1", Revision: r.ID, Tests: model.TestsDeclared, CreatedAt: at, Environments: []model.Environment{tahoe},
		Targets: []model.PlanTarget{
			{ID: "libharbor", Target: model.Target{Name: "libharbor", Variants: map[string]bool{"docs": false}}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed},
			{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}, Directory: "devel/harbor-cli", Kind: model.RevisionOnly, Role: model.Changed, DependsOn: []model.TargetID{"libharbor"}},
		}}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(b); err != nil {
			return err
		}
		if err := tx.AddRevision(r); err != nil {
			return err
		}
		return tx.AddPlan(p)
	}))
	return b, r, p
}

func (f fixture) run(t *testing.T, b model.Branch, r model.Revision, p model.Plan) model.Run {
	t.Helper()
	var run model.Run
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		run = model.Run{ID: model.RunID(fmt.Sprintf("run_%d", number)), Branch: b.ID, Revision: r.ID, Plan: p.ID, Number: number, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at}
		return tx.AddRun(run)
	}))
	return run
}

func TestOpenCreatesReopensAndRefusesOtherDatabases(t *testing.T) {
	f := open(t)
	again, err := f.store.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)
	require.Equal(t, f.repo, again, "a registration is found, not repeated")
	_, err = f.store.Register(t.Context(), "relative/.git")
	require.ErrorIs(t, err, model.ErrInvalid)
	require.NoError(t, f.store.Close())

	reopened, err := Open(t.Context(), f.path, Options{})
	require.NoError(t, err)
	require.NoError(t, reopened.Close())

	for name, setup := range map[string]string{
		"v2":      fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=24;", v2ApplicationID),
		"foreign": "CREATE TABLE notes(x TEXT);",
		"newer":   fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d;", applicationID, schemaVersion+1),
	} {
		path := filepath.Join(t.TempDir(), name+".db")
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		_, err = db.Exec(setup)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		_, err = Open(t.Context(), path, Options{})
		require.ErrorIs(t, err, store.ErrSchema, name)
		if name == "v2" {
			require.ErrorContains(t, err, "dockhand v2 database")
		}
	}
}

func TestTransactionsAreScopedAndNotNested(t *testing.T) {
	f := open(t)
	other, err := f.store.Register(t.Context(), "/src/second-clone/.git")
	require.NoError(t, err)
	b, _, _ := f.seed(t)
	require.NoError(t, f.store.View(t.Context(), other, func(r store.Reader) error {
		_, err := r.Branch(b.ID)
		require.ErrorIs(t, err, store.ErrNotFound, "another repository sees nothing of this one")
		return nil
	}))
	require.ErrorIs(t, f.store.View(t.Context(), "repo_missing", func(store.Reader) error { return nil }), store.ErrNotFound)

	err = f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		return r.(store.Tx).AddBranch(f.branch("br_2", "dockhand/jq-1"))
	})
	require.ErrorIs(t, err, model.ErrInvalid, "a read transaction refuses writes")

	err = f.update(t, func(tx store.Tx) error {
		return f.store.View(t.Context(), f.repo, func(store.Reader) error { return nil })
	})
	require.NoError(t, err, "a separate context is a separate transaction")
	err = f.store.Update(t.Context(), f.repo, func(tx store.Tx) error {
		return f.store.View(context.WithValue(t.Context(), inTransaction{}, true), f.repo, func(store.Reader) error { return nil })
	})
	require.ErrorIs(t, err, model.ErrInvalid)

	failed := f.update(t, func(tx store.Tx) error {
		require.NoError(t, tx.AddBranch(f.branch("br_3", "dockhand/fd-1")))
		return fmt.Errorf("stop")
	})
	require.EqualError(t, failed, "stop")
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		_, err := r.Branch("br_3")
		require.ErrorIs(t, err, store.ErrNotFound, "a failed callback commits nothing")
		return nil
	}))
}

func TestBranchesKeepTheirRules(t *testing.T) {
	f := open(t)
	b, _, _ := f.seed(t)
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(f.branch("br_2", b.Name)) }), store.ErrConflict, "one open branch per name")

	b.Title, b.PullRequest = "libharbor: update to 2.0", &model.PullRequest{Repository: "macports/macports-ports", Number: 34901, Head: "ada/macports-ports:dockhand/libharbor-2"}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		got, err := r.BranchNamed(b.Name)
		require.NoError(t, err)
		require.Equal(t, b, got)
		return nil
	}))

	merged := b
	merged.State = model.BranchMerged
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(merged) }))
	reopened := merged
	reopened.State = model.BranchOpen
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(reopened) }), store.ErrConflict, "a merge is final")
	reused := f.branch("br_2", b.Name)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(reused) }), "a merged branch's name can be used again")

	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		open, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		require.NoError(t, err)
		require.Len(t, open, 1)
		require.Equal(t, reused.ID, open[0].ID)
		return nil
	}))
}

func TestSnapshotsAreNumberedInOrder(t *testing.T) {
	f := open(t)
	b, first, _ := f.seed(t)
	snapshot := func(n int) model.Revision {
		return model.Revision{ID: model.RevisionID(fmt.Sprintf("rev_s%d", n)), Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: n, Source: model.Source{Tree: "t", Base: "base"}, CreatedAt: at}
	}
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddRevision(snapshot(3)) }), store.ErrConflict)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		next, err := tx.NextSnapshot(b.ID)
		require.NoError(t, err)
		require.Equal(t, 2, next)
		return tx.AddRevision(snapshot(next))
	}))
	commit := model.Revision{ID: "rev_c", Branch: b.ID, Kind: model.RevisionCommit, Source: model.Source{Commit: "c", Tree: "t", Base: "base"}, CreatedAt: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddRevision(commit) }), "commits are not numbered")
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		revisions, err := r.Revisions(b.ID)
		require.NoError(t, err)
		require.Equal(t, []model.RevisionID{first.ID, "rev_s2", "rev_c"}, []model.RevisionID{revisions[0].ID, revisions[1].ID, revisions[2].ID})
		return nil
	}))
}

func TestPlansRoundTrip(t *testing.T) {
	f := open(t)
	_, _, p := f.seed(t)
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		got, err := r.Plan(p.ID)
		require.NoError(t, err)
		require.Equal(t, p, got)
		return nil
	}))
}

func TestRunsAreNumberedImmutableAndMoveForward(t *testing.T) {
	f := open(t)
	b, r, p := f.seed(t)
	first := f.run(t, b, r, p)
	second := f.run(t, b, r, p)
	require.Equal(t, "check-1", first.Name())
	require.Equal(t, "check-2", second.Name())

	skipped := first
	skipped.ID, skipped.Number = "run_x", 5
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddRun(skipped) }), store.ErrConflict)

	moved := first
	moved.Plan = "plan_other"
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(moved) }), store.ErrConflict, "what a run asks never changes")

	running := first
	running.State = model.RunRunning
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(running) }))
	finished := at.Add(time.Hour)
	passed := running
	passed.State, passed.FinishedAt = model.RunPassed, &finished
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(passed) }))
	again := passed
	again.State, again.FinishedAt = model.RunRunning, nil
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(again) }), store.ErrConflict)

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		got, err := rd.RunNumbered(1)
		require.NoError(t, err)
		require.Equal(t, passed, got)
		queued, err := rd.Runs(store.RunFilter{States: []model.RunState{model.RunQueued}})
		require.NoError(t, err)
		require.Len(t, queued, 1)
		require.Equal(t, second.ID, queued[0].ID)
		return nil
	}))

	unresolved := p
	unresolved.ID, unresolved.Unresolved = "plan_u", []model.Unresolved{{Target: model.Target{Name: "harbor-viewer"}, Reason: "evaluation failed"}}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddPlan(unresolved) }))
	blocked := model.Run{ID: "run_u", Branch: b.ID, Revision: r.ID, Plan: unresolved.ID, Number: 3, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at}
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddRun(blocked) }), model.ErrInvalid, "an unresolved plan cannot run")
}

func TestExecutionsAndCheckpoints(t *testing.T) {
	f := open(t)
	b, r, p := f.seed(t)
	run := f.run(t, b, r, p)
	execution := model.GuestExecution{ID: "ex_1", Run: run.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(execution) }))

	elsewhere := execution
	elsewhere.ID, elsewhere.Environment.Platform.Version = "ex_2", "23"
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(elsewhere) }), model.ErrInvalid, "an environment outside the plan")
	repeated := execution
	repeated.ID = "ex_3"
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(repeated) }), store.ErrConflict, "one execution per environment and attempt")

	result := func(target model.TargetID, outcome model.Outcome, phase model.Phase) model.TargetResult {
		return model.TargetResult{Execution: execution.ID, Target: target, Outcome: outcome, Phase: phase, Tests: model.TestsNone, RecordedAt: at}
	}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("libharbor", model.OutcomePassed, "")) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("harbor-cli", model.OutcomeNotRun, "")) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("harbor-cli", model.OutcomeInterrupted, "")) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.RecordResult(result("harbor-cli", model.OutcomeFailed, model.PhaseInstall))
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("libharbor", model.OutcomeFailed, model.PhaseTest)) }), store.ErrConflict, "a verdict is final")
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("harbor-viewer", model.OutcomePassed, "")) }), model.ErrInvalid, "a target outside the plan")

	// The schema keeps the rule even if Go code went around the store.
	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec("UPDATE results SET outcome='failed', phase='test' WHERE target_id='libharbor'")
	require.ErrorContains(t, err, "final")

	finished := at.Add(time.Hour)
	infrastructure := execution
	infrastructure.State, infrastructure.FinishedAt, infrastructure.Detail = model.ExecutionInfrastructure, &finished, "the guest was lost"
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateExecution(infrastructure) }))
	retry := model.GuestExecution{ID: "ex_4", Run: run.ID, Environment: tahoe, Attempt: 2, State: model.ExecutionWaiting, CreatedAt: finished}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(retry) }), "a retry is a new execution")

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		results, err := rd.Results(execution.ID)
		require.NoError(t, err)
		require.Len(t, results, 2)
		executions, err := rd.Executions(run.ID)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2}, []int{executions[0].Attempt, executions[1].Attempt})
		return nil
	}))
}

func session(f fixture, id model.SessionID, kind model.SessionKind) model.Session {
	return model.Session{ID: id, Repository: f.repo, Kind: kind, PID: 4711, ProcessStart: "linux:12345", Version: "test", StartedAt: at, HeartbeatAt: at}
}

func TestLeasesAreFenced(t *testing.T) {
	f := open(t)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddSession(session(f, "ses_a", model.SessionServe)); err != nil {
			return err
		}
		return tx.AddSession(session(f, "ses_b", model.SessionServe))
	}))
	var first, second model.Lease
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		var err error
		first, err = tx.AcquireLease("leader", "ses_a")
		return err
	}))
	require.Equal(t, uint64(1), first.Generation)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		var err error
		second, err = tx.AcquireLease("leader", "ses_b")
		return err
	}))
	require.Equal(t, uint64(2), second.Generation)
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.CheckLease(first) }), store.ErrStale, "the displaced holder's writes are refused")
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.ReleaseLease(first) }), store.ErrStale)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.ReleaseLease(second) }))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.CheckLease(second) }), store.ErrStale, "a released lease fences its holder too")

	var third model.Lease
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		var err error
		third, err = tx.AcquireLease("leader", "ses_a")
		return err
	}))
	require.Equal(t, uint64(3), third.Generation, "a generation survives release")

	ended := session(f, "ses_a", model.SessionServe)
	endedAt := at.Add(time.Minute)
	ended.EndedAt = &endedAt
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateSession(ended) }))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.AcquireLease("run:run_1", "ses_a")
		return err
	}), store.ErrConflict, "an ended session holds nothing new")
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		live, err := r.Sessions()
		require.NoError(t, err)
		require.Len(t, live, 1)
		require.Equal(t, model.SessionID("ses_b"), live[0].ID)
		return nil
	}))

	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec("UPDATE leases SET generation=1 WHERE resource='leader'")
	require.ErrorContains(t, err, "never decreases")
}

func TestEventsAreAJournal(t *testing.T) {
	f := open(t)
	var sequences []int64
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for i, kind := range []string{"run.state", "guest.clone", "target.result"} {
			seq, err := tx.AppendEvent(model.Event{At: at.Add(time.Duration(i) * time.Second), Session: "ses_a", Run: "run_1", Kind: kind, Message: kind})
			if err != nil {
				return err
			}
			sequences = append(sequences, seq)
		}
		return nil
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: at})
		return err
	}), model.ErrInvalid)
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		tail, err := r.Events(sequences[0], 0)
		require.NoError(t, err)
		require.Len(t, tail, 2, "an observer reads what came after the last event it saw")
		require.Equal(t, "guest.clone", tail[0].Kind)
		require.Equal(t, model.LevelInfo, tail[0].Level)
		return nil
	}))
}

func TestConcurrentWritersQueueRatherThanCollide(t *testing.T) {
	f := open(t)
	b, r, p := f.seed(t)
	const writers = 8
	errs := make(chan error, writers)
	for i := range writers {
		go func() {
			errs <- f.update(t, func(tx store.Tx) error {
				number, err := tx.NextRunNumber()
				if err != nil {
					return err
				}
				return tx.AddRun(model.Run{ID: model.RunID(fmt.Sprintf("run_w%d", i)), Branch: b.ID, Revision: r.ID, Plan: p.ID, Number: number, Origin: model.OriginServe, State: model.RunQueued, CreatedAt: at})
			})
		}()
	}
	for range writers {
		require.NoError(t, <-errs)
	}
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		runs, err := rd.Runs(store.RunFilter{})
		require.NoError(t, err)
		require.Len(t, runs, writers)
		require.Equal(t, writers, runs[0].Number, "numbers are 1 to 8 with none repeated")
		return nil
	}))
}
