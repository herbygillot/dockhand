package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
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

// db is the fixture's database, for counting what a test can't read
// through the store.
func (f fixture) db(t *testing.T) *sql.DB {
	t.Helper()
	return f.store.db
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
	return model.Branch{ID: id, Repository: f.repo, Name: name, Base: "base", Worktree: "/w/" + name, Managed: true, State: model.BranchOpen, CreatedAt: at, Origin: model.OriginPerson}
}

// seed records a branch, a snapshot revision, and a plan with two targets,
// libharbor before harbor-cli.
func (f fixture) seed(t *testing.T) (model.Branch, model.Revision, model.Plan) {
	t.Helper()
	b := f.branch("br_1", "dockhand/libharbor-2")
	r := model.Revision{ID: "rev_1", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 1, Source: model.Source{Tree: "tree", Base: "base"}, Head: "head", CreatedAt: at}
	p := model.Plan{ID: "plan_1", Revision: r.ID, Tests: model.TestsDeclared, CreatedAt: at, Environments: []model.Environment{tahoe},
		Targets: []model.PlanTarget{
			{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed},
			{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}, Directory: "devel/harbor-cli", Kind: model.RevisionOnly, Role: model.Changed},
		},
		Builds: []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"libharbor", "harbor-cli"},
			Dependencies: map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}}}}}
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

// A database a newer build migrates while this one has it open is refused
// at this one's next transaction, as opening it would be: a serve started
// on an older build went on with its own code against the newer schema
// (the person's question, 2026-10-06).
func TestANewerSchemaIsRefusedWhileOpen(t *testing.T) {
	t.Parallel()
	f := open(t)
	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", schemaVersion+1))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	err = f.store.View(t.Context(), f.repo, func(store.Reader) error { return nil })
	require.ErrorIs(t, err, store.ErrSchema)
	require.ErrorContains(t, err, "newer than this dockhand supports")
}

func TestOpenCreatesReopensAndRefusesOtherDatabases(t *testing.T) {
	t.Parallel()
	f := open(t)
	again, err := f.store.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)
	require.Equal(t, f.repo, again, "a registration is found, not repeated")
	_, err = f.store.Register(t.Context(), "relative/.git")
	require.ErrorIs(t, err, model.ErrInvalid)
	// The checkouts registered are listed, oldest first, for a command
	// run outside any to find the one it means.
	_, err = f.store.Register(t.Context(), "/src/second/.git")
	require.NoError(t, err)
	dirs, err := f.store.Repositories(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"/src/macports-ports/.git", "/src/second/.git"}, dirs)
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
	t.Parallel()
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
	t.Parallel()
	f := open(t)
	b, _, _ := f.seed(t)
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(f.branch("br_2", b.Name)) }), store.ErrConflict, "one open branch per name")

	b.Title, b.PullRequest = "libharbor: update to 2.0", &model.PullRequest{Repository: "macports/macports-ports", Number: 34901, Head: "ada/macports-ports:dockhand/libharbor-2"}
	// A person's note for the pull request is the branch's (schema 27).
	b.Note = "The tests failed on a permission error,\nbefore any test ran."
	// The ports it was started for are the branch's (schema 32).
	b.Ports = []string{"libharbor", "harbor-cli"}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		got, err := r.BranchNamed(b.Name)
		require.NoError(t, err)
		require.Equal(t, b, got)
		return nil
	}))

	merged := b
	merged.State, merged.EndedAt = model.BranchMerged, at.Add(time.Hour)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(merged) }))
	reopened := merged
	reopened.State = model.BranchOpen
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(reopened) }), store.ErrConflict, "a merge is final")
	reused := f.branch("br_2", b.Name)
	reused.CreatedAt = at.Add(time.Hour)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(reused) }), "a merged branch's name can be used again")

	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		open, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		require.NoError(t, err)
		require.Len(t, open, 1)
		require.Equal(t, reused.ID, open[0].ID)
		got, err := r.MergedBranchNamed(b.Name)
		require.NoError(t, err)
		require.Equal(t, merged.ID, got.ID, "the merged record, not the open branch that took its name")
		_, err = r.MergedBranchNamed("dockhand/never")
		require.ErrorIs(t, err, store.ErrNotFound)
		return nil
	}))
	reused.State, reused.EndedAt = model.BranchMerged, at.Add(2*time.Hour)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(reused) }))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		got, err := r.MergedBranchNamed(b.Name)
		require.NoError(t, err)
		require.Equal(t, reused.ID, got.ID, "the newest of the merged branches of a name")
		return nil
	}))
}

func TestSnapshotsAreNumberedInOrder(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	f := open(t)
	_, revision, p := f.seed(t)
	// A target built with variants keeps them, and is known by them.
	variant := p
	variant.ID = "plan_2"
	variant.Targets = []model.PlanTarget{{ID: "libharbor -docs +tests", Target: model.Target{Name: "libharbor", Variants: map[string]bool{"docs": false, "tests": true}},
		Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}}
	variant.Builds = []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"libharbor -docs +tests"}}}
	variant.Revision = revision.ID
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddPlan(variant) }))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		for _, want := range []model.Plan{p, variant} {
			got, err := r.Plan(want.ID)
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
		return nil
	}))
}

// A plan recorded before each environment had its own is refused by name,
// not read as one with nothing to build.
func TestAPlanRecordedBeforeEnvironmentPlansIsRefused(t *testing.T) {
	t.Parallel()
	f := open(t)
	_, r, _ := f.seed(t)
	arm, _ := json.Marshal(tahoe)
	body := `{"ID": "plan_old", "Revision": "` + string(r.ID) + `", "Environments": [` + string(arm) + `],
		"Targets": [{"ID": "libharbor", "Target": {"Name": "libharbor"}, "Directory": "devel/libharbor", "Kind": "substantive", "Role": "changed"}],
		"Tests": "declared", "CreatedAt": "2026-09-25T12:00:00Z"}`
	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO plans(repository_id, id, revision_id, body, created_at) VALUES(?,?,?,?,?)", f.repo, "plan_old", r.ID, body, millis(at))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		_, err := rd.Plan("plan_old")
		require.ErrorIs(t, err, store.ErrUnavailable)
		require.ErrorContains(t, err, "plan plan_old: recorded before each environment had a plan of its own")
		return nil
	}))
}

func TestRunsAreNumberedImmutableAndMoveForward(t *testing.T) {
	t.Parallel()
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

	// Its start is when it took its slot, and stands once recorded
	// (schema 53).
	started := at.Add(time.Minute)
	running := first
	running.State, running.StartedAt = model.RunRunning, &started
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(running) }))
	restarted := running
	restarted.StartedAt = new(at.Add(2 * time.Minute))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.UpdateRun(restarted) }), store.ErrConflict, "a start stands once recorded")
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
		waited, ok := got.Waited()
		require.True(t, ok)
		require.Equal(t, time.Minute, waited)
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
	t.Parallel()
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
	passed := result("libharbor", model.OutcomePassed, "")
	passed.Links = []model.Link{{Binary: "/opt/local/bin/harbor", Library: "/opt/local/lib/libz.1.dylib", Port: "zlib"}}
	passed.Lint, passed.Nitpicked = []string{"Line 3 has trailing whitespace"}, true
	passed.Declared = &model.Declared{Ports: []model.DeclaredPort{{Port: "oniguruma", Libraries: []string{"/opt/local/lib/libonig.5.dylib"}, Modules: []model.PkgConfigModule{{Name: "oniguruma", Libs: []string{"onig"}, Requires: []string{}}}}}, MachOs: 2}
	passed.Carried = &model.Carried{Check: "check-766", Commit: "3f2a1c9", Ports: []model.InertPort{{Port: "libharbor", Options: []string{"maintainers"}, From: "devel/libharbor"}}, Linted: true}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(passed) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("harbor-cli", model.OutcomeNotRun, "")) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(result("harbor-cli", model.OutcomeInterrupted, "")) }))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return tx.RecordResult(result("harbor-cli", model.OutcomeFailed, model.PhaseInstall))
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		return tx.RecordResult(result("libharbor", model.OutcomeFailed, model.PhaseTest))
	}), store.ErrConflict, "a verdict is final")
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
		for _, got := range results {
			if got.Target == "libharbor" {
				require.Equal(t, passed.Links, got.Links, "what its binaries link")
				require.Equal(t, passed.Lint, got.Lint, "what lint warned of (schema 48)")
				require.True(t, got.Nitpicked)
				require.Equal(t, passed.Carried, got.Carried, "how it's carried over an inert change (schema 56)")
				require.Equal(t, passed.Declared, got.Declared, "what its declared ports install (schema 57)")
			} else {
				require.Nil(t, got.Links, "not read is nil, never none (schema 39)")
				require.Nil(t, got.Carried)
				require.Nil(t, got.Declared)
			}
		}
		executions, err := rd.Executions(run.ID)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2}, []int{executions[0].Attempt, executions[1].Attempt})
		return nil
	}))
}

// A result names what its build read by key, and the record is kept once
// for every build that read the same (decision 28).
func TestAResultKeepsWhatItsBuildRead(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	run := f.run(t, b, r, p)
	execution := model.GuestExecution{ID: "ex_1", Run: run.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(execution) }))
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil,
		[]model.ActivePort{{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "3333333333333333333333333333333333333333", Archive: "sha256:44"}})

	missing := model.TargetResult{Execution: execution.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: inputs.Key(), RecordedAt: at}
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(missing) }), store.ErrNotFound, "inputs are recorded before the result naming them")
	elsewhere := missing
	elsewhere.Execution = "ex_none"
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(elsewhere) }), store.ErrNotFound, "a result is recorded in an execution there is")

	var keys []string
	for range 2 {
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			key, err := tx.RecordInputs(inputs)
			keys = append(keys, key)
			return err
		}))
	}
	require.Equal(t, []string{inputs.Key(), inputs.Key()}, keys, "recorded once, by content")
	built := missing
	built.Archive = "sha256:55"
	built.Detail = "a dependency failed to install: zlib"
	built.Builders = []model.BuilderResult{{Builder: "macos-14", Outcome: model.OutcomeNotRun}, {Builder: "macos-15", Outcome: model.OutcomePassed, Tests: model.TestsPassed, Log: "15.log"}}
	built.Log, built.Steps = "target-1.log", []model.LogStep{{Name: model.StepLint, Line: 1}, {Name: model.StepDependencies, Line: 4}, {Name: model.StepFetch, Line: 46400}}
	borrowed := built
	borrowed.ReusedFrom = "ex_none"
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(borrowed) }), store.ErrNotFound, "a result is reused from an execution there is")
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(built) }))

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		results, err := rd.Results(execution.ID)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "sha256:55", results[0].Archive)
		require.Equal(t, "a dependency failed to install: zlib", results[0].Detail)
		require.Equal(t, built.Builders, results[0].Builders)
		require.Equal(t, built.Steps, results[0].Steps, "where each step began in its log")
		read, err := rd.Inputs(results[0].Inputs)
		require.NoError(t, err)
		require.Equal(t, inputs, read)
		_, err = rd.Inputs("sha256:none")
		require.ErrorIs(t, err, store.ErrNotFound)
		return nil
	}))
}

// A target's reusable results are its passed builds in the environment
// that recorded what they read, newest first, each with the execution
// that built it; a failure, a reuse, and a build that read nobody knows
// what are not (decision 28).
func TestReusableResultsComeWithTheirBuilds(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	first, second := f.run(t, b, r, p), f.run(t, b, r, p)
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil, nil)
	executions := []model.GuestExecution{
		{ID: "ex_old", Run: first.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at},
		{ID: "ex_new", Run: first.ID, Environment: tahoe, Attempt: 2, State: model.ExecutionWaiting, CreatedAt: at, Observed: model.Observed{MacOS: "26.0", Architecture: "arm64"}},
		{ID: "ex_reused", Run: first.ID, Environment: tahoe, Attempt: 3, State: model.ExecutionWaiting, CreatedAt: at, Reused: true},
		{ID: "ex_failed", Run: second.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at},
	}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		key, err := tx.RecordInputs(inputs)
		if err != nil {
			return err
		}
		for i, e := range executions {
			if err := tx.AddExecution(e); err != nil {
				return err
			}
			result := model.TargetResult{Execution: e.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: key, RecordedAt: at.Add(time.Duration(i) * time.Minute)}
			switch e.ID {
			case "ex_reused":
				result.ReusedFrom = "ex_old"
			case "ex_failed":
				// A dependency's failure keeps which dependency, and its
				// version, for a standing acceptance (schema 36).
				result.Outcome, result.Phase = model.OutcomeFailed, model.PhaseDependencies
				result.Dependency = model.FailedDependency{Name: "libgcc15", Version: "15.2.0_1"}
			}
			if err := tx.RecordResult(result); err != nil {
				return err
			}
			// A build that couldn't say what it read.
			if err := tx.RecordResult(model.TargetResult{Execution: e.ID, Target: "harbor-cli", Outcome: model.OutcomePassed, Tests: model.TestsNone, RecordedAt: at}); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		builds, err := rd.Reusable("libharbor", tahoe, 5)
		require.NoError(t, err)
		require.Len(t, builds, 2)
		require.Equal(t, []model.ExecutionID{"ex_new", "ex_old"}, []model.ExecutionID{builds[0].Result.Execution, builds[1].Result.Execution}, "newest first")
		for _, build := range builds {
			built, err := rd.Execution(build.Result.Execution)
			require.NoError(t, err)
			require.Equal(t, built, build.Execution, "the execution that built it, read whole")
		}
		require.Equal(t, "26.0", builds[0].Execution.Observed.MacOS)
		failed, err := rd.Results("ex_failed")
		require.NoError(t, err)
		i := slices.IndexFunc(failed, func(r model.TargetResult) bool { return r.Target == "libharbor" })
		require.Equal(t, model.FailedDependency{Name: "libgcc15", Version: "15.2.0_1"}, failed[i].Dependency)

		builds, err = rd.Reusable("libharbor", tahoe, 1)
		require.NoError(t, err)
		require.Len(t, builds, 1)
		none, err := rd.Reusable("harbor-cli", tahoe, 5)
		require.NoError(t, err)
		require.Empty(t, none)
		return nil
	}))
}

// An execution's environment is the whole environment: a run's attempt in
// one platform with the Command Line Tools alone is not its attempt there
// with Xcode.
func TestAnExecutionsEnvironmentIncludesItsDeveloperTools(t *testing.T) {
	t.Parallel()
	f := open(t)
	tools, xcode := tahoe, tahoe
	tools.DeveloperTools, xcode.DeveloperTools = model.DeveloperToolsCommandLine, model.DeveloperToolsXcode
	b := f.branch("br_1", "dockhand/libharbor-2")
	r := model.Revision{ID: "rev_1", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 1, Source: model.Source{Tree: "tree", Base: "base"}, Head: "head", CreatedAt: at}
	p := model.Plan{ID: "plan_1", Revision: r.ID, Tests: model.TestsDeclared, CreatedAt: at, Environments: []model.Environment{tools, xcode},
		Targets: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}},
		Builds:  []model.EnvironmentPlan{{Environment: tools, Order: []model.TargetID{"libharbor"}}, {Environment: xcode, Order: []model.TargetID{"libharbor"}}}}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return errors.Join(tx.AddBranch(b), tx.AddRevision(r), tx.AddPlan(p))
	}))
	run := f.run(t, b, r, p)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return errors.Join(
			tx.AddExecution(model.GuestExecution{ID: "ex_tools", Run: run.ID, Environment: tools, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}),
			tx.AddExecution(model.GuestExecution{ID: "ex_xcode", Run: run.ID, Environment: xcode, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}))
	}))
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		return tx.AddExecution(model.GuestExecution{ID: "ex_again", Run: run.ID, Environment: xcode, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at})
	}), store.ErrConflict, "one attempt in each whole environment")
}

// A local check's environment includes its prefix (schema 41): one check
// builds in two prefixes on this Mac, each its own attempt, and reuse
// reads a prefix's builds alone.
func TestAnExecutionsEnvironmentIncludesItsPrefix(t *testing.T) {
	t.Parallel()
	f := open(t)
	local := model.Environment{Provider: "local", Platform: tahoe.Platform, Prefix: "/opt/local"}
	test := local
	test.Prefix = "/opt/macports-test"
	b := f.branch("br_1", "dockhand/libharbor-2")
	r := model.Revision{ID: "rev_1", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 1, Source: model.Source{Tree: "tree", Base: "base"}, Head: "head", CreatedAt: at}
	p := model.Plan{ID: "plan_1", Revision: r.ID, Tests: model.TestsDeclared, CreatedAt: at, Environments: []model.Environment{local, test},
		Targets: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}},
		Builds:  []model.EnvironmentPlan{{Environment: local, Order: []model.TargetID{"libharbor"}}, {Environment: test, Order: []model.TargetID{"libharbor"}}}}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return errors.Join(tx.AddBranch(b), tx.AddRevision(r), tx.AddPlan(p))
	}))
	run := f.run(t, b, r, p)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		return errors.Join(
			tx.AddExecution(model.GuestExecution{ID: "ex_local", Run: run.ID, Environment: local, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}),
			tx.AddExecution(model.GuestExecution{ID: "ex_test", Run: run.ID, Environment: test, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}))
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		executions, err := rd.Executions(run.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"/opt/local", "/opt/macports-test"}, []string{executions[0].Environment.Prefix, executions[1].Environment.Prefix})
		return nil
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		found, err := tx.Reusable("libharbor", test, 5)
		require.Empty(t, found)
		return err
	}))
}

// schemaAt is a database at an earlier schema version, with setup run
// in it.
func schemaAt(t *testing.T, version int, setup string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dockhand.db")
	require.NoError(t, CreateAt(path, version, setup))
	return path
}

// A run's executions and their results, as schema 23 kept them.
const schema23Records = `
INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);
INSERT INTO branches(repository_id, id, name, base, worktree, managed, title, state, created_at) VALUES('repo_1','br_1','dockhand/jq-4k2p','base','/w',1,'','open',1);
INSERT INTO revisions VALUES('repo_1','rev_1','br_1','snapshot',1,'','tree','base','head',1);
INSERT INTO plans VALUES('repo_1','plan_1','rev_1','{}',1);
INSERT INTO runs(repository_id, id, number, branch_id, revision_id, plan_id, origin, state, detail, created_at) VALUES('repo_1','run_1',1,'br_1','rev_1','plan_1','person','running','',1);
INSERT INTO executions(repository_id, id, run_id, provider, platform_os, platform_version, platform_architecture, attempt, state, detail, provider_ref, created_at, developer_tools, observed, identity, reused)
 VALUES('repo_1','ex_2','run_1','tart','darwin','25','arm64',2,'waiting','','clone-2',5,'xcode','{"macos":"26.0"}','image sha256:1',0),
       ('repo_1','ex_1','run_1','tart','darwin','25','arm64',1,'waiting','','clone-1',5,'xcode','','',0);
INSERT INTO results(repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at, archive, detail, builders, reused_from)
 VALUES('repo_1','ex_1','zlib','passed','','none','zlib.log','',7,'sha256:aa','','',''),
       ('repo_1','ex_1','jq','failed','install','none','jq.log','',7,'','no space','',''),
       ('repo_1','ex_2','jq','not-run','','none','','',7,'','','','');
`

// A run recorded before schema 53 kept no start, and has none after it:
// how long it waited isn't known, so nothing says it.
func TestARunBeforeSchema53HasNoStart(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), schemaAt(t, 52, schema23Records), Options{})
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		run, err := r.Run("run_1")
		require.NoError(t, err)
		require.Nil(t, run.StartedAt)
		_, ok := run.Waited()
		require.False(t, ok)
		return nil
	}))
}

// Schema 24 rebuilds executions and results; what they held comes
// through, in the order it was made, with the rules the tables kept.
func TestExecutionsAndResultsSurviveTheirRebuild(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), schemaAt(t, 23, schema23Records), Options{})
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		executions, err := r.Executions("run_1")
		require.NoError(t, err)
		require.Equal(t, []model.ExecutionID{"ex_2", "ex_1"}, []model.ExecutionID{executions[0].ID, executions[1].ID}, "made at the same time, in the order made")
		require.Equal(t, model.DeveloperToolsXcode, executions[0].Environment.DeveloperTools)
		require.Equal(t, "26.0", executions[0].Observed.MacOS)
		require.Equal(t, "image sha256:1", executions[0].Identity)
		results, err := r.Results("ex_1")
		require.NoError(t, err)
		require.Equal(t, []model.TargetID{"zlib", "jq"}, []model.TargetID{results[0].Target, results[1].Target}, "recorded at the same time, in the order recorded")
		require.Equal(t, "sha256:aa", results[0].Archive)
		require.Equal(t, "no space", results[1].Detail)
		referred, err := r.ExecutionsReferred("clone-1")
		require.NoError(t, err)
		require.Len(t, referred, 1)
		return nil
	}))
	_, err = s.db.Exec("UPDATE results SET outcome='passed' WHERE execution_id='ex_1' AND target_id='jq'")
	require.ErrorContains(t, err, "a complete target result is final", "the trigger was made again")
	_, err = s.db.Exec("INSERT INTO results(repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at) VALUES('repo_1','ex_none','jq','passed','','none','','',8)")
	require.ErrorContains(t, err, "FOREIGN KEY", "results refer to the rebuilt executions")
	var violations int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations))
	require.Zero(t, violations)
}

// A migration that fails leaves the database as it was: the rebuild
// refuses a result whose execution is gone, which foreign keys, off when
// this one was written, would have refused before it.
func TestAFailedMigrationChangesNothing(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, 23, schema23Records+"INSERT INTO results(repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at) VALUES('repo_1','ex_gone','jq','passed','','none','','',8);")
	_, err := Open(t.Context(), path, Options{})
	require.ErrorContains(t, err, "schema 24")
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer db.Close()
	var version, executions, results int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.NoError(t, db.QueryRow("SELECT (SELECT count(*) FROM executions), (SELECT count(*) FROM results)").Scan(&executions, &results))
	require.Equal(t, []int{23, 2, 4}, []int{version, executions, results})
}

// A kept archive is recorded once by its digest, with the file name
// MacPorts gives it; one that can't be named as a file, or has no digest,
// isn't recorded (decision 28).
func TestAnArchiveIsRecordedOnceByDigest(t *testing.T) {
	t.Parallel()
	f := open(t)
	f.seed(t)
	archive := model.Archive{Digest: "sha256:55", Name: "libharbor-4_0.darwin_25.arm64.tbz2", Size: 1024, KeptAt: at}
	for range 2 {
		require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.KeepArchive(archive) }))
	}
	for _, bad := range []model.Archive{{Digest: "sha256:66", Name: "../x.tbz2", Size: 1}, {Digest: "66", Name: "x.tbz2", Size: 1}, {Digest: "sha256:66", Name: "x.tbz2"}} {
		require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.KeepArchive(bad) }), store.ErrConflict)
	}
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		read, err := rd.Archive(archive.Digest)
		require.NoError(t, err)
		require.Equal(t, archive, read)
		_, err = rd.Archive("sha256:66")
		require.ErrorIs(t, err, store.ErrNotFound)
		return nil
	}))
}

// A dependency's kept archives are found by port in an environment, the
// newest two of each first, and go with their archives when pruning
// forgets them (batch 90).
func TestADependencysArchivesAreFoundByPortAndGoWithTheirArchives(t *testing.T) {
	t.Parallel()
	f := open(t)
	f.seed(t)
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	other := tahoe
	other.Platform.Version = "27"
	keep := func(digest, name, port string, environment model.Environment, kept time.Time) {
		t.Helper()
		archive := model.Archive{Digest: digest, Name: name, Size: 10, KeptAt: kept}
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			if err := tx.KeepArchive(archive); err != nil {
				return err
			}
			return tx.KeepDependencyArchive(model.DependencyArchive{Archive: archive, Port: port, Environment: environment})
		}))
	}
	keep("sha256:r1", "rust-1.90.0_0.darwin_25.arm64.tbz2", "rust", tahoe, at)
	keep("sha256:r2", "rust-1.91.0_0.darwin_25.arm64.tbz2", "rust", tahoe, at.Add(time.Hour))
	keep("sha256:r3", "rust-1.91.0_1.darwin_25.arm64.tbz2", "rust", tahoe, at.Add(2*time.Hour))
	keep("sha256:c1", "cargo-1.91.0_0.darwin_25.arm64.tbz2", "cargo", tahoe, at)
	keep("sha256:g1", "rust-1.91.0_0.darwin_27.arm64.tbz2", "rust", other, at)
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error {
		return tx.KeepDependencyArchive(model.DependencyArchive{Archive: model.Archive{Digest: "sha256:r1", KeptAt: at}, Environment: tahoe})
	}), store.ErrConflict, "kept for no port")
	names := func(environment model.Environment, ports ...string) []string {
		t.Helper()
		var found []string
		require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
			archives, err := rd.DependencyArchives(environment, ports)
			for _, archive := range archives {
				found = append(found, archive.Name)
			}
			return err
		}))
		return found
	}
	require.Equal(t, []string{"cargo-1.91.0_0.darwin_25.arm64.tbz2", "rust-1.91.0_1.darwin_25.arm64.tbz2", "rust-1.91.0_0.darwin_25.arm64.tbz2"}, names(tahoe, "rust", "cargo"))
	require.Equal(t, []string{"rust-1.91.0_0.darwin_27.arm64.tbz2"}, names(other, "rust"))
	require.Empty(t, names(tahoe))

	// Each port's newest in each environment lasts until cleanup.after,
	// though no result names it: rust built from source was built again
	// the next day (the rc9 full stage).
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.PruneArchives(at.Add(90*time.Minute), at)
		return err
	}))
	require.Equal(t, []string{"cargo-1.91.0_0.darwin_25.arm64.tbz2", "rust-1.91.0_1.darwin_25.arm64.tbz2"}, names(tahoe, "rust", "cargo"), "the older rust's go")
	require.Equal(t, []string{"rust-1.91.0_0.darwin_27.arm64.tbz2"}, names(other, "rust"))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.PruneArchives(at.Add(90*time.Minute), at.Add(90*time.Minute))
		return err
	}))
	require.Equal(t, []string{"rust-1.91.0_1.darwin_25.arm64.tbz2"}, names(tahoe, "rust", "cargo"), "pruned with their archives once past cleanup.after")
}

// A dependency's kept archive says the PortGroups the guest built it
// with, where it built it from source, and none where it was installed
// from MacPorts' own (schema 54). Kept again by a guest that installed it,
// and so built nothing, it keeps what it was built with.
func TestADependencysArchiveKeepsThePortGroupsItWasBuiltWith(t *testing.T) {
	t.Parallel()
	f := open(t)
	f.seed(t)
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	cargo := map[string]model.ObjectID{"_resources/port1.0/group/cargo-1.0.tcl": "4c18e512dba79e4c8300dd08aeb37f8e728b8dad"}
	keep := func(digest, port string, groups map[string]model.ObjectID, kept time.Time) {
		t.Helper()
		archive := model.Archive{Digest: digest, Name: port + "-1_0.darwin_25.arm64.tbz2", Size: 10, KeptAt: kept}
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			if err := tx.KeepArchive(archive); err != nil {
				return err
			}
			return tx.KeepDependencyArchive(model.DependencyArchive{Archive: archive, Port: port, Environment: tahoe, PortGroups: groups})
		}))
	}
	groups := func() map[string]map[string]model.ObjectID {
		t.Helper()
		found := map[string]map[string]model.ObjectID{}
		require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
			archives, err := rd.DependencyArchives(tahoe, []string{"rust", "zlib"})
			for _, archive := range archives {
				found[archive.Port] = archive.PortGroups
			}
			return err
		}))
		return found
	}
	keep("sha256:r1", "rust", cargo, at)
	keep("sha256:z1", "zlib", nil, at)
	require.Equal(t, map[string]map[string]model.ObjectID{"rust": cargo, "zlib": nil}, groups())
	keep("sha256:r1", "rust", nil, at.Add(time.Hour))
	require.Equal(t, cargo, groups()["rust"], "kept again by a guest that installed it")
}

// A dependency's archive kept before schema 54 says no PortGroups, and is
// offered as it was.
func TestADependencysArchiveBeforeSchema54SaysNoPortGroups(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), schemaAt(t, 53, schema23Records+`
INSERT INTO archives(repository_id, digest, name, size, kept_at) VALUES('repo_1','sha256:r1','rust-1_0.darwin_25.arm64.tbz2',10,1);
INSERT INTO dependency_archives(repository_id, digest, port, provider, platform_os, platform_version, platform_architecture, developer_tools, kept_at)
 VALUES('repo_1','sha256:r1','rust','tart','darwin','25','arm64','xcode',1);
`), Options{})
	require.NoError(t, err)
	defer s.Close()
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
		archives, err := r.DependencyArchives(tahoe, []string{"rust"})
		require.NoError(t, err)
		require.Len(t, archives, 1)
		require.Equal(t, "sha256:r1", archives[0].Digest)
		require.Nil(t, archives[0].PortGroups)
		return nil
	}))
}

// An archive is forgotten once no live result names it (D6): an open
// branch's newest passed result of each target in each environment keeps
// its own, and the newest passed build reuse may choose keeps its own
// whatever its branch; an older build's goes, however recent. One kept
// since the cutoff stays however it is named (decisions 36 and 44).
func TestArchivesGoWhenNoLiveResultNamesThem(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	run := f.run(t, b, r, p)
	later := at.Add(2 * time.Hour)
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil, nil)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		key, err := tx.RecordInputs(inputs)
		if err != nil {
			return err
		}
		for i, result := range []model.TargetResult{
			{Target: "libharbor", Archive: "sha256:aa", Inputs: key, RecordedAt: at},
			{Target: "libharbor", Archive: "sha256:a2", Inputs: key, RecordedAt: later},
			// One that recorded nothing it read, which reuse can't choose.
			{Target: "harbor-cli", Archive: "sha256:cc", RecordedAt: later},
		} {
			execution := model.GuestExecution{ID: model.ExecutionID(fmt.Sprintf("ex_%d", i+1)), Run: run.ID, Environment: tahoe, Attempt: i + 1, State: model.ExecutionWaiting, CreatedAt: at}
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			result.Execution, result.Outcome, result.Tests = execution.ID, model.OutcomePassed, model.TestsNone
			if err := tx.RecordResult(result); err != nil {
				return err
			}
		}
		for _, archive := range []model.Archive{
			{Digest: "sha256:aa", Name: "libharbor-1.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:a2", Name: "libharbor-2.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:bb", Name: "unnamed.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:cc", Name: "harbor-cli.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:dd", Name: "fresh.tbz2", Size: 1, KeptAt: later},
			// Kept after bb, and listed before it: what's forgotten is
			// listed by digest.
			{Digest: "sha256:ab", Name: "unnamed-too.tbz2", Size: 1, KeptAt: at},
		} {
			if err := tx.KeepArchive(archive); err != nil {
				return err
			}
		}
		return nil
	}))
	prune := func() []string {
		t.Helper()
		var digests []string
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			pruned, err := tx.PruneArchives(at.Add(time.Hour), at.Add(time.Hour))
			for _, archive := range pruned {
				digests = append(digests, archive.Digest)
			}
			return err
		}))
		return digests
	}
	require.Equal(t, []string{"sha256:aa", "sha256:ab", "sha256:bb"}, prune(), "no result names two, and libharbor's older build is superseded")
	b.State, b.EndedAt = model.BranchMerged, at
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.Equal(t, []string{"sha256:cc"}, prune(), "a retired branch's goes, but for what reuse may choose")
	require.Empty(t, prune())
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		kept, err := rd.Archives()
		require.NoError(t, err)
		require.Equal(t, []string{"sha256:a2", "sha256:dd"}, []string{kept[0].Digest, kept[1].Digest})
		return nil
	}))
}

// A branch records when it leaves open, and forgets it when it opens
// again; a time given is kept (D6).
func TestABranchRecordsWhenItEnded(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, _, _ := f.seed(t)
	read := func() model.Branch {
		t.Helper()
		var got model.Branch
		require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
			var err error
			got, err = rd.Branch(b.ID)
			return err
		}))
		return got
	}
	require.True(t, read().EndedAt.IsZero())
	b.State, b.EndedAt = model.BranchArchived, at.Add(time.Hour)
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.Equal(t, at.Add(time.Hour), read().EndedAt)
	b.State, b.EndedAt = model.BranchOpen, time.Time{}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.True(t, read().EndedAt.IsZero(), "opened again")
	b.State = model.BranchClosed
	require.ErrorContains(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }), "leaves open, so it says when",
		"the store invents no time (the architecture review's X5)")
}

// What an ended branch recorded of its checks goes once it's been ended
// past the cutoff (D6): its runs but its newest, which status shows, with
// their executions, results, plans, and revisions, and its assessments;
// but a result reuse may still choose stays with its execution and run,
// and so does an execution a remaining result reused. An open branch's
// records, and a branch ended since the cutoff, are untouched.
func TestAnEndedBranchsHistoryGoesButWhatReuseNeeds(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	old, reusedFrom, newest := f.run(t, b, r, p), f.run(t, b, r, p), f.run(t, b, r, p)
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil, nil)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		key, err := tx.RecordInputs(inputs)
		if err != nil {
			return err
		}
		for _, e := range []struct {
			execution model.ExecutionID
			run       model.RunID
			result    model.TargetResult
		}{
			{"ex_old", old.ID, model.TargetResult{Target: "harbor-cli", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, RecordedAt: at}},
			{"ex_built", reusedFrom.ID, model.TargetResult{Target: "libharbor", Outcome: model.OutcomePassed, Inputs: key, Archive: "sha256:aa", RecordedAt: at}},
			{"ex_newest", newest.ID, model.TargetResult{Target: "harbor-cli", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, RecordedAt: at}},
		} {
			if err := tx.AddExecution(model.GuestExecution{ID: e.execution, Run: e.run, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}); err != nil {
				return err
			}
			e.result.Execution, e.result.Tests = e.execution, model.TestsNone
			if err := tx.RecordResult(e.result); err != nil {
				return err
			}
		}
		return tx.RecordAssessment(model.Assessment{Branch: b.ID, Tree: "tree", Base: "base", Port: "libharbor", Directory: "devel/libharbor", Policy: 1, At: at})
	}))
	count := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, f.db(t).QueryRow("SELECT count(*) FROM "+table).Scan(&n))
		return n
	}
	pruneAt := func(before time.Time) store.Pruned {
		t.Helper()
		var pruned store.Pruned
		for from := model.BranchID(""); ; {
			require.NoError(t, f.update(t, func(tx store.Tx) error {
				chunk, next, err := tx.PruneHistory(before, from, 10)
				pruned.Add(chunk)
				from = next
				return err
			}))
			if from == "" {
				return pruned
			}
		}
	}
	require.Equal(t, store.Pruned{}, pruneAt(at.Add(time.Hour)), "an open branch keeps everything")
	b.State, b.EndedAt = model.BranchMerged, at
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.Equal(t, store.Pruned{}, pruneAt(at), "ended since the cutoff")
	require.Equal(t, store.Pruned{Runs: 1, Executions: 2, Results: 2, Assessments: 1}, pruneAt(at.Add(time.Hour)))
	require.Equal(t, 2, count("runs"), "the newest run, and the one whose build reuse may choose")
	require.Equal(t, []int{1, 1, 1, 1, 1}, []int{count("executions"), count("results"), count("plans"), count("revisions"), count("inputs")})
	require.Equal(t, store.Pruned{}, pruneAt(at.Add(time.Hour)))
	var violations int
	require.NoError(t, f.db(t).QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations))
	require.Zero(t, violations)
}

// History goes a chunk of branches at a time, in ID order, each call
// saying the last branch it took for the next, and "" once none is left,
// when the inputs no result names go too (the SQL study's SQL-S2).
func TestHistoryIsPrunedAChunkAtATime(t *testing.T) {
	t.Parallel()
	f := open(t)
	first, r, p := f.seed(t)
	second := f.branch("br_2", "dockhand/jq-2")
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddBranch(second) }))
	for _, b := range []*model.Branch{&first, &second} {
		b.State, b.EndedAt = model.BranchMerged, at
		require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(*b) }))
	}
	f.run(t, first, r, p)
	f.run(t, first, r, p)
	var chunks []model.BranchID
	var pruned store.Pruned
	for from := model.BranchID(""); ; {
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			chunk, next, err := tx.PruneHistory(at.Add(time.Hour), from, 1)
			pruned.Add(chunk)
			from = next
			return err
		}))
		chunks = append(chunks, from)
		if from == "" {
			break
		}
	}
	require.Equal(t, []model.BranchID{first.ID, second.ID, ""}, chunks)
	require.Equal(t, 1, pruned.Runs, "the older of br_1's two runs; its newest stays")
}

// Pruning history reaches its rows through indexes: no statement scans a
// table from a correlated subquery, as three did, which went quadratic in
// history and passed the prune's timeout at a year's scale (the SQL
// study's SQL-S1).
func TestHistoryIsPrunedWithoutCorrelatedScans(t *testing.T) {
	t.Parallel()
	f := open(t)
	branches := []string{"br_1", "br_2"}
	steps := pruneBranchSteps(len(branches))
	steps = append(steps, pruneStep{query: pruneInputs, layout: "rr"})
	for _, step := range steps {
		for _, scan := range f.correlatedScans(t, step.query, step.args(f.repo, branches)...) {
			t.Errorf("%s\nscans %s from a correlated subquery", step.query, scan)
		}
	}
}

// correlatedScans are the plan's full scans of a table that run under a
// correlated subquery: once for each row of the outer statement.
func (f fixture) correlatedScans(t *testing.T, query string, args ...any) []string {
	t.Helper()
	rows, err := f.store.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	parents, details := map[int]int{}, map[int]string{}
	var ids []int
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		parents[id], details[id] = parent, detail
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	var scans []string
	for _, id := range ids {
		if !strings.HasPrefix(details[id], "SCAN ") || strings.Contains(details[id], " INDEX ") {
			continue
		}
		for up := parents[id]; up != 0; up = parents[up] {
			if strings.HasPrefix(details[up], "CORRELATED ") {
				scans = append(scans, details[id])
				break
			}
		}
	}
	return scans
}

// An open branch's assessments of a tree it moved past go once its newer
// revision is older than the cutoff; those of its newest tree stay (D6).
func TestAssessmentsOfATreeMovedPastGo(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, _, _ := f.seed(t)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddRevision(model.Revision{ID: "rev_2", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 2, Source: model.Source{Tree: "tree2", Base: "base"}, Head: "head", CreatedAt: at.Add(time.Hour)}); err != nil {
			return err
		}
		for _, tree := range []model.ObjectID{"tree", "tree2"} {
			if err := tx.RecordAssessment(model.Assessment{Branch: b.ID, Tree: tree, Base: "base", Port: "libharbor", Directory: "devel/libharbor", Policy: 1, At: at}); err != nil {
				return err
			}
			if err := tx.RecordChange(model.ChangeRecord{Branch: b.ID, Tree: tree, Base: "base", Directory: "devel/libharbor", Policy: 1, At: at}); err != nil {
				return err
			}
		}
		return nil
	}))
	prune := func(before time.Time) int {
		t.Helper()
		var n int
		require.NoError(t, f.update(t, func(tx store.Tx) error {
			var err error
			n, err = tx.PruneAssessments(before)
			return err
		}))
		return n
	}
	require.Zero(t, prune(at.Add(time.Hour)), "moved past only since the cutoff")
	require.Equal(t, 2, prune(at.Add(2*time.Hour)), "the old tree's assessment and change record")
	require.Zero(t, prune(at.Add(2*time.Hour)))
}

func session(f fixture, id model.SessionID, kind model.SessionKind) model.Session {
	return model.Session{ID: id, Repository: f.repo, Kind: kind, PID: 4711, ProcessStart: "linux:12345", Version: "test", StartedAt: at, HeartbeatAt: at}
}

func TestLeasesAreFenced(t *testing.T) {
	t.Parallel()
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
	// One for a resource never leased, or since gone, is stale too, and
	// says so (the test plan's step 2, item 22).
	never := model.Lease{Resource: "never-leased", Holder: "ses_a", Generation: 1}
	err := f.update(t, func(tx store.Tx) error { return tx.ReleaseLease(never) })
	require.ErrorIs(t, err, store.ErrStale)
	require.ErrorContains(t, err, "no lease on never-leased")

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
		a, err := r.Session("ses_a")
		require.NoError(t, err)
		require.Equal(t, endedAt, *a.EndedAt)
		b, err := r.Session("ses_b")
		require.NoError(t, err)
		require.Nil(t, b.EndedAt)
		return nil
	}))

	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec("UPDATE leases SET generation=1 WHERE resource='leader'")
	require.ErrorContains(t, err, "never decreases")
}

func TestEventsAreAJournal(t *testing.T) {
	t.Parallel()
	f := open(t)
	var sequences []int64
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for i, kind := range []model.EventKind{model.EventRunState, "guest.clone", model.EventTargetResult} {
			seq, err := tx.AppendEvent(model.Event{At: at.Add(time.Duration(i) * time.Second), Session: "ses_a", Run: "run_1", Kind: kind, Message: string(kind)})
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
		tail, err := r.Events(sequences[0], 0, "")
		require.NoError(t, err)
		require.Len(t, tail, 2, "an observer reads what came after the last event it saw")
		require.Equal(t, model.EventKind("guest.clone"), tail[0].Kind)
		require.Equal(t, model.LevelInfo, tail[0].Level)
		counted, err := r.CountEvents("guest.clone", at)
		require.NoError(t, err)
		require.Equal(t, 1, counted)
		counted, err = r.CountEvents("guest.clone", at.Add(2*time.Second))
		require.NoError(t, err)
		require.Zero(t, counted, "only what was journaled since")
		return nil
	}))

	// A run's events are read by the run, and a watcher starts at the
	// newest.
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: at.Add(3 * time.Second), Session: "ses_a", Run: "run_2", Kind: "run.state", Message: "another run"})
		return err
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(r store.Reader) error {
		first, err := r.RunEvents("run_1", 0, 0)
		require.NoError(t, err)
		require.Len(t, first, 3)
		later, err := r.RunEvents("run_1", sequences[1], 0)
		require.NoError(t, err)
		require.Len(t, later, 1)
		require.Equal(t, model.EventTargetResult, later[0].Kind)
		last, err := r.LastEvent()
		require.NoError(t, err)
		require.Greater(t, last, sequences[2])
		// A run's newest event of a kind, as live progress reads it.
		newest, found, err := r.LatestRunEvent("run_1", model.EventTargetResult)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, later[0], newest)
		_, found, err = r.LatestRunEvent("run_2", model.EventTargetResult)
		require.NoError(t, err)
		require.False(t, found)
		return nil
	}))
}

func TestConcurrentWritersQueueRatherThanCollide(t *testing.T) {
	t.Parallel()
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

// planOf is how SQLite would run a query: EXPLAIN QUERY PLAN's details,
// one step to a line.
func (f fixture) planOf(t *testing.T, query string, args ...any) string {
	t.Helper()
	rows, err := f.store.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan += detail + "\n"
	}
	require.NoError(t, rows.Err())
	return plan
}

// The queries that read history find their rows through an index, rather
// than read every row the repository has (docs/reviews/2026-09-28-sql-
// review.md). A change to a query's wording that loses its index fails
// here, not a year of history later.
func TestHistoryIsReadThroughIndexes(t *testing.T) {
	t.Parallel()
	f := open(t)
	reusable := []any{f.repo, "libharbor", "tart", "darwin", "25", "arm64", "", "", 5}
	revisionAssessments, revision := assessmentsQuery(f.repo, store.AssessmentFilter{Branch: "br_1", Tree: "tree", Base: "base"})
	for _, c := range []struct {
		query string
		args  []any
		index string
	}{
		// Schema 41's rebuilt results have SQLite read the builds a reuse
		// may choose by reused_from, which they're all filtered by.
		{"SELECT digest FROM archives a WHERE " + unnamedArchive, []any{f.repo, 1, f.repo, f.repo, f.repo, 1}, "result_reused (repository_id=? AND reused_from=?)"},
		{reusableQuery, reusable, "result_target (repository_id=? AND target_id=?)"},
		{revisionsQuery, []any{f.repo, "br_1"}, "revision_branch (repository_id=? AND branch_id=?)"},
		{referredQuery, []any{f.repo, "clone"}, "execution_ref (repository_id=? AND provider_ref=?)"},
		{countEventsQuery, []any{f.repo, "serve.submit", 1}, "event_kind (repository_id=? AND kind=? AND at>?)"},
		{checkpointsQuery, []any{f.repo, "br_1"}, "checkpoint_branch (repository_id=? AND branch_id=?)"},
		{revisionAssessments, revision, "sqlite_autoindex_assessments_1 (repository_id=? AND branch_id=? AND tree=? AND base=?)"},
	} {
		require.Contains(t, f.planOf(t, c.query, c.args...), "INDEX "+c.index, c.query)
	}
	require.NotContains(t, f.planOf(t, reusableQuery, reusable...), "TEMP B-TREE", "reuse reads the newest first from the index, without sorting")
}

// Every connection syncs through to the disk, and bounds the WAL file a
// checkpoint leaves.
func TestEveryConnectionSyncsToTheDisk(t *testing.T) {
	t.Parallel()
	f := open(t)
	var conns []*sql.Conn
	for range 4 {
		conn, err := f.store.db.Conn(t.Context())
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	for _, conn := range conns {
		for pragma, want := range map[string]int{"synchronous": 2, "fullfsync": 1, "checkpoint_fullfsync": 1, "journal_size_limit": journalSizeLimit, "foreign_keys": 1} {
			var got int
			require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got))
			require.Equal(t, want, got, pragma)
		}
	}
	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}
}

// Opening the database keeps the planner's statistics, without which it
// read every run to find the queued ones serve polls for, sparing itself
// a sort over a few. It needs the sampled values, which show queued runs
// are few; averages alone left it reading every run.
func TestOpeningKeepsThePlannersStatistics(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for number := 1; number <= 200; number++ {
			run := model.Run{ID: model.RunID(fmt.Sprintf("run_%d", number)), Branch: b.ID, Revision: r.ID, Plan: p.ID, Number: number, Origin: model.OriginServe, State: model.RunQueued, CreatedAt: at}
			if err := tx.AddRun(run); err != nil {
				return err
			}
			if number == 200 {
				continue
			}
			run.State = model.RunRunning
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
			finished := at.Add(time.Minute)
			run.State, run.FinishedAt = model.RunPassed, &finished
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
		}
		return nil
	}))
	active, args := runsQuery(f.repo, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	require.NotContains(t, f.planOf(t, active, args...), "run_state", "without statistics")

	require.NoError(t, f.store.Close())
	reopened, err := Open(t.Context(), f.path, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { reopened.Close() })
	f.store = reopened
	require.Contains(t, f.planOf(t, active, args...), "INDEX run_state (repository_id=? AND state=?)")
}

// Migrating a database keeps a copy of it as it was beside it first, for
// the builds that can't open it after, and says what it did; a database at
// this build's schema, or a new one, is neither copied nor migrated. An
// old copy goes once it's old, the one just made staying (the certigo
// run: its first run migrated the database silently).
func TestAMigrationKeepsTheDatabaseAsItWas(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, schemaVersion-1, schema23Records)
	stale := path + ".schema-2"
	require.NoError(t, os.WriteFile(stale, []byte("an old copy"), 0o600))
	old := time.Now().Add(-keptCopies - time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	require.Equal(t, &Migration{From: schemaVersion - 1, To: schemaVersion, Copy: path + fmt.Sprintf(".schema-%d", schemaVersion-1)}, s.Migration())
	require.NoError(t, s.Close())
	require.NoFileExists(t, stale, "an old copy goes")
	requireWholeCopy(t, s.Migration().Copy)

	again, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	require.Nil(t, again.Migration(), "at this build's schema, nothing to migrate")
	require.NoError(t, again.Close())
	fresh, err := Open(t.Context(), filepath.Join(t.TempDir(), "dockhand.db"), Options{})
	require.NoError(t, err)
	require.Nil(t, fresh.Migration(), "a new database is made, not migrated")
	require.NoError(t, fresh.Close())
}

// A copy cut short leaves nothing under the kept copy's name, and the next
// open copies the whole database: VACUUM INTO writes its file as it goes,
// and an empty file it left was taken for the copy and migrated past (the
// SQL review's rescan). A page the copy can't read stops it partway, as a
// timeout or the process's end would. A copy a process left partway is
// never taken for the kept one, and goes once it's old.
func TestACopyCutShortNeverStandsAsTheKeptOne(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, schemaVersion-1, schema23Records+
		"CREATE TABLE filler(x); WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<200) INSERT INTO filler SELECT randomblob(1000) FROM n;")
	copy := path + fmt.Sprintf(".schema-%d", schemaVersion-1)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	defer file.Close()
	info, err := file.Stat()
	require.NoError(t, err)
	last := info.Size() - 4096
	page := make([]byte, 4096)
	_, err = file.ReadAt(page, last)
	require.NoError(t, err)
	_, err = file.WriteAt(bytes.Repeat([]byte{0xff}, len(page)), last)
	require.NoError(t, err)

	_, err = Open(t.Context(), path, Options{})
	require.ErrorIs(t, err, store.ErrUnavailable)
	require.ErrorContains(t, err, "nothing was migrated")
	left, err := filepath.Glob(path + ".schema-*")
	require.NoError(t, err)
	require.Empty(t, left, "a copy cut short leaves nothing")

	_, err = file.WriteAt(page, last)
	require.NoError(t, err)
	abandoned, making := copy+partialCopy+"1", copy+partialCopy+"2"
	for _, partial := range []string{abandoned, making} {
		require.NoError(t, os.WriteFile(partial, []byte("SQLite format 3\x00 and no more"), 0o600))
	}
	old := time.Now().Add(-abandonedCopies - time.Minute)
	require.NoError(t, os.Chtimes(abandoned, old, old))
	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.Equal(t, copy, s.Migration().Copy)
	requireWholeCopy(t, copy)
	require.NoFileExists(t, abandoned, "one left partway long ago goes")
	require.FileExists(t, making, "another process may be making one now")
}

// Processes opening a database to migrate at once all open it, and keep
// one whole copy: each copied it to the one name, which VACUUM INTO
// refuses once it holds a database, failing a later open (the SQL review's
// rescan). A copy finished once another's is in place leaves that one.
func TestOpensAtOnceAllMigrate(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, schemaVersion-1, schema23Records)
	copy := path + fmt.Sprintf(".schema-%d", schemaVersion-1)
	opened := make(chan error)
	for range 4 {
		go func() {
			s, err := Open(t.Context(), path, Options{})
			if err == nil {
				err = s.Close()
			}
			opened <- err
		}()
	}
	for range 4 {
		require.NoError(t, <-opened)
	}
	requireWholeCopy(t, copy)
	left, err := filepath.Glob(path + ".schema-*")
	require.NoError(t, err)
	require.Equal(t, []string{copy}, left, "no copy in the making is left")

	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer s.Close()
	kept, err := os.ReadFile(copy)
	require.NoError(t, err)
	require.NoError(t, s.keepCopy(t.Context(), copy))
	again, err := os.ReadFile(copy)
	require.NoError(t, err)
	require.Equal(t, kept, again, "the copy in place first stays")
	left, err = filepath.Glob(path + ".schema-*")
	require.NoError(t, err)
	require.Equal(t, []string{copy}, left)
}

// requireWholeCopy checks that a migration's copy is the database before
// it, whole, and readable only by its owner.
func requireWholeCopy(t *testing.T, copy string) {
	t.Helper()
	copied, err := sql.Open("sqlite", "file:"+copy)
	require.NoError(t, err)
	defer copied.Close()
	var check string
	var version, runs int
	require.NoError(t, copied.QueryRow("PRAGMA integrity_check").Scan(&check))
	require.Equal(t, "ok", check)
	require.NoError(t, copied.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion-1, version, "the copy is the database before")
	require.NoError(t, copied.QueryRow("SELECT count(*) FROM runs").Scan(&runs))
	require.Equal(t, 1, runs)
	info, err := os.Stat(copy)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// A transaction whose context ends as it begins leaves no connection
// inside a transaction for the next to find: the driver reports the
// cancel even where BEGIN had taken effect, and the connection went back
// to the pool in the transaction, where the next BEGIN failed with
// "cannot start a transaction within a transaction" (CI's Intel run at
// 90de4fc2). Contexts canceled at every moment around the BEGIN, then
// transactions on every connection the pool holds, each of which must
// begin.
func TestACanceledBeginLeavesNoTransactionOpen(t *testing.T) {
	t.Parallel()
	f := open(t)
	for i := range 3000 {
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			for range i % 50 {
				runtime.Gosched()
			}
			cancel()
		}()
		_ = f.store.Update(ctx, f.repo, func(store.Tx) error { return nil })
		cancel()
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.store.View(t.Context(), f.repo, func(store.Reader) error {
				time.Sleep(20 * time.Millisecond)
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// A file that isn't a database, or a damaged one, is said with its path,
// that dockhand left it as it is, and what to do, never to delete it (the
// rc6 full stage, D-T5).
func TestADamagedDatabaseIsSaidPlainly(t *testing.T) {
	t.Parallel()
	for name, content := range map[string][]byte{
		"not a database": []byte(strings.Repeat("not a database at all\n", 400)),
		"truncated":      append([]byte("SQLite format 3\x00"), make([]byte, 84)...),
	} {
		path := filepath.Join(t.TempDir(), "dockhand.db")
		require.NoError(t, os.WriteFile(path, content, 0o600))
		_, err := Open(t.Context(), path, Options{})
		require.ErrorIs(t, err, store.ErrUnavailable, name)
		require.ErrorContains(t, err, path+" can't be read as dockhand's database", name)
		require.ErrorContains(t, err, "Dockhand left the file unchanged. Put back a copy dockhand backup made (dockhand usage recovery says how), or move this file aside to start a new database", name)
		require.NotContains(t, err.Error(), "delete")
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, content, after, "left as it was")
	}
}

// A damaged database names the newest copy a migration kept beside it as
// a candidate, with its date and what putting it back loses; a copy still
// being made, or a name that isn't a copy, is never named, and none is
// opened to find out (#214).
func TestADamagedDatabaseNamesTheCopyKeptBeforeAMigration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "dockhand.db")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("not a database at all\n", 400)), 0o600))
	older, newer := path+".schema-58", path+".schema-59"
	for i, copy := range []string{older, newer, path + ".schema-59" + partialCopy + "1", path + ".schema-x"} {
		require.NoError(t, os.WriteFile(copy, []byte("kept"), 0o600))
		at := time.Date(2026, 11, 1+i, 9, 30, 0, 0, time.Local)
		require.NoError(t, os.Chtimes(copy, at, at))
	}
	_, err := Open(t.Context(), path, Options{})
	require.ErrorIs(t, err, store.ErrUnavailable)
	require.ErrorContains(t, err, "Put back a copy dockhand backup made, or the copy kept before a migration, "+newer+", from 2026-11-02 09:30, which loses what was recorded since (dockhand usage recovery says how)")
	kept, err := os.ReadFile(newer)
	require.NoError(t, err)
	require.Equal(t, "kept", string(kept), "the candidate isn't opened")
}

// What holds the name of the copy a migration keeps has to be a usable
// copy, made now or found there: an empty file, a directory, a link that
// loops, and one that dangles each stood as the copy, and the migration
// went ahead with nothing to go back to. Each now refuses, names the path,
// leaves it as it is, and migrates nothing (#214).
func TestAMigrationRefusesACopyItCantGoBackTo(t *testing.T) {
	t.Parallel()
	for name, place := range map[string]func(t *testing.T, copy string){
		"an empty file": func(t *testing.T, copy string) { require.NoError(t, os.WriteFile(copy, nil, 0o600)) },
		"a directory":   func(t *testing.T, copy string) { require.NoError(t, os.Mkdir(copy, 0o700)) },
		"a looping link": func(t *testing.T, copy string) {
			require.NoError(t, os.Symlink(filepath.Base(copy), copy))
		},
		"a dangling link": func(t *testing.T, copy string) {
			require.NoError(t, os.Symlink(filepath.Join(filepath.Dir(copy), "nowhere"), copy))
		},
	} {
		path := schemaAt(t, schemaVersion-1, schema23Records)
		copy := path + fmt.Sprintf(".schema-%d", schemaVersion-1)
		place(t, copy)
		before, err := os.Lstat(copy)
		require.NoError(t, err)

		_, err = Open(t.Context(), path, Options{})
		require.ErrorIs(t, err, store.ErrUnavailable, name)
		require.ErrorContains(t, err, copy+", the copy of "+path+" kept before migrating it", name)
		require.ErrorContains(t, err, "isn't usable", name)
		require.ErrorContains(t, err, "nothing was migrated; move it aside and run the command again", name)
		after, err := os.Lstat(copy)
		require.NoError(t, err, name)
		require.Equal(t, before.Mode(), after.Mode(), "%s: left as it is", name)
		require.NoFileExists(t, filepath.Join(filepath.Dir(copy), "nowhere"), name)
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		version, _, err := schemaOf(t.Context(), db)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		require.Equal(t, schemaVersion-1, version, "%s: nothing migrated", name)
	}
}

// A good copy already in place is the migration's, as before: a database
// put back from it is migrated again, and the copy is reused (#214).
func TestACopyPutBackIsMigratedAgain(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, schemaVersion-1, schema23Records)
	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	copy := s.Migration().Copy
	kept, err := os.ReadFile(copy)
	require.NoError(t, err)

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
		}
	}
	require.NoError(t, os.WriteFile(path, kept, 0o600))
	again, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	require.NoError(t, again.Close())
	require.Equal(t, copy, again.Migration().Copy)
	still, err := os.ReadFile(copy)
	require.NoError(t, err)
	require.Equal(t, kept, still, "the copy in place is reused")
}

// A migration is refused while a serve or a foreground process that reads
// the older schema has a fresh heartbeat, naming them, since none of them
// could record its work after it; an observer, an ended process, or a
// heartbeat gone stale doesn't hold it, and a person's answer migrates
// anyway (the v0.3.1 port update, 2026-10-15).
func TestAMigrationWaitsForTheProcessesOnTheOlderSchema(t *testing.T) {
	t.Parallel()
	now := time.Now().UnixMilli()
	sessions := fmt.Sprintf(`
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_check','foreground',101,'p','v0.3.1',%[1]d,%[1]d,NULL);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_serve','serve',102,'p','v0.3.1',%[1]d,%[1]d,NULL);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_watch','observer',103,'p','v0.3.1',%[1]d,%[1]d,NULL);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_ended','foreground',104,'p','v0.3.0',%[1]d,%[1]d,%[1]d);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_stale','foreground',105,'p','v0.3.0',%[2]d,%[2]d,NULL);
INSERT INTO leases(repository_id, resource, holder, generation, acquired_at) VALUES('repo_1','run:run_1','ses_check',1,%[1]d);
INSERT INTO leases(repository_id, resource, holder, generation, acquired_at) VALUES('repo_1','leader','ses_serve',1,%[1]d);
`, now, now-int64(time.Hour/time.Millisecond))
	path := schemaAt(t, schemaVersion-1, schema23Records+sessions)
	// The engine gives coord's window; with none, the stale one counts too.
	_, err := Open(t.Context(), path, Options{})
	all := new(store.LiveUnderMigrationError)
	require.ErrorAs(t, err, &all)
	require.Equal(t, 3, all.Processes)
	_, err = Open(t.Context(), path, Options{LiveWithin: 2 * time.Minute})
	refused := new(store.LiveUnderMigrationError)
	require.ErrorAs(t, err, &refused)
	started := time.UnixMilli(now).UTC()
	require.Equal(t, &store.LiveUnderMigrationError{Processes: 2, Builds: []string{"v0.3.1"}, From: schemaVersion - 1, To: schemaVersion,
		Live: []store.LiveProcess{
			{Kind: "foreground", PID: 101, Build: "v0.3.1", Started: started, Checks: []int{1}},
			{Kind: "serve", PID: 102, Build: "v0.3.1", Started: started, Leads: true},
		}}, refused, "each named, with what it does, as status names it (the abook session)")
	require.ErrorContains(t, err, fmt.Sprintf("2 dockhand processes are using schema %d (v0.3.1); migrating this database to schema %d would prevent them from recording further work", schemaVersion-1, schemaVersion))

	s, err := Open(t.Context(), path, Options{MigrateUnderLive: true})
	require.NoError(t, err, "a person's answer migrates")
	require.Equal(t, schemaVersion, s.Migration().To)
	require.NoError(t, s.Close())
}

// A copy of a database carries its sessions, their heartbeats fresh, and
// the processes behind them beat in the original: field testing's .backup
// copy was refused for the serve and check working on the shared database
// (2026-10-29). Where a heartbeat is given, a migration counts only the
// processes whose heartbeat moves in this file while it waits three beats.
func TestAMigrationCountsOnlyTheProcessesWorkingOnThisFile(t *testing.T) {
	t.Parallel()
	now := time.Now().UnixMilli()
	sessions := fmt.Sprintf(`
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_here','foreground',101,'p','v0.3.1',%[1]d,%[1]d,NULL);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_original','serve',102,'p','v0.3.1',%[1]d,%[1]d,NULL);
INSERT INTO leases(repository_id, resource, holder, generation, acquired_at) VALUES('repo_1','leader','ses_original',1,%[1]d);
`, now)
	path := schemaAt(t, schemaVersion-1, schema23Records+sessions)
	options := Options{LiveWithin: 2 * time.Minute, Heartbeat: 50 * time.Millisecond}

	// One process beats in this file, as a check working on it would.
	beater, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	defer beater.Close()
	stop := make(chan struct{})
	beating := make(chan struct{})
	go func() {
		defer close(beating)
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
				_, _ = beater.Exec("UPDATE sessions SET heartbeat_at=? WHERE id='ses_here'", time.Now().UnixMilli())
			}
		}
	}()
	_, err = Open(t.Context(), path, options)
	close(stop)
	<-beating
	refused := new(store.LiveUnderMigrationError)
	require.ErrorAs(t, err, &refused)
	require.Equal(t, 1, refused.Processes, "the one beating here, not the serve beating in the original")
	require.Len(t, refused.Live, 1)
	require.Equal(t, 101, refused.Live[0].PID)

	// Neither beats here now: the copy migrates.
	s, err := Open(t.Context(), path, options)
	require.NoError(t, err)
	require.Equal(t, schemaVersion, s.Migration().To)
	require.NoError(t, s.Close())
}

// A check's records come in one read: the run, its plan, its executions,
// and each one's results, with each reused result's origin and the
// inputs its results name (the SQL study's O2).
func TestACheckIsReadWhole(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	earlier, run := f.run(t, b, r, p), f.run(t, b, r, p)
	built := model.GuestExecution{ID: "ex_built", Run: earlier.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	reusing := model.GuestExecution{ID: "ex_reusing", Run: run.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil, nil)
	var key string
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		var err error
		if key, err = tx.RecordInputs(inputs); err != nil {
			return err
		}
		if err := tx.AddExecution(built); err != nil {
			return err
		}
		if err := tx.AddExecution(reusing); err != nil {
			return err
		}
		if err := tx.RecordResult(model.TargetResult{Execution: built.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: key, RecordedAt: at}); err != nil {
			return err
		}
		return tx.RecordResult(model.TargetResult{Execution: reusing.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: key, ReusedFrom: built.ID, RecordedAt: at})
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		check, err := rd.Check(run.ID)
		require.NoError(t, err)
		require.Equal(t, run.ID, check.Run.ID)
		require.Equal(t, p.ID, check.Plan.ID)
		require.Len(t, check.Executions, 1)
		require.Len(t, check.Results[reusing.ID], 1)
		require.Equal(t, earlier.ID, check.Origins[built.ID].Run.ID)
		require.Equal(t, built.ID, check.Origins[built.ID].Execution.ID)
		require.Contains(t, check.Inputs, key)
		return nil
	}))
}

// Runs selects a tree's runs, and leaves baselines out where asked, in
// SQL: "runs of a tree" read every revision and run of the branch to join
// them in Go (SQL-O1).
func TestRunsOfATreeAndWithoutBaselines(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, r, p := f.seed(t)
	other := model.Revision{ID: "rev_2", Branch: b.ID, Kind: model.RevisionSnapshot, Snapshot: 2, Source: model.Source{Tree: "tree2", Base: "base"}, Head: "head", CreatedAt: at}
	otherPlan := p
	otherPlan.ID, otherPlan.Revision = "plan_2", other.ID
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddRevision(other); err != nil {
			return err
		}
		return tx.AddPlan(otherPlan)
	}))
	first := f.run(t, b, r, p)
	second := f.run(t, b, other, otherPlan)
	baseline := model.Run{ID: "run_3", Branch: b.ID, Revision: r.ID, Plan: p.ID, Number: 3, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at, BaselineOf: first.ID}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddRun(baseline) }))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		ofTree, err := rd.Runs(store.RunFilter{Branch: b.ID, Tree: "tree"})
		require.NoError(t, err)
		require.Equal(t, []model.RunID{baseline.ID, first.ID}, runIDs(ofTree))
		checks, err := rd.Runs(store.RunFilter{Branch: b.ID, Tree: "tree", NoBaselines: true})
		require.NoError(t, err)
		require.Equal(t, []model.RunID{first.ID}, runIDs(checks))
		latest, err := rd.Runs(store.RunFilter{Branch: b.ID, NoBaselines: true, Limit: 1})
		require.NoError(t, err)
		require.Equal(t, []model.RunID{second.ID}, runIDs(latest))
		return nil
	}))
}

func runIDs(runs []model.Run) []model.RunID {
	var ids []model.RunID
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}

// Forgetting an archived branch keeps of its checks only what another
// branch's result names, hides it from every list of branches, frees its
// name, and leaves no foreign key dangling (roadmap item 8).
func TestAForgottenBranchKeepsOnlyWhatOthersName(t *testing.T) {
	t.Parallel()
	f := open(t)
	a, r, p := f.seed(t)
	other := f.branch("br_2", "dockhand/harbor-2")
	otherRevision := model.Revision{ID: "rev_2", Branch: other.ID, Kind: model.RevisionSnapshot, Snapshot: 1, Source: model.Source{Tree: "tree2", Base: "base"}, Head: "head2", CreatedAt: at}
	otherPlan := p
	otherPlan.ID, otherPlan.Revision = "plan_2", otherRevision.ID
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		if err := tx.AddBranch(other); err != nil {
			return err
		}
		if err := tx.AddRevision(otherRevision); err != nil {
			return err
		}
		return tx.AddPlan(otherPlan)
	}))
	named, unnamed := f.run(t, a, r, p), f.run(t, a, r, p)
	reusing := f.run(t, other, otherRevision, otherPlan)
	built := model.GuestExecution{ID: "ex_built", Run: named.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	lone := model.GuestExecution{ID: "ex_lone", Run: unnamed.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	theirs := model.GuestExecution{ID: "ex_theirs", Run: reusing.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for _, execution := range []model.GuestExecution{built, lone, theirs} {
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
		}
		if err := tx.RecordResult(model.TargetResult{Execution: built.ID, Target: "libharbor", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, RecordedAt: at}); err != nil {
			return err
		}
		if err := tx.RecordResult(model.TargetResult{Execution: lone.ID, Target: "libharbor", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, RecordedAt: at}); err != nil {
			return err
		}
		return tx.RecordResult(model.TargetResult{Execution: theirs.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, ReusedFrom: built.ID, RecordedAt: at})
	}))
	a.State, a.EndedAt = model.BranchArchived, at
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(a) }))

	open := a
	open.ID = "br_3"
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.ForgetBranch(other.ID, at)
		require.ErrorIs(t, err, store.ErrConflict, "an open branch is not forgotten")
		pruned, err := tx.ForgetBranch(a.ID, at.Add(time.Hour))
		require.Equal(t, 1, pruned.Executions, "ex_lone; ex_built is another's result's origin")
		require.Equal(t, 1, pruned.Runs)
		return err
	}))
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		branches, err := rd.Branches(store.BranchFilter{})
		require.NoError(t, err)
		require.Len(t, branches, 1)
		require.Equal(t, other.ID, branches[0].ID)
		_, err = rd.BranchNamed(a.Name)
		require.ErrorIs(t, err, store.ErrNotFound)
		forgotten, err := rd.Branch(a.ID)
		require.NoError(t, err)
		require.Equal(t, at.Add(time.Hour), forgotten.ForgottenAt)
		check, err := rd.Check(reusing.ID)
		require.NoError(t, err)
		require.Equal(t, built.ID, check.Origins[built.ID].Execution.ID, "the origin another result names stays")
		return nil
	}))
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		_, err := tx.ForgetBranch(a.ID, at)
		require.ErrorIs(t, err, store.ErrConflict, "once")
		return tx.AddBranch(open)
	}), "its name is free")
	var violations int
	require.NoError(t, f.db(t).QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations))
	require.Zero(t, violations)
}

// A development or rc build doesn't migrate, unasked, a database a release
// last opened, since the release couldn't open it after: the exiv2
// session's read-only status on a main build locked the installed v0.4.0
// out (2026-10-24). Asked, it migrates; a release migrates it unasked, as
// the port's upgrade.
func TestADevelopmentBuildAsksBeforeMigratingAReleasesDatabase(t *testing.T) {
	t.Parallel()
	ended := "INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);\n" +
		"INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_1','foreground',101,'p','v0.4.0 (5897a828e1c2)',1,1,2);"
	path := schemaAt(t, 32, ended)
	_, err := Open(t.Context(), path, Options{Build: "v0.5.0-rc5 (6246e4b9eb84)"})
	refused := new(store.ReleaseUnderMigrationError)
	require.ErrorAs(t, err, &refused)
	require.Equal(t, store.ReleaseUnderMigrationError{Release: "v0.4.0", Copy: path + ".schema-32", From: 32, To: schemaVersion}, *refused)
	require.NoFileExists(t, path+".schema-32", "nothing kept or migrated before the person answers")

	asked, err := Open(t.Context(), path, Options{Build: "v0.5.0-rc5 (6246e4b9eb84)", MigrateRelease: true})
	require.NoError(t, err)
	require.Equal(t, &Migration{From: 32, To: schemaVersion, Copy: path + ".schema-32", Release: "v0.4.0"}, asked.Migration(), "the release passed over, for the engine to say")
	require.NoError(t, asked.Close())

	release := schemaAt(t, 32, ended)
	upgraded, err := Open(t.Context(), release, Options{Build: "v0.5.0"})
	require.NoError(t, err, "a release's migration is the port's upgrade")
	require.NoError(t, upgraded.Close())

	devel := schemaAt(t, 32, strings.Replace(ended, "v0.4.0 (5897a828e1c2)", "v0.4.1-0.20261024120000-0123456789ab", 1))
	opened, err := Open(t.Context(), devel, Options{Build: "devel (abcdef123456)"})
	require.NoError(t, err, "a database a development build last opened is no release's")
	require.NoError(t, opened.Close())
}

// A newer schema's refusal names the build that last opened the database,
// and when, the way to that build, and the copy kept at this build's
// schema: v0.4.0's "use a newer build" named neither (2026-10-24).
func TestANewerSchemasRefusalNamesItsBuildAndTheCopy(t *testing.T) {
	t.Parallel()
	f := open(t)
	require.NoError(t, f.store.Close())
	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES(?, 'ses_new', 'foreground', 101, 'p', 'v0.5.1-0.20261024120000-0123456789ab', 1792878000000, 1792878000000, 1792878000001)", string(f.repo))
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", schemaVersion+1))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, os.WriteFile(fmt.Sprintf("%s.schema-%d", f.path, schemaVersion), []byte("kept"), 0o600))
	_, err = Open(t.Context(), f.path, Options{})
	require.ErrorIs(t, err, store.ErrSchema)
	require.ErrorContains(t, err, "newer than this dockhand supports")
	require.ErrorContains(t, err, "dockhand v0.5.1-0.20261024120000-0123456789ab opened it last")
	require.ErrorContains(t, err, "it was built from commit 0123456789ab of "+buildinfo.ProjectURL)
	require.ErrorContains(t, err, fmt.Sprintf("A schema %d backup is kept at %s.schema-%d; restoring it loses changes recorded after that backup was made", schemaVersion, f.path, schemaVersion))
}

// A branch's publication is written only over the one its writer read, by
// ID: the first only where there is none, and no write of an old one over
// a successor's; UpdateBranch leaves it as it is. The pull request's
// provenance and its reviewer receipt are kept with it (#191).
func TestAPublicationIsWrittenOnlyOverTheOneItsWriterRead(t *testing.T) {
	t.Parallel()
	f := open(t)
	b, _, _ := f.seed(t)
	read := func() model.Branch {
		t.Helper()
		var got model.Branch
		require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
			var err error
			got, err = rd.Branch(b.ID)
			return err
		}))
		return got
	}
	first := &model.Publication{ID: "pub_1", Commit: "abc123", At: at, By: "alice", Head: "alice/macports-ports:jq", Base: "macports/macports-ports:master",
		Owes: []model.PublicationStep{model.StepPush, model.StepOpen}, Review: &model.ReviewAsk{Answer: model.ReviewYes, Reviewers: []model.Reviewer{{Login: "bob", Reviewed: at, Sent: model.MaySent}}}}
	set := func(was string, next *model.Publication) error {
		return f.update(t, func(tx store.Tx) error { return tx.SetPublication(b.ID, was, next) })
	}
	require.NoError(t, set("", first))
	require.Equal(t, first, read().Publication)
	require.ErrorIs(t, set("", &model.Publication{ID: "pub_2"}), store.ErrConflict, "a second first write finds one there")

	second := *first
	second.ID, second.Owes = "pub_2", []model.PublicationStep{model.StepOpen}
	require.NoError(t, set("pub_1", &second))
	require.ErrorIs(t, set("pub_1", nil), store.ErrConflict, "the old one's writer can't end its successor")

	got := read()
	got.Title = "jq: update to 1.8.1"
	got.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 7, Head: "alice/macports-ports:jq", Provenance: model.Opened,
		Rerequested: &model.ReviewReceipt{Head: "abc123", Reviewers: []model.Reviewer{{Login: "bob", Reviewed: at, Sent: model.SentRequest}}}}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(got) }))
	after := read()
	require.Equal(t, &second, after.Publication, "UpdateBranch leaves the publication")
	require.Equal(t, got.PullRequest, after.PullRequest)

	require.NoError(t, set("pub_2", nil))
	require.Nil(t, read().Publication)
}

// The publication record's migration waits for quiet: refused, with each
// process to stop and nothing migrated, while another dockhand runs on
// this Mac, and where the process list can't be read; it goes ahead with
// none, and a new database isn't asked (#191).
func TestThePublicationMigrationWaitsForQuiet(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		quiet func() ([]int, error)
		words string
	}{
		{"two running", func() ([]int, error) { return []int{101, 202}, nil }, "2 other dockhand processes are running on this Mac, and migrating this database from schema 60 to"},
		{"unreadable", func() ([]int, error) { return nil, errors.New("operation not permitted") }, "this Mac's process list can't be read here (operation not permitted)"},
	} {
		path := schemaAt(t, quietSchema-1, schema23Records)
		_, err := Open(t.Context(), path, Options{Quiet: c.quiet})
		var refused *store.QuietUnderMigrationError
		require.ErrorAs(t, err, &refused, c.name)
		require.ErrorContains(t, err, c.words, c.name)
		require.ErrorContains(t, err, "Nothing was migrated", c.name)
		if c.name == "two running" {
			require.ErrorContains(t, err, "stop each (kill 101; kill 202)")
		}
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		version, _, err := schemaOf(t.Context(), db)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		require.Equal(t, quietSchema-1, version, "%s: nothing migrated", c.name)
	}
	asked := false
	quiet := func() ([]int, error) { asked = true; return nil, nil }
	s, err := Open(t.Context(), schemaAt(t, quietSchema-1, schema23Records), Options{Quiet: quiet})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.True(t, asked)
	asked = false
	fresh, err := Open(t.Context(), filepath.Join(t.TempDir(), "dockhand.db"), Options{Quiet: quiet})
	require.NoError(t, err)
	require.NoError(t, fresh.Close())
	require.False(t, asked, "a new database has no older submission to wait for")
}

// An older submit's reviewer left-over is said once by the migration and
// never sent: a pull request whose reviewers requested changes, with a
// push recorded after that review and no request since, gets a receipt
// saying the reviewers may not have been asked; one asked since, or with
// no push after, gets none (#191).
func TestTheMigrationSaysAnOldReviewerLeftOver(t *testing.T) {
	t.Parallel()
	setup := schema23Records + `
INSERT INTO branches(repository_id, id, name, base, worktree, managed, title, state, created_at) VALUES('repo_1','br_2','dockhand/fd','base','/w2',1,'','open',1);
UPDATE branches SET pr_repository='macports/macports-ports', pr_number=CASE id WHEN 'br_1' THEN 7 ELSE 8 END, pr_head='ada/macports-ports:dockhand/jq-4k2p', pr_pushed='abc',
 pr_observed='{"State":"open","Review":"changes-requested","ChangesRequestedBy":["bob"],"ReviewedAt":"2026-11-10T12:00:00.5-05:00"}' WHERE id IN ('br_1','br_2');
INSERT INTO events(repository_id, at, session_id, branch_id, run_id, target_id, kind, level, message) VALUES
 ('repo_1', 1794336000000, '', 'br_1', '', '', 'branch.push', 'verbose', 'pushed abc to #7'),
 ('repo_1', 1794336000000, '', 'br_2', '', '', 'branch.push', 'verbose', 'pushed abc to #7'),
 ('repo_1', 1794336100000, '', 'br_2', '', '', 'branch.rerequest', 'info', 'asked @bob to review #7 again');`
	path := schemaAt(t, quietSchema-1, setup)
	s, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer s.Close()
	read := func(id model.BranchID) model.Branch {
		var got model.Branch
		require.NoError(t, s.View(t.Context(), "repo_1", func(r store.Reader) error {
			var err error
			got, err = r.Branch(id)
			return err
		}))
		return got
	}
	require.Equal(t, &model.ReviewReceipt{Head: "abc", Unasked: true}, read("br_1").PullRequest.Rerequested, "pushed after the review, never asked")
	require.Nil(t, read("br_2").PullRequest.Rerequested, "asked since")
}
