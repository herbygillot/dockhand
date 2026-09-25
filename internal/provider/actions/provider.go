// Package actions is the github provider (Design v3 §7): it builds a
// revision with MacPorts' own workflow, in your fork. It pushes the
// revision's commit to a branch of your fork, where the workflow runs on
// push as it does for every branch but master, waits for that run, and
// reads each runner's log for what it says about each port. The workflow
// builds the ports the commit changes, on each macOS its matrix names;
// dockhand does not choose the runners, and ports it does not change are
// not built.
package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Workflow is MacPorts' workflow, the one that builds a branch's changed
// ports on push.
const Workflow = "main.yml"

// BranchPrefix names the branches the provider pushes to your fork.
const BranchPrefix = "dockhand-check/"

// Run is one run of the workflow, at its latest attempt.
type Run struct {
	ID      int64
	Attempt int
	// Status is queued, in_progress, or completed; Conclusion, once
	// completed, is success, failure, cancelled, and so on.
	Status, Conclusion string
	URL                string
}

// RunnerJob is one job of a run: one runner of the workflow's matrix.
type RunnerJob struct {
	ID                 int64
	Name               string
	Status, Conclusion string
}

// API is what the provider needs of GitHub Actions.
type API interface {
	// Runs are the workflow's push runs of the commit on the branch.
	Runs(ctx context.Context, repository, branch, commit string) ([]Run, error)
	Run(ctx context.Context, repository string, id int64) (Run, error)
	// Rerun runs the run's unsuccessful jobs again, as a new attempt.
	Rerun(ctx context.Context, repository string, id int64) error
	Jobs(ctx context.Context, repository string, id int64, attempt int) ([]RunnerJob, error)
	JobLog(ctx context.Context, repository string, job int64) ([]byte, error)
}

// Provider builds in your fork's Actions.
type Provider struct {
	Repo *git.Repository
	// Fork finds your fork and the remote that pushes to it.
	Fork func(ctx context.Context) (engine.Fork, error)
	API  API
	// Poll is how often it asks after the run; Appear, how long it waits
	// for GitHub to start one. Zero means 30 seconds and 10 minutes.
	Poll, Appear time.Duration
	// Sleep, when set, stands in for waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) sleep(ctx context.Context, d time.Duration) error {
	if p.Sleep != nil {
		return p.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Provider) Execute(ctx context.Context, job engine.Job, build engine.Build) error {
	if err := os.MkdirAll(job.Directory, 0o755); err != nil {
		return err
	}
	poll, appear := p.Poll, p.Appear
	if poll == 0 {
		poll = 30 * time.Second
	}
	if appear == 0 {
		appear = 10 * time.Minute
	}
	fork, err := p.Fork(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", engine.ErrInfrastructure, err)
	}
	branch := BranchPrefix + job.Commit[:12]
	head, err := p.Repo.RemoteHead(ctx, fork.PushURL, branch)
	if err != nil {
		return fmt.Errorf("%w: reading %s on %s: %w", engine.ErrInfrastructure, branch, fork.Repository, err)
	}
	if head != (git.RefValue{Exists: true, Object: job.Commit}) {
		build.Progress(fmt.Sprintf("pushing to %s on %s", branch, fork.Repository))
		if err := p.Repo.Push(ctx, git.Push{Remote: fork.PushURL, Branch: branch, Commit: job.Commit, ExpectedRemote: head}); err != nil {
			return fmt.Errorf("%w: pushing to %s: %w", engine.ErrInfrastructure, fork.Repository, err)
		}
	}

	build.Progress("waiting for GitHub to start " + Workflow + " on " + fork.Repository)
	var run Run
	for waited := time.Duration(0); ; waited += poll {
		runs, err := p.API.Runs(ctx, fork.Repository, branch, job.Commit)
		if err != nil {
			return fmt.Errorf("%w: finding the workflow's run: %w", engine.ErrInfrastructure, err)
		}
		if len(runs) > 0 {
			run = slices.MaxFunc(runs, func(a, b Run) int { return int(a.ID - b.ID) })
			break
		}
		if waited >= appear {
			return fmt.Errorf("%w: GitHub started no run of %s on %s after %s; are Actions enabled for your fork? (https://github.com/%s/actions)",
				engine.ErrInfrastructure, Workflow, branch, appear, fork.Repository)
		}
		if err := p.sleep(ctx, poll); err != nil {
			return err
		}
	}
	// A run that stopped short of building, or whose failure an earlier
	// attempt found no port to blame for, runs again rather than being read
	// again.
	if run.Status == "completed" && (stoppedShort(run.Conclusion) || job.Execution.Attempt > 1 && run.Conclusion != "success") {
		build.Progress(fmt.Sprintf("running %s's unsuccessful jobs again", run.URL))
		if err := p.API.Rerun(ctx, fork.Repository, run.ID); err != nil {
			return fmt.Errorf("%w: running %s again: %w", engine.ErrInfrastructure, run.URL, err)
		}
		previous := run.Attempt
		for run.Attempt == previous {
			if err := p.sleep(ctx, poll); err != nil {
				return err
			}
			if run, err = p.API.Run(ctx, fork.Repository, run.ID); err != nil {
				return fmt.Errorf("%w: %w", engine.ErrInfrastructure, err)
			}
		}
	}
	status := ""
	for run.Status != "completed" {
		if run.Status != status {
			build.Progress(fmt.Sprintf("%s %s", strings.ReplaceAll(run.Status, "_", " "), run.URL))
			status = run.Status
		}
		if err := p.sleep(ctx, poll); err != nil {
			return err
		}
		if run, err = p.API.Run(ctx, fork.Repository, run.ID); err != nil {
			return fmt.Errorf("%w: %w", engine.ErrInfrastructure, err)
		}
	}
	build.Progress(fmt.Sprintf("%s: %s; reading the logs", run.URL, run.Conclusion))

	jobs, err := p.API.Jobs(ctx, fork.Repository, run.ID, run.Attempt)
	if err != nil {
		return fmt.Errorf("%w: listing %s's jobs: %w", engine.ErrInfrastructure, run.URL, err)
	}
	var runners []runner
	for _, j := range jobs {
		if j.Conclusion == "skipped" {
			continue
		}
		log, err := p.API.JobLog(ctx, fork.Repository, j.ID)
		if err != nil {
			return fmt.Errorf("%w: reading %s's log: %w", engine.ErrInfrastructure, j.Name, err)
		}
		path := filepath.Join(job.Directory, logName(j.Name))
		if err := atomicfile.Write(path, log, 0o644); err != nil {
			return err
		}
		runners = append(runners, runner{job: j, log: path, built: ReadLog(log)})
	}

	recorded := 0
	var unbuilt []string
	for _, target := range job.Targets {
		if _, blocked := build.Blocked(target.ID); blocked {
			if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomeBlocked}); err != nil {
				return err
			}
			continue
		}
		result, ok := verdict(target.Target.Name, runners)
		if !ok {
			if !listed(target.Target.Name, runners) {
				unbuilt = append(unbuilt, target.Target.Name)
			}
			continue
		}
		result.Target = target.ID
		if err := build.Record(result); err != nil {
			return err
		}
		recorded++
	}
	if len(unbuilt) > 0 {
		build.Progress(fmt.Sprintf("the workflow did not build %s; it builds only the ports a commit changes", strings.Join(unbuilt, ", ")))
	}
	if recorded == 0 && run.Conclusion != "success" {
		return fmt.Errorf("%w: %s ended %s without building a port; see %s", engine.ErrInfrastructure, run.URL, run.Conclusion, job.Directory)
	}
	return nil
}

// stoppedShort is a conclusion that says nothing about the ports.
func stoppedShort(conclusion string) bool {
	switch conclusion {
	case "cancelled", "timed_out", "startup_failure", "stale":
		return true
	}
	return false
}

// runner is one job's log, read.
type runner struct {
	job   RunnerJob
	log   string
	built map[string]*Built
}

// verdict is a port's result across the runners: failed if it failed on
// any, at the first such runner's phase; passed if it passed on every
// runner, with the log of the first whose tests failed, else the first's.
// A port a runner listed but never reached has no verdict yet.
func verdict(name string, runners []runner) (model.TargetResult, bool) {
	if len(runners) == 0 {
		return model.TargetResult{}, false
	}
	result := model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsNone}
	for _, r := range runners {
		built := r.built[name]
		if built == nil {
			return model.TargetResult{}, false
		}
		outcome, phase := built.Outcome()
		switch {
		case outcome == model.OutcomeFailed:
			return model.TargetResult{Outcome: outcome, Phase: phase, Tests: built.Tests(), Log: r.log}, true
		case outcome != model.OutcomePassed:
			return model.TargetResult{}, false
		}
		switch built.Tests() {
		case model.TestsFailed:
			result.Tests, result.Log = model.TestsFailed, r.log
		case model.TestsPassed:
			if result.Tests == model.TestsNone {
				result.Tests = model.TestsPassed
			}
		}
		if result.Log == "" {
			result.Log = r.log
		}
	}
	return result, true
}

func listed(name string, runners []runner) bool {
	return slices.ContainsFunc(runners, func(r runner) bool { return r.built[name] != nil })
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// logName is a job's log file, named for the job: "build (macos-14)" is
// build-macos-14.log.
func logName(job string) string {
	name := strings.Trim(unsafeName.ReplaceAllString(job, "-"), "-.")
	if name == "" {
		name = "job"
	}
	return name + ".log"
}
