// Package actions is the github provider (Design v3 §7): it builds a
// revision with MacPorts' own workflow, in your fork. It pushes the
// revision's commit to a branch of your fork, where the workflow runs on
// push as it does for every branch but master, waits for that run, reads
// each runner's log for what it says about each port, and removes the
// branch once the check is done with the run. The workflow
// builds the ports the commit changes, on each macOS its matrix names;
// dockhand does not choose the runners, and ports it does not change are
// not built. Nor does the workflow say which commit a Git-fetched port's
// fetch checked out, so this provider never reports one (Build.Fetched):
// such a result's source is unknown, which the engine says, and it stands
// for its own check alone.
package ghactions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Workflow is MacPorts' workflow, the one that builds a branch's changed
// ports on push.
const Workflow = "main.yml"

// BranchPrefix names the branches the provider pushes to your fork. It
// removes each once the check is done with its run; clean removes a merged
// branch's that are left.
const BranchPrefix = buildenv.CheckBranchPrefix

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
	// Labels are the labels the job asked its runner for, its runs-on,
	// macos-15; RunnerName names the runner that took it, and is empty
	// for a job no runner took.
	Labels     []string
	RunnerName string
}

// GitHub's labels for its macOS runners name a release, macos-15, with a
// runner's size or architecture after it, macos-15-xlarge or
// macos-15-intel; macos-latest names none.
var releaseLabel = regexp.MustCompile(`^macos-(\d+(?:\.\d+)?)(?:-[a-z0-9-]+)?$`)

// Release is the macOS release of the runner that took the job, as its
// labels name it: 15 for macos-15. Labels that name no release, such as
// macos-latest, whose release GitHub moves, say nothing, nor do labels
// that name two, nor a job no runner took.
func (j RunnerJob) Release() string {
	if j.RunnerName == "" {
		return ""
	}
	release := ""
	for _, label := range j.Labels {
		match := releaseLabel.FindStringSubmatch(strings.ToLower(label))
		switch {
		case match == nil:
		case release != "" && release != match[1]:
			return ""
		default:
			release = match[1]
		}
	}
	return release
}

// API is what the provider needs of GitHub Actions.
type API interface {
	// Runs are the workflow's push runs of the commit on the branch.
	Runs(ctx context.Context, repository, branch, commit string) ([]Run, error)
	Run(ctx context.Context, repository string, id int64) (Run, error)
	// Rerun runs the run's unsuccessful jobs again, as a new attempt.
	Rerun(ctx context.Context, repository string, id int64) error
	// Cancel stops a run that has not finished.
	Cancel(ctx context.Context, repository string, id int64) error
	Jobs(ctx context.Context, repository string, id int64, attempt int) ([]RunnerJob, error)
	JobLog(ctx context.Context, repository string, job int64) ([]byte, error)
}

// Provider builds in your fork's Actions.
type Provider struct {
	Repo *git.Repository
	// Fork finds your fork and the remote that pushes to it.
	Fork func(ctx context.Context) (buildenv.Fork, error)
	API  API
	// Poll is how often it asks after the run; Appear, how long it waits
	// for GitHub to start one. Zero means 30 seconds and 10 minutes.
	Poll, Appear time.Duration
	// Sleep, when set, stands in for waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

func (p *Provider) Name() string { return buildenv.GitHub }

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

func (p *Provider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
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
		return fmt.Errorf("%w: %w", buildenv.ErrInfrastructure, err)
	}
	branch := BranchPrefix + job.Commit[:12]
	head, err := p.Repo.RemoteHead(ctx, fork.PushURL, branch)
	if err != nil {
		return fmt.Errorf("%w: reading %s on %s: %w", buildenv.ErrInfrastructure, branch, fork.Repository, err)
	}
	// GitHub keeps listing a branch's runs once the branch is gone, so a
	// run of the commit from before the push, an earlier check's whose
	// branch was removed once it was read, isn't this check's: the push
	// starts one of its own.
	var before int64
	if head != (git.RefValue{Exists: true, Object: job.Commit}) {
		earlier, err := p.API.Runs(ctx, fork.Repository, branch, job.Commit)
		if err != nil {
			return fmt.Errorf("%w: finding the workflow's earlier runs: %w", buildenv.ErrInfrastructure, err)
		}
		for _, r := range earlier {
			before = max(before, r.ID)
		}
		build.Progress(fmt.Sprintf("pushing to %s on %s", branch, fork.Repository))
		if err := p.Repo.Push(ctx, git.Push{Remote: fork.PushURL, Branch: branch, Commit: job.Commit, ExpectedRemote: head}); err != nil {
			return fmt.Errorf("%w: pushing to %s: %w", buildenv.ErrInfrastructure, fork.Repository, err)
		}
	}

	build.Progress("waiting for GitHub to start " + Workflow + " on " + fork.Repository)
	var run Run
	for waited := time.Duration(0); ; waited += poll {
		runs, err := p.API.Runs(ctx, fork.Repository, branch, job.Commit)
		if err != nil {
			return fmt.Errorf("%w: finding the workflow's run: %w", buildenv.ErrInfrastructure, err)
		}
		runs = slices.DeleteFunc(runs, func(r Run) bool { return r.ID <= before })
		if len(runs) > 0 {
			run = slices.MaxFunc(runs, func(a, b Run) int { return int(a.ID - b.ID) })
			break
		}
		if waited >= appear {
			return fmt.Errorf("%w: GitHub started no run of %s on %s after %s; are Actions enabled for your fork? (https://github.com/%s/actions)",
				buildenv.ErrInfrastructure, Workflow, branch, appear, fork.Repository)
		}
		if err := p.sleep(ctx, poll); err != nil {
			return err
		}
	}
	// The workflow run's URL is how the pull request names this run, and
	// how dockhand logs finds it.
	if err := build.Refer(run.URL); err != nil {
		return err
	}
	// A canceled check cancels its run; a driver that is only stopping
	// leaves it, for the next driver to find again.
	defer func() {
		if ctx.Err() != nil && build.Canceled() && run.Status != "completed" {
			stopping, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			_ = p.API.Cancel(stopping, fork.Repository, run.ID)
		}
	}()
	// A run that stopped short of building, or whose failure an earlier
	// attempt found no port to blame for, runs again rather than being read
	// again.
	if run.Status == "completed" && (stoppedShort(run.Conclusion) || job.Execution.Attempt > 1 && run.Conclusion != "success") {
		build.Progress(fmt.Sprintf("running %s's unsuccessful jobs again", run.URL))
		if err := p.API.Rerun(ctx, fork.Repository, run.ID); err != nil {
			return fmt.Errorf("%w: running %s again: %w", buildenv.ErrInfrastructure, run.URL, err)
		}
		previous := run.Attempt
		for run.Attempt == previous {
			if err := p.sleep(ctx, poll); err != nil {
				return err
			}
			if run, err = p.API.Run(ctx, fork.Repository, run.ID); err != nil {
				return fmt.Errorf("%w: %w", buildenv.ErrInfrastructure, err)
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
			return fmt.Errorf("%w: %w", buildenv.ErrInfrastructure, err)
		}
	}
	build.Progress(fmt.Sprintf("%s: %s; reading the logs", run.URL, run.Conclusion))
	err = p.read(ctx, job, build, fork.Repository, run)
	// The check is done with the run once it is read, or once its last
	// attempt has ended: its logs are kept here, and nothing reads the run
	// again. Until then the branch stays, since a later attempt runs the
	// run again (Rerun), as does a driver that stopped with the check left
	// for the next.
	if ctx.Err() == nil && (err == nil || job.Execution.Attempt >= model.MaxAttempts) {
		p.removeBranch(ctx, build, fork, branch, job.Commit)
	}
	return err
}

// removeBranch removes the check's branch from your fork, while it still
// holds the commit pushed. One it can't remove is said, once, and left for
// clean, which removes a merged branch's; it never fails the check.
func (p *Provider) removeBranch(ctx context.Context, build buildenv.Build, fork buildenv.Fork, branch, commit string) {
	err := p.Repo.DeleteRemoteBranch(ctx, fork.PushURL, branch, git.RefValue{Exists: true, Object: commit})
	var moved *git.RefConflict
	switch {
	case errors.As(err, &moved):
		build.Progress(fmt.Sprintf("left %s on %s, since it has moved since the check", branch, fork.Repository))
	case err != nil:
		build.Progress(fmt.Sprintf("couldn't remove %s from %s, which dockhand clean removes once the branch is merged: %v", branch, fork.Repository, err))
	default:
		build.Progress(fmt.Sprintf("removed %s from %s", branch, fork.Repository))
	}
}

// read reads a completed run: what its runners were, each runner's log,
// kept in the job's directory, and from them each target's result.
func (p *Provider) read(ctx context.Context, job buildenv.Job, build buildenv.Build, repository string, run Run) error {
	jobs, err := p.API.Jobs(ctx, repository, run.ID, run.Attempt)
	if err != nil {
		return fmt.Errorf("%w: listing %s's jobs: %w", buildenv.ErrInfrastructure, run.URL, err)
	}
	if err := observe(build, jobs); err != nil {
		return err
	}
	var runners []runner
	for _, j := range jobs {
		if j.Conclusion == "skipped" {
			continue
		}
		log, err := p.API.JobLog(ctx, repository, j.ID)
		if err != nil {
			return fmt.Errorf("%w: reading %s's log: %w", buildenv.ErrInfrastructure, j.Name, err)
		}
		path := filepath.Join(job.Directory, logName(j.Name))
		if err := atomicfile.Write(path, log, 0o644); err != nil {
			return err
		}
		runners = append(runners, runner{job: j, log: path, built: ReadLog(log), listing: ListsSubports(log)})
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
		return fmt.Errorf("%w: %s ended %s without building a port; see %s", buildenv.ErrInfrastructure, run.URL, run.Conclusion, job.Directory)
	}
	return nil
}

// observe records the macOS release of each of the run's runners, as its
// job's labels name it (RunnerJob.Release), for the pull request's Tested
// on: the jobs API is GitHub's documented word for it. A runner's Xcode
// is only in its log's text, which isn't a documented interface, so it
// isn't recorded. Where no runner's labels name a release, nothing is.
func observe(build buildenv.Build, jobs []RunnerJob) error {
	var observed model.Observed
	named := false
	for _, j := range jobs {
		if j.Conclusion == "skipped" {
			continue
		}
		builder := model.BuilderObserved{Builder: j.Name, MacOS: j.Release()}
		observed.Builders = append(observed.Builders, builder)
		named = named || builder.MacOS != ""
	}
	if !named {
		return nil
	}
	// One order, whichever GitHub lists them in, so that two attempts'
	// reports of the same runners are the same.
	slices.SortStableFunc(observed.Builders, func(a, b model.BuilderObserved) int { return strings.Compare(a.Builder, b.Builder) })
	return build.Observe(observed)
}

// stoppedShort is a conclusion that says nothing about the ports.
func stoppedShort(conclusion string) bool {
	switch conclusion {
	case "cancelled", "timed_out", "startup_failure", "stale":
		return true
	}
	return false
}

// RunsOwnTests says the workflow runs a port's declared tests whatever the
// check's policy: it is MacPorts' own, and dockhand doesn't change it.
func (p *Provider) RunsOwnTests() bool { return true }

var _ buildenv.OwnTestsProvider = (*Provider)(nil)

// runner is one job's log, read.
type runner struct {
	job   RunnerJob
	log   string
	built map[string]*Built
	// listing is true when the log shows the workflow listing the
	// subports it would build, which a runner that stopped first doesn't.
	listing bool
}

// verdict is a port's result across the runners, with each runner's part
// (the architecture review of 2026-09-27: the runners had been folded into
// one result, and which built what was lost). A runner that listed the
// subports without this one didn't build it, as the workflow leaves a
// port off a macOS it doesn't support, and its part is not run. Of the
// runners that built it, the port failed if it failed on any, at the first
// such runner's phase, with that runner named; and passed if it passed on
// all of them, with the log of the first whose tests failed, else the
// first's. There is no verdict yet while a runner hasn't listed the
// subports, or listed the port and never reached it, nor when none built
// it.
func verdict(name string, runners []runner) (model.TargetResult, bool) {
	var parts []model.BuilderResult
	var failed *model.BuilderResult
	result := model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsNone}
	for _, r := range runners {
		if !r.listing {
			return model.TargetResult{}, false
		}
		part := model.BuilderResult{Builder: r.job.Name, Outcome: model.OutcomeNotRun}
		built := r.built[name]
		if built == nil || !built.Listed {
			parts = append(parts, part)
			continue
		}
		part.Outcome, part.Phase = built.Outcome()
		part.Tests, part.Log = built.Tests(), r.log
		parts = append(parts, part)
		switch {
		case part.Outcome == model.OutcomeFailed:
			if failed == nil {
				failed = &parts[len(parts)-1]
			}
			continue
		case part.Outcome != model.OutcomePassed:
			return model.TargetResult{}, false
		}
		switch part.Tests {
		case model.TestsFailed:
			if result.Tests != model.TestsFailed {
				result.Tests, result.Log = model.TestsFailed, r.log
			}
		case model.TestsPassed:
			if result.Tests == model.TestsNone {
				result.Tests = model.TestsPassed
			}
		}
		if result.Log == "" {
			result.Log = r.log
		}
	}
	built := slices.ContainsFunc(parts, func(part model.BuilderResult) bool { return part.Outcome != model.OutcomeNotRun })
	switch {
	case !built:
		return model.TargetResult{}, false
	case failed != nil:
		result = model.TargetResult{Outcome: model.OutcomeFailed, Phase: failed.Phase, Tests: failed.Tests, Log: failed.Log, Detail: "on " + failed.Builder}
	}
	result.Builders = parts
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
