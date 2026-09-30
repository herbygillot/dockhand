// Package script is the command provider (Design v3 §7): a person's own
// build script. It is given a request file naming the revision, as a Git
// bundle, the targets in order, the platform, and the test policy; it
// writes a result file with each target's outcome. Dockhand cannot vouch
// for how it built, so its results read "reported by <name>".
package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Version is the request and result files' format.
const Version = 1

// Request is the file the script is given.
type Request struct {
	Version int    `json:"version"`
	Run     string `json:"run"`
	Attempt int    `json:"attempt"`
	// Execution is this provider run's ID, command_7y62p4sigena6xlr, which
	// the pull request names and dockhand logs finds it by.
	Execution string `json:"execution"`
	// Bundle holds Commit under Ref, less what Base already holds: fetch
	// Base (MacPorts' master has it) before fetching the bundle.
	Bundle   string         `json:"bundle"`
	Ref      string         `json:"ref"`
	Commit   string         `json:"commit"`
	Base     string         `json:"base"`
	Platform model.Platform `json:"platform"`
	Tests    string         `json:"tests"`
	Targets  []Target       `json:"targets"`
	// Result is where the script writes its result file, and Logs a
	// directory for its logs.
	Result string `json:"result"`
	Logs   string `json:"logs"`
}

// Target is one port to build, in the order given.
type Target struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Portfile  string          `json:"portfile"`
	Subport   string          `json:"subport,omitempty"`
	Variants  map[string]bool `json:"variants,omitempty"`
	Kind      string          `json:"kind"`
	Role      string          `json:"role"`
	DependsOn []string        `json:"depends_on,omitempty"`
	// Git is what a port fetched with Git fetches; absent for one fetched
	// otherwise.
	Git *Git `json:"git,omitempty"`
}

// Git is what a Git-fetched target's fetch clones, and must check out
// (batch 20): git.url, git.branch, empty for the default branch, and the
// commit git.branch named when the check was planned. Commit is absent
// where dockhand couldn't resolve it, and where git.branch abbreviates a
// commit, which only a clone expands.
type Git struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// Result is the file the script writes.
type Result struct {
	Version int            `json:"version"`
	Targets []TargetResult `json:"targets"`
	// Reference is the script's own name for the run, such as its CI's
	// URL, which dockhand records, and a pull request names when it's a
	// link.
	Reference string `json:"reference,omitempty"`
}

// TargetResult is one target's outcome: passed, failed (with the phase it
// stopped at), or blocked.
type TargetResult struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase,omitempty"`
	Tests   string `json:"tests,omitempty"`
	Log     string `json:"log,omitempty"`
	// Fetched is the commit a Git-fetched target's fetch checked out,
	// where the script read it; one other than its request's commit fails
	// the target at fetch, whatever its outcome says.
	Fetched string `json:"fetched,omitempty"`
}

// Provider runs the script.
type Provider struct {
	// Run is the command, run by sh with the request file's path as $1.
	Run   string
	Label string
	Repo  *git.Repository
	// Grace is how long a canceled command has to stop, after an
	// interrupt, before what still runs is killed; 30 seconds when zero.
	Grace time.Duration
}

func (p *Provider) Name() string { return buildenv.Command }

var _ buildenv.Provider = (*Provider)(nil)

func (p *Provider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	if err := os.MkdirAll(job.Directory, 0o755); err != nil {
		return err
	}
	request := Request{Version: Version, Run: job.Run.Name(), Attempt: job.Execution.Attempt, Execution: string(job.Execution.ID),
		Bundle: filepath.Join(job.Directory, "source.bundle"), Ref: "refs/dockhand/check/" + job.Run.Name(),
		Commit: job.Commit, Base: string(job.Revision.Source.Base), Platform: job.Environment.Platform, Tests: string(job.Plan.Tests),
		Result: filepath.Join(job.Directory, "result.json"), Logs: job.Directory}
	for _, target := range job.Targets {
		t := Target{ID: string(target.ID), Name: target.Target.Name, Portfile: target.Target.Portfile, Subport: target.Target.Subport,
			Variants: target.Target.Variants, Kind: string(target.Kind), Role: string(target.Role)}
		for _, dep := range target.DependsOn {
			t.DependsOn = append(t.DependsOn, string(dep))
		}
		if target.Git != nil {
			t.Git = &Git{URL: target.Git.URL, Branch: target.Git.Ref, Commit: string(target.Git.Commit)}
		}
		request.Targets = append(request.Targets, t)
	}
	build.Progress("bundling the revision")
	// A baseline builds the base itself; git refuses an empty bundle, so it
	// holds the base less its parent, which the receiver has too.
	exclude := request.Base
	if request.Commit == request.Base {
		parent, err := p.Repo.Resolve(ctx, request.Base+"^")
		if err != nil {
			return fmt.Errorf("%w: bundling the base: %w", buildenv.ErrInfrastructure, err)
		}
		exclude = parent
	}
	if err := p.Repo.Bundle(ctx, request.Bundle, request.Ref, request.Commit, exclude); err != nil {
		return fmt.Errorf("%w: bundling the revision: %w", buildenv.ErrInfrastructure, err)
	}
	requestPath := filepath.Join(job.Directory, "request.json")
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(requestPath, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(request.Result)

	logPath := filepath.Join(job.Directory, "command.log")
	log, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer log.Close()
	build.Progress("running " + p.Label)
	command := exec.CommandContext(ctx, "sh", "-c", p.Run+` "$1"`, "sh", requestPath)
	grace := p.Grace
	if grace <= 0 {
		grace = 30 * time.Second
	}
	ownSession(command, grace)
	command.Dir = job.Directory
	command.Env = append(os.Environ(), "DOCKHAND_REQUEST="+requestPath)
	command.Stdout, command.Stderr = log, log
	runErr := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	data, err = os.ReadFile(request.Result)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s wrote no result file (%v); see %s", buildenv.ErrInfrastructure, p.Label, runErr, logPath)
	}
	if err != nil {
		return err
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("%w: %s wrote an unreadable result file, %s: %w", buildenv.ErrInfrastructure, p.Label, request.Result, err)
	}
	if result.Version != Version {
		return fmt.Errorf("%w: %s wrote a version %d result file, %s; dockhand reads version %d", buildenv.ErrInfrastructure, p.Label, result.Version, request.Result, Version)
	}
	if result.Reference != "" {
		if err := build.Refer(result.Reference); err != nil {
			return err
		}
	}
	reported := map[string]TargetResult{}
	for _, target := range result.Targets {
		reported[target.ID] = target
	}
	// Results are recorded in the plan's order, so a target whose changed
	// dependency did not pass is blocked whatever the script said: an old
	// binary never stands in for the dependency.
	for _, target := range job.Targets {
		if _, blocked := build.Blocked(target.ID); blocked {
			if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomeBlocked}); err != nil {
				return err
			}
			continue
		}
		got, ok := reported[string(target.ID)]
		if !ok {
			continue
		}
		if got.Log != "" && !filepath.IsAbs(got.Log) {
			got.Log = filepath.Join(job.Directory, got.Log)
		}
		recorded, err := convert(target.ID, got, logPath)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", buildenv.ErrInfrastructure, p.Label, err)
		}
		// What a Git fetch checked out is the script's to say; the engine
		// judges it by the commit the request named.
		if got.Fetched != "" {
			if !git.ValidObjectID(got.Fetched) {
				return fmt.Errorf("%w: %s: %s fetched %q, which isn't a commit", buildenv.ErrInfrastructure, p.Label, target.ID, got.Fetched)
			}
			build.Fetched(target.ID, got.Fetched)
		}
		if err := build.Record(recorded); err != nil {
			return err
		}
	}
	return nil
}

func convert(id model.TargetID, got TargetResult, fallbackLog string) (model.TargetResult, error) {
	result := model.TargetResult{Target: id, Outcome: model.Outcome(got.Outcome), Phase: model.Phase(got.Phase), Tests: model.TestOutcome(got.Tests), Log: got.Log}
	switch result.Outcome {
	case model.OutcomePassed, model.OutcomeBlocked:
		result.Phase = ""
	case model.OutcomeFailed:
		if !result.Phase.Valid() {
			return model.TargetResult{}, fmt.Errorf("%s failed at unknown phase %q", id, got.Phase)
		}
	default:
		return model.TargetResult{}, fmt.Errorf("%s has unknown outcome %q", id, got.Outcome)
	}
	if result.Tests == "" {
		result.Tests = model.TestsNone
	}
	if !result.Tests.Valid() {
		return model.TargetResult{}, fmt.Errorf("%s has unknown tests outcome %q", id, got.Tests)
	}
	if result.Log == "" {
		result.Log = fallbackLog
	}
	return result, nil
}
