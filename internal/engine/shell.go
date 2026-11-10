package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// ShellRequest is a command in a clone a check kept (model.Plan.Keep): the
// check's, on Release where it kept several, in Port's work directory,
// the first port it changed where Port is empty. Command is what to run, a
// shell where it's empty, with Input and Output its streams, on a terminal
// where Terminal.
type ShellRequest struct {
	Run     model.Run
	Release string
	Port    string
	Command []string
	// Test copies the port's directory, as it is in the branch's worktree
	// now, over the one the check staged, and runs its test phase alone
	// again, on the build as it stands: never a result or evidence.
	Test     bool
	Input    io.Reader
	Output   io.Writer
	Terminal bool
	// Notice hears, before a shell opens in a running check's guest, what
	// it leaves as no evidence.
	Notice func(string)
}

// Shell runs a command in a clone a check kept, and returns its exit
// status: the clone is started for it and stopped after, and stays kept.
func (e *Engine) Shell(ctx context.Context, request ShellRequest) (int, error) {
	if !request.Run.State.Terminal() {
		return e.shellRunning(ctx, request)
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return -1, err
	}
	defer session.End(context.WithoutCancel(ctx))
	leftovers, err := e.PlanLeftovers(ctx, session)
	if err != nil {
		return -1, err
	}
	var kept []Leftover
	for _, leftover := range leftovers {
		if leftover.KeptAt.IsZero() || leftover.Run == nil || leftover.Run.ID != request.Run.ID || leftover.Execution == nil {
			continue
		}
		if request.Release != "" {
			release, err := macos.ParseRelease(request.Release)
			if err != nil {
				return -1, err
			}
			if leftover.Execution.Environment.Platform.Version != strconv.Itoa(release.Darwin) {
				continue
			}
		}
		kept = append(kept, leftover)
	}
	name := request.Run.Name()
	switch {
	case len(kept) == 0 && request.Release != "":
		return -1, fmt.Errorf("%s kept no clone on %s", name, request.Release)
	case len(kept) == 0:
		return -1, fmt.Errorf("%s kept no clone; dockhand check --keep retains each Tart environment's clone", name)
	case len(kept) > 1:
		var releases []string
		for _, leftover := range kept {
			darwin, _ := strconv.Atoi(leftover.Execution.Environment.Platform.Version)
			if release, err := macos.ReleaseForDarwin(darwin); err == nil {
				releases = append(releases, "--on "+release.Slug)
			}
		}
		return -1, fmt.Errorf("%s kept clones for several releases; choose one with %s", name, strings.Join(releases, " or "))
	}
	clone := kept[0]
	provider, ok := e.Providers[clone.Provider].(buildenv.ShellProvider)
	if !ok {
		return -1, fmt.Errorf("%s can't be started for a shell", clone.What)
	}
	chosen, err := e.shellTarget(ctx, request)
	if err != nil {
		return -1, err
	}
	job := buildenv.ShellJob{Ref: clone.Ref, Portfile: chosen.Target.Portfile, Command: request.Command, Input: request.Input, Output: request.Output, Terminal: request.Terminal}
	return e.shellTest(ctx, request, provider, job, chosen)
}

// shellTarget is the target a shell starts in: the port named, else the
// first the check changed.
func (e *Engine) shellTarget(ctx context.Context, request ShellRequest) (*model.PlanTarget, error) {
	plan, err := e.Plan(ctx, request.Run.Plan)
	if err != nil {
		return nil, err
	}
	for i, target := range plan.Targets {
		named := request.Port == target.Target.Name || (target.Target.Subport != "" && request.Port == target.Target.Subport)
		if named || (request.Port == "" && target.Role == model.Changed) {
			return &plan.Targets[i], nil
		}
	}
	return nil, fmt.Errorf("%s didn't build %s", request.Run.Name(), request.Port)
}

// shellRunning opens a shell, or runs the command, in a running check's
// guest, on Release where it builds on several: the guest the run's live
// attempt runs in, reached as the check reaches it and left running, the
// check owning it. The entry is recorded first, so a result the shell
// could have touched, one recorded after it, isn't evidence
// (evidence.CellEntered).
func (e *Engine) shellRunning(ctx context.Context, request ShellRequest) (int, error) {
	name := request.Run.Name()
	if request.Test {
		return -1, fmt.Errorf("--test reruns the test phase in a kept clone; %s is running", name)
	}
	var live []model.GuestExecution
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		executions, err := r.Executions(request.Run.ID)
		for _, execution := range executions {
			if execution.State == model.ExecutionRunning && execution.ProviderRef != "" {
				live = append(live, execution)
			}
		}
		return err
	})
	if err != nil {
		return -1, err
	}
	if request.Release != "" {
		release, err := macos.ParseRelease(request.Release)
		if err != nil {
			return -1, err
		}
		live = slices.DeleteFunc(live, func(execution model.GuestExecution) bool {
			return execution.Environment.Platform.Version != strconv.Itoa(release.Darwin)
		})
	}
	switch {
	case len(live) == 0:
		return -1, fmt.Errorf("%s is %s and has no running guest available for a shell yet", name, request.Run.State)
	case len(live) > 1:
		var releases []string
		for _, execution := range live {
			darwin, _ := strconv.Atoi(execution.Environment.Platform.Version)
			if release, err := macos.ReleaseForDarwin(darwin); err == nil {
				releases = append(releases, "--on "+release.Slug)
			}
		}
		return -1, fmt.Errorf("%s is building on several releases; choose one with %s", name, strings.Join(releases, " or "))
	}
	execution := live[0]
	provider, ok := e.Providers[execution.Environment.Provider].(buildenv.ShellProvider)
	if !ok {
		return -1, fmt.Errorf("%s's guest on %s can't be entered", name, execution.Environment.Provider)
	}
	chosen, err := e.shellTarget(ctx, request)
	if err != nil {
		return -1, err
	}
	if request.Notice != nil {
		request.Notice(fmt.Sprintf("%s is building in %s; entering a shell makes this guest's current and subsequent builds unusable as submission evidence. Run dockhand check again to rebuild them.", name, execution.ProviderRef))
	}
	entry := model.Entry{At: e.now(), Environment: execution.Environment, Command: strings.Join(request.Command, " ")}
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.EnterRun(request.Run.ID, entry) }); err != nil {
		return -1, err
	}
	return provider.Shell(ctx, buildenv.ShellJob{Ref: execution.ProviderRef, Portfile: chosen.Target.Portfile, Command: request.Command, Running: true, Environment: execution.Environment,
		Input: request.Input, Output: request.Output, Terminal: request.Terminal})
}

// shellTest runs a kept clone's shell, with --test's port taken from the
// branch's worktree.
func (e *Engine) shellTest(ctx context.Context, request ShellRequest, provider buildenv.ShellProvider, job buildenv.ShellJob, chosen *model.PlanTarget) (int, error) {
	if request.Test {
		branch, err := e.Branch(ctx, request.Run.Branch)
		if err != nil {
			return -1, err
		}
		if branch.Worktree == "" {
			return -1, fmt.Errorf("%s has no worktree to take the port from", branch.ShortName())
		}
		job.Test, job.Variants = filepath.Join(branch.Worktree, filepath.Dir(chosen.Target.Portfile)), chosen.Target.VariantSpec()
	}
	return provider.Shell(ctx, job)
}

// ShellSteps are the dockhand shell commands that open the clones a check
// kept: the check's name, with --on and the release where it kept
// several; none where it kept none.
func (e *Engine) ShellSteps(ctx context.Context, run model.Run) ([]refusal.Next, error) {
	kept, err := e.KeptClones(ctx)
	if err != nil {
		return nil, err
	}
	var releases []string
	for _, leftover := range kept {
		if leftover.Run.ID != run.ID {
			continue
		}
		darwin, _ := strconv.Atoi(leftover.Execution.Environment.Platform.Version)
		slug := leftover.Execution.Environment.Platform.Version
		if release, err := macos.ReleaseForDarwin(darwin); err == nil {
			slug = release.Slug
		}
		releases = append(releases, slug)
	}
	if len(releases) == 1 {
		return []refusal.Next{{Verb: refusal.VerbShell, Args: []string{run.Name()}}}, nil
	}
	var steps []refusal.Next
	for _, release := range releases {
		steps = append(steps, refusal.Next{Verb: refusal.VerbShell, Args: []string{run.Name(), "--on", release}})
	}
	return steps, nil
}
