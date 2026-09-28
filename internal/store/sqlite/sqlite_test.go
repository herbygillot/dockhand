package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
			{ID: "libharbor", Target: model.Target{Name: "libharbor", Variants: map[string]bool{"docs": false}}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed},
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

// A plan recorded before each environment had its own reads as one: each
// environment's plan is what the old form said about it.
func TestAPlanRecordedBeforeEnvironmentPlansReadsAsOne(t *testing.T) {
	f := open(t)
	_, r, _ := f.seed(t)
	intel := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	arm, _ := json.Marshal(tahoe)
	x86, _ := json.Marshal(intel)
	// Two environments: libharbor needs Xcode on x86_64, which has none,
	// harbor-viewer links libharbor only there, and harbor-cli is arm64's
	// alone.
	body := `{"ID": "plan_old", "Revision": "` + string(r.ID) + `", "Environments": [` + string(arm) + `, ` + string(x86) + `],
		"Targets": [
			{"ID": "libharbor", "Target": {"Name": "libharbor"}, "Directory": "devel/libharbor", "Kind": "substantive", "Role": "changed", "NeedsXcode": [` + string(x86) + `]},
			{"ID": "harbor-cli", "Target": {"Name": "harbor-cli"}, "Directory": "devel/harbor-cli", "Kind": "substantive", "Role": "changed"},
			{"ID": "harbor-viewer", "Target": {"Name": "harbor-viewer"}, "Directory": "graphics/harbor-viewer", "Kind": "substantive", "Role": "changed", "DependsOn": ["libharbor"]}],
		"Dependencies": [{}, {"harbor-viewer": ["libharbor"]}],
		"Exclusions": [{"Target": {"Name": "harbor-cli"}, "Platform": {"OS": "darwin", "Version": "25", "Architecture": "x86_64"}, "Reason": "supported_archs arm64 only"}],
		"Unmet": [{"Target": "libharbor", "Environment": ` + string(x86) + `, "Needs": "Xcode"},
			{"Target": "harbor-viewer", "Environment": ` + string(x86) + `, "Needs": "Xcode", "Through": "libharbor"}],
		"Tests": "declared", "CreatedAt": "2026-09-25T12:00:00Z"}`
	db, err := sql.Open("sqlite", f.path)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO plans(repository_id, id, revision_id, body, created_at) VALUES(?,?,?,?,?)", f.repo, "plan_old", r.ID, body, millis(at))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		got, err := rd.Plan("plan_old")
		require.NoError(t, err)
		require.NoError(t, got.Validate())
		require.Equal(t, []model.EnvironmentPlan{
			{Environment: tahoe, Order: []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}},
			{Environment: intel, Order: []model.TargetID{"libharbor", "harbor-viewer"},
				Dependencies: map[model.TargetID][]model.TargetID{"harbor-viewer": {"libharbor"}},
				NeedsXcode:   []model.TargetID{"libharbor"},
				Unmet: []model.Unmet{{Target: "libharbor", Environment: intel, Needs: model.RequiresXcode},
					{Target: "harbor-viewer", Environment: intel, Needs: model.RequiresXcode, Through: "libharbor"}},
				Exclusions: []model.Exclusion{{Target: model.Target{Name: "harbor-cli"}, Reason: "supported_archs arm64 only"}}},
		}, got.Builds)
		return nil
	}))

	// One environment, before its dependencies were kept apart: a
	// target's dependencies were its environment's.
	single := `{"ID": "plan_one", "Revision": "` + string(r.ID) + `", "Environments": [` + string(arm) + `],
		"Targets": [
			{"ID": "libharbor", "Target": {"Name": "libharbor"}, "Directory": "devel/libharbor", "Kind": "substantive", "Role": "changed"},
			{"ID": "harbor-cli", "Target": {"Name": "harbor-cli"}, "Directory": "devel/harbor-cli", "Kind": "substantive", "Role": "changed", "DependsOn": ["libharbor"]}],
		"Tests": "declared", "CreatedAt": "2026-09-25T12:00:00Z"}`
	got, err := decodePlan([]byte(single))
	require.NoError(t, err)
	require.Equal(t, []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"libharbor", "harbor-cli"},
		Dependencies: map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}}}}, got.Builds)
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
		executions, err := rd.Executions(run.ID)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2}, []int{executions[0].Attempt, executions[1].Attempt})
		return nil
	}))
}

// A result names what its build read by key, and the record is kept once
// for every build that read the same (decision 28).
func TestAResultKeepsWhatItsBuildRead(t *testing.T) {
	f := open(t)
	b, r, p := f.seed(t)
	run := f.run(t, b, r, p)
	execution := model.GuestExecution{ID: "ex_1", Run: run.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.AddExecution(execution) }))
	inputs := model.NewTargetInputs("source sha256:a; setup 2", "devel/libharbor", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", nil,
		[]model.ActivePort{{Name: "zlib", Spec: "@1.3.2_0", Directory: "archivers/zlib", Tree: "3333333333333333333333333333333333333333", Archive: "sha256:44"}})

	missing := model.TargetResult{Execution: execution.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: inputs.Key(), RecordedAt: at}
	require.ErrorIs(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(missing) }), store.ErrNotFound, "inputs are recorded before the result naming them")

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
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.RecordResult(built) }))

	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		results, err := rd.Results(execution.ID)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "sha256:55", results[0].Archive)
		require.Equal(t, "a dependency failed to install: zlib", results[0].Detail)
		require.Equal(t, built.Builders, results[0].Builders)
		read, err := rd.Inputs(results[0].Inputs)
		require.NoError(t, err)
		require.Equal(t, inputs, read)
		_, err = rd.Inputs("sha256:none")
		require.ErrorIs(t, err, store.ErrNotFound)
		return nil
	}))
}

// A kept archive is recorded once by its digest, with the file name
// MacPorts gives it; one that can't be named as a file, or has no digest,
// isn't recorded (decision 28).
func TestAnArchiveIsRecordedOnceByDigest(t *testing.T) {
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

// An archive is forgotten once no live result names it: none of an open
// branch's checks, and none recorded since the cutoff. One kept since the
// cutoff stays however it is named (decisions 36 and 44).
func TestArchivesGoWhenNoLiveResultNamesThem(t *testing.T) {
	f := open(t)
	b, r, p := f.seed(t)
	run := f.run(t, b, r, p)
	later := at.Add(2 * time.Hour)
	require.NoError(t, f.update(t, func(tx store.Tx) error {
		for i, recorded := range []time.Time{at, later} {
			execution := model.GuestExecution{ID: model.ExecutionID(fmt.Sprintf("ex_%d", i+1)), Run: run.ID, Environment: tahoe, Attempt: i + 1, State: model.ExecutionWaiting, CreatedAt: at}
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			target, archive := model.TargetID("libharbor"), "sha256:aa"
			if i == 1 {
				target, archive = "harbor-cli", "sha256:cc"
			}
			if err := tx.RecordResult(model.TargetResult{Execution: execution.ID, Target: target, Outcome: model.OutcomePassed, Tests: model.TestsNone, Archive: archive, RecordedAt: recorded}); err != nil {
				return err
			}
		}
		for _, archive := range []model.Archive{
			{Digest: "sha256:aa", Name: "libharbor.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:bb", Name: "unnamed.tbz2", Size: 1, KeptAt: at},
			// Kept long ago, and named by a result recorded since, as a
			// later check's reuse of the build names its archive.
			{Digest: "sha256:cc", Name: "harbor-cli.tbz2", Size: 1, KeptAt: at},
			{Digest: "sha256:dd", Name: "fresh.tbz2", Size: 1, KeptAt: later},
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
			pruned, err := tx.PruneArchives(at.Add(time.Hour))
			for _, archive := range pruned {
				digests = append(digests, archive.Digest)
			}
			return err
		}))
		return digests
	}
	require.Equal(t, []string{"sha256:bb"}, prune(), "no result names it; an open branch's are kept")
	b.State = model.BranchMerged
	require.NoError(t, f.update(t, func(tx store.Tx) error { return tx.UpdateBranch(b) }))
	require.Equal(t, []string{"sha256:aa"}, prune(), "a retired branch's goes once its results are older than the cutoff, and one a recent result names stays")
	require.Empty(t, prune())
	require.NoError(t, f.store.View(t.Context(), f.repo, func(rd store.Reader) error {
		kept, err := rd.Archives()
		require.NoError(t, err)
		require.Len(t, kept, 2)
		require.Equal(t, []string{"sha256:cc", "sha256:dd"}, []string{kept[0].Digest, kept[1].Digest})
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
		require.Equal(t, "target.result", later[0].Kind)
		last, err := r.LastEvent()
		require.NoError(t, err)
		require.Greater(t, last, sequences[2])
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
