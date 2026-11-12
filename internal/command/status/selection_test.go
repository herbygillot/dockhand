package status

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// unreadIdentity is a provider whose environment's identity can't be read.
type unreadIdentity struct{ buildenv.Provider }

func (unreadIdentity) Identity(context.Context, model.Environment) (string, error) {
	return "", errors.New("the image's record couldn't be read")
}

// unreadAfterBuild is a provider whose identity reads before its build and
// can't after it.
type unreadAfterBuild struct {
	buildenv.Provider
	built *bool
}

func (p unreadAfterBuild) Identity(context.Context, model.Environment) (string, error) {
	if *p.built {
		return "", errors.New("the image's record couldn't be read")
	}
	return "", nil
}

func (p unreadAfterBuild) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	err := p.Provider.Execute(ctx, job, build)
	*p.built = true
	return err
}

// check --json says an environment whose identity couldn't be read once its
// build was done, as its report reads the identities now (Codex's review of
// #193, finding 2).
func TestCheckSaysAnIdentityThatCouldntBeRead(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	original := cmdtest.Stands(t).Forge
	built := false
	cmdtest.Stands(t).Forge = func(e *engine.Engine) engine.Forge {
		for name, provider := range e.Providers {
			e.Providers[name] = unreadAfterBuild{provider, &built}
		}
		return original(e)
	}
	out, _, err := cmdtest.Dockhand(t, "check", "--json")
	require.NoError(t, err)
	require.True(t, built)
	require.Contains(t, out, `"identities_unread"`)
	require.Contains(t, out, "the image's record couldn't be read")
}

// status --json, wait --json and wait say an environment whose identity
// status could read and couldn't, as text status does (Codex's review of
// #193, findings 2 and 3).
func TestEveryReportSaysAnIdentityThatCouldntBeRead(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	original := cmdtest.Stands(t).Forge
	cmdtest.Stands(t).Forge = func(e *engine.Engine) engine.Forge {
		for name, provider := range e.Providers {
			e.Providers[name] = unreadIdentity{provider}
		}
		return original(e)
	}
	text, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, text, "the image's record couldn't be read")
	for _, args := range [][]string{{"status", "--json"}, {"wait", "check-1", "--json"}, {"wait", "check-1"}} {
		out, _, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err)
		require.Contains(t, out, "the image's record couldn't be read", "%v", args)
	}
}

// A provider run several environments share stands for those attempts and
// no others: logs of its URL lists them, and a port's log, --full, is one
// of theirs, never another provider's attempt in the same check (Codex's
// review of #193, finding 1).
func TestAProviderRunSelectsItsAttemptsAlone(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	e, err := cmdtest.TestSettings(t).Open(t.Context())
	require.NoError(t, err)
	defer e.Close()
	runs, err := e.Runs(t.Context(), store.RunFilter{Limit: 1})
	require.NoError(t, err)
	run := runs[0]
	ref := "https://github.com/ada/macports-ports/actions/runs/193"
	envs := []model.Environment{
		{Provider: "github", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}},
		{Provider: "github", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}},
	}
	ended := time.Now()
	logDir := filepath.Join(e.LogDirectory(), "selection")
	testsupport.WriteFiles(t, logDir, map[string]string{"command.log": "UNSELECTED COMMAND OUTPUT\n", "a.log": "SELECTED MATRIX A\n", "b.log": "SELECTED MATRIX B\n"})
	plan, err := e.Plan(t.Context(), run.Plan)
	require.NoError(t, err)
	plan.ID = model.PlanID(store.NewID("plan"))
	for _, env := range envs {
		plan.Environments = append(plan.Environments, env)
		build := plan.Builds[0]
		build.Environment = env
		plan.Builds = append(plan.Builds, build)
	}
	branch, err := e.Branch(t.Context(), run.Branch)
	require.NoError(t, err)
	run, err = e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		run.State = model.RunRunning
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		if err := tx.AddExecution(model.GuestExecution{ID: "command_selection", Run: run.ID, Environment: model.Environment{Provider: "command"}, Attempt: 1, State: model.ExecutionFinished, CreatedAt: run.CreatedAt, FinishedAt: &ended}); err != nil {
			return err
		}
		if err := tx.RecordResult(model.TargetResult{Execution: "command_selection", Target: "jq", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Log: filepath.Join(logDir, "command.log"), RecordedAt: ended}); err != nil {
			return err
		}
		for i, id := range []model.ExecutionID{"github_selectiona", "github_selectionb"} {
			if err := tx.AddExecution(model.GuestExecution{ID: id, Run: run.ID, Environment: envs[i], Attempt: 1, State: model.ExecutionFinished, ProviderRef: ref, CreatedAt: run.CreatedAt, FinishedAt: &ended}); err != nil {
				return err
			}
			log := "a.log"
			if i == 1 {
				log = "b.log"
			}
			if err := tx.RecordResult(model.TargetResult{Execution: id, Target: "jq", Outcome: model.OutcomePassed, Log: filepath.Join(logDir, log), RecordedAt: ended}); err != nil {
				return err
			}
		}
		return nil
	}))
	selected, err := e.SelectBuild(t.Context(), engine.SelectRequest{Name: ref})
	require.NoError(t, err)
	require.Equal(t, engine.SelectedProviderRun, selected.Kind)
	require.Len(t, selected.Executions, 2)
	out, _, err := cmdtest.Dockhand(t, "logs", ref)
	require.NoError(t, err)
	require.NotContains(t, out, "run command_", "the URL selects no other provider's attempt")
	out, _, err = cmdtest.Dockhand(t, "logs", ref, "jq", "--full")
	require.NoError(t, err)
	require.NotContains(t, out, "UNSELECTED COMMAND OUTPUT")
	require.Contains(t, out, "SELECTED MATRIX")
}

// A port the check doesn't build is refused by name, on a running check
// too, where it was said to have recorded no log yet (Codex's review of
// #193, finding 4).
func TestAPortTheCheckDoesntBuildIsRefusedByName(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	_, _, err := cmdtest.Dockhand(t, "start", "selection")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "selection")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	cmdtest.WithScript(t, w, "passed")
	testsupport.Git(t, dir, "sparse-checkout", "add", "textproc/jq")
	testsupport.WriteFiles(t, dir, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	e, err := cmdtest.TestSettings(t).Open(t.Context())
	require.NoError(t, err)
	defer e.Close()
	runs, err := e.Runs(t.Context(), store.RunFilter{Limit: 1})
	require.NoError(t, err)
	run := runs[0]
	run.State = model.RunRunning
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateRun(run) }))
	leader, err := e.Session(t.Context(), model.SessionForeground)
	require.NoError(t, err)
	defer leader.End(context.Background())
	_, err = leader.Acquire(t.Context(), checks.RunResource(run.ID))
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "logs", run.Name(), "never-built-port")
	require.EqualError(t, err, run.Name()+" builds no port named never-built-port; it builds jq")

	// A port named alone, in the branch's worktree, is resolved by the
	// same one selection: refused by the check it means, where it was
	// said to be neither a check nor a provider run, to be named
	// "outside its branch's worktree" (the confirmation of #193's fixes,
	// A4); and a port the running check builds and hasn't logged yet is
	// that check's, where it was refused the same way.
	_, _, err = cmdtest.Dockhand(t, "logs", "never-built-port")
	require.EqualError(t, err, "never-built-port is neither a check, such as check-42, nor a provider run, such as tart_7y62p4sigena6xlr or a workflow run's URL, and "+run.Name()+" builds no port named never-built-port; it builds jq")
	_, _, err = cmdtest.Dockhand(t, "logs", "jq")
	require.EqualError(t, err, run.Name()+" is running and hasn't recorded a log for jq yet; dockhand wait "+run.Name()+" follows it")
}
