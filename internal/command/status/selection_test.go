package status

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
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
	ref := matrixRun(t, e)
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

// matrixRun queues a check, after the branch's latest, that ran on the
// command provider and on a GitHub workflow run two environments share,
// and is that run's URL.
func matrixRun(t *testing.T, e *engine.Engine) string {
	t.Helper()
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
	return ref
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

// A port named alone takes the flags a port takes: logs jq --phase was
// refused as naming no port, though logs jq worked, since the flags were
// judged before the lone argument was read (docs/designs/check-lifecycle.md,
// "Selection"). A bare number is a check, as check-N is, to each command
// that takes one.
func TestALonePortAndABareNumberAreResolvedOnce(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	_, _, named := cmdtest.Dockhand(t, "logs", "check-1", "jq", "--phase", "install")
	_, _, alone := cmdtest.Dockhand(t, "logs", "jq", "--phase", "install")
	require.EqualError(t, alone, "jq's build recorded no steps, so its install step can't be picked out of its log; logs without --phase prints it whole", "the lone name is the port, before --phase is judged")
	require.EqualError(t, alone, named.Error())
	_, _, err = cmdtest.Dockhand(t, "logs", "jq", "--phase", "configure")
	require.ErrorContains(t, err, "--phase takes one of the steps a build takes")
	_, _, err = cmdtest.Dockhand(t, "logs", "nosuch", "--phase", "install")
	require.ErrorContains(t, err, "nosuch is neither a check, such as check-42, nor a provider run")
	require.ErrorContains(t, err, "check-1 builds no port named nosuch; it builds jq")

	for _, verb := range [][]string{{"logs"}, {"artifacts"}, {"wait"}, {"logs", "%s", "jq"}} {
		args := func(name string) []string {
			if len(verb) == 1 {
				return []string{verb[0], name}
			}
			return []string{verb[0], name, verb[2]}
		}
		byName, _, err := cmdtest.Dockhand(t, args("check-1")...)
		require.NoError(t, err, "%v", verb)
		byNumber, _, err := cmdtest.Dockhand(t, args("1")...)
		require.NoError(t, err, "%v", verb)
		require.Equal(t, byName, byNumber, "%v: 1 is check-1", verb)
	}
	_, _, err = cmdtest.Dockhand(t, "artifacts", "9")
	require.ErrorContains(t, err, "there is no run 9", "a number is a check's, never a branch's")
}

// artifacts and shell serve a check; a provider's run, here a workflow run
// two environments share, is refused by name with the check to name
// instead, as one attempt is (TestQueueWaitCancelAndLogs).
func TestAProviderRunIsRefusedWhereOnlyACheckIsServed(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	e, err := cmdtest.TestSettings(t).Open(t.Context())
	require.NoError(t, err)
	ref := matrixRun(t, e)
	e.Close()

	_, _, err = cmdtest.Dockhand(t, "artifacts", ref)
	require.EqualError(t, err, "artifacts lists a check's passed builds; "+ref+" is one provider run of check-2. Run dockhand artifacts check-2")
	_, _, err = cmdtest.Dockhand(t, "shell", ref, "--", "true")
	require.EqualError(t, err, "shell opens a check's kept clone; "+ref+" is one provider run of check-2. Run dockhand shell check-2, with --on for the release")
}

// keptOnTwoReleases is a provider that kept a clone of a check on sequoia
// and one on tahoe, each of which takes a shell that exits 7.
type keptOnTwoReleases struct {
	buildenv.Provider
	shells *[]buildenv.ShellJob
}

func (keptOnTwoReleases) Leftovers(context.Context) ([]buildenv.Leftover, error) {
	at := time.Now()
	return []buildenv.Leftover{{Ref: "clone-sequoia", What: "clone clone-sequoia", KeptAt: at}, {Ref: "clone-tahoe", What: "clone clone-tahoe", KeptAt: at}}, nil
}

func (keptOnTwoReleases) RemoveLeftover(context.Context, string) error { return nil }

func (p keptOnTwoReleases) Shell(_ context.Context, job buildenv.ShellJob) (int, error) {
	*p.shells = append(*p.shells, job)
	return 7, nil
}

// shell, the command, chooses among the clones a check kept by --on: with
// several and no --on it refuses, naming each release; an --on the check
// kept no clone on is refused by name; the one chosen runs the command,
// whose exit status is the command's, the check named by its number too.
func TestShellChoosesAmongTheClonesACheckKept(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	e, err := cmdtest.TestSettings(t).Open(t.Context())
	require.NoError(t, err)
	// check-2 ran on sequoia and on tahoe, an attempt on each, and kept
	// both clones.
	checked, err := e.RunNamed(t.Context(), "check-1")
	require.NoError(t, err)
	branch, err := e.Branch(t.Context(), checked.Branch)
	require.NoError(t, err)
	plan, err := e.Plan(t.Context(), checked.Plan)
	require.NoError(t, err)
	plan.ID = model.PlanID(store.NewID("plan"))
	builds := plan.Builds
	plan.Environments, plan.Builds = nil, nil
	for _, version := range []string{"24", "25"} {
		environment := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}}
		plan.Environments = append(plan.Environments, environment)
		build := builds[0]
		build.Environment = environment
		plan.Builds = append(plan.Builds, build)
	}
	run, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	ended := time.Now()
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		run.State = model.RunRunning
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		for i, version := range []string{"24", "25"} {
			ref := []string{"clone-sequoia", "clone-tahoe"}[i]
			environment := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}}
			if err := tx.AddExecution(model.GuestExecution{ID: model.ExecutionID("command_kept" + version), Run: run.ID, Environment: environment, Attempt: 1, State: model.ExecutionFinished, ProviderRef: ref, CreatedAt: run.CreatedAt, FinishedAt: &ended}); err != nil {
				return err
			}
		}
		run.State, run.FinishedAt = model.RunPassed, &ended
		return tx.UpdateRun(run)
	}))
	e.Close()
	var shells []buildenv.ShellJob
	original := cmdtest.Stands(t).Forge
	cmdtest.Stands(t).Forge = func(e *engine.Engine) engine.Forge {
		e.Providers["command"] = keptOnTwoReleases{e.Providers["command"], &shells}
		return original(e)
	}

	_, _, err = cmdtest.Dockhand(t, "shell", "check-2", "--", "ls")
	require.EqualError(t, err, "check-2 kept clones for several releases; choose one with --on sequoia or --on tahoe")
	_, _, err = cmdtest.Dockhand(t, "shell", "check-2", "--on", "sonoma", "--", "ls")
	require.EqualError(t, err, "check-2 kept no clone on sonoma")
	require.Empty(t, shells, "nothing was started")
	_, _, err = cmdtest.Dockhand(t, "shell", "check-2", "--on", "tahoe", "--", "ls")
	require.Equal(t, 7, term.ExitCode(err), "the command's exit status")
	require.Len(t, shells, 1)
	require.Equal(t, "clone-tahoe", shells[0].Ref)
	require.Equal(t, []string{"ls"}, shells[0].Command)
	_, _, err = cmdtest.Dockhand(t, "shell", "2", "--on", "sequoia", "--", "ls")
	require.Equal(t, 7, term.ExitCode(err))
	require.Len(t, shells, 2)
	require.Equal(t, "clone-sequoia", shells[1].Ref, "a bare number is the check")
}

// A check cleanup removed is said to be gone, to every command that names
// one, by its name or its number, rather than never there; an attempt of
// it, named by its ID, is no longer found (D6).
func TestAPrunedCheckIsSaidToBeGone(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	for range 2 {
		_, _, err := cmdtest.Dockhand(t, "check", "--fresh")
		require.NoError(t, err)
	}
	out, _, err := cmdtest.Dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	attempt := regexp.MustCompile(`command_[a-z0-9]{16}`).FindString(out)
	require.NotEmpty(t, attempt)

	ctx := t.Context()
	e, err := cmdtest.TestSettings(t).Open(ctx)
	require.NoError(t, err)
	run, err := e.RunNamed(ctx, "check-1")
	require.NoError(t, err)
	branch, err := e.Branch(ctx, run.Branch)
	require.NoError(t, err)
	gone := time.Now().Add(-20 * 24 * time.Hour)
	branch.State, branch.EndedAt = model.BranchArchived, gone
	require.NoError(t, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	session, err := e.Session(ctx, model.SessionForeground)
	require.NoError(t, err)
	report, err := e.Cleanup(ctx, session, 15*24*time.Hour)
	require.NoError(t, err)
	require.NoError(t, session.End(context.WithoutCancel(ctx)))
	e.Close()
	require.Equal(t, 1, report.History.Runs, "check-1 went; check-2, the newest, stays")

	said := "check-1 is no longer recorded: cleanup removes what a branch's checks recorded once it has been merged, closed, or archived for cleanup.after, keeping its newest check"
	for _, args := range [][]string{{"logs", "check-1"}, {"logs", "1"}, {"artifacts", "check-1"}, {"wait", "check-1"}, {"shell", "check-1", "--", "ls"}, {"logs", "check-1", "jq"}} {
		_, _, err := cmdtest.Dockhand(t, args...)
		require.EqualError(t, err, said, "%v", args)
	}
	_, _, err = cmdtest.Dockhand(t, "logs", attempt)
	require.ErrorContains(t, err, attempt+" is neither a check, such as check-42, nor a provider run")
	_, _, err = cmdtest.Dockhand(t, "logs", "check-9")
	require.ErrorContains(t, err, "there is no run check-9", "never there")
	_, _, err = cmdtest.Dockhand(t, "logs", "check-2")
	require.NoError(t, err)
}
