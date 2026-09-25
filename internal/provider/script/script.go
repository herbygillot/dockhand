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

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/engine"
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
}

// Result is the file the script writes.
type Result struct {
	Version int            `json:"version"`
	Targets []TargetResult `json:"targets"`
}

// TargetResult is one target's outcome: passed, failed (with the phase it
// stopped at), or blocked.
type TargetResult struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase,omitempty"`
	Tests   string `json:"tests,omitempty"`
	Log     string `json:"log,omitempty"`
}

// Provider runs the script.
type Provider struct {
	// Run is the command, run by sh with the request file's path as $1.
	Run   string
	Label string
	Repo  *git.Repository
}

func (p *Provider) Name() string { return "command" }

func (p *Provider) Execute(ctx context.Context, job engine.Job, build engine.Build) error {
	if err := os.MkdirAll(job.Directory, 0o755); err != nil {
		return err
	}
	request := Request{Version: Version, Run: job.Run.Name(), Attempt: job.Execution.Attempt,
		Bundle: filepath.Join(job.Directory, "source.bundle"), Ref: "refs/dockhand/check/" + job.Run.Name(),
		Commit: job.Commit, Base: string(job.Revision.Source.Base), Platform: job.Environment.Platform, Tests: string(job.Plan.Tests),
		Result: filepath.Join(job.Directory, "result.json"), Logs: job.Directory}
	for _, target := range job.Targets {
		t := Target{ID: string(target.ID), Name: target.Target.Name, Portfile: target.Target.Portfile, Subport: target.Target.Subport,
			Variants: target.Target.Variants, Kind: string(target.Kind), Role: string(target.Role)}
		for _, dep := range target.DependsOn {
			t.DependsOn = append(t.DependsOn, string(dep))
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
			return fmt.Errorf("%w: bundling the base: %w", engine.ErrInfrastructure, err)
		}
		exclude = parent
	}
	if err := p.Repo.Bundle(ctx, request.Bundle, request.Ref, request.Commit, exclude); err != nil {
		return fmt.Errorf("%w: bundling the revision: %w", engine.ErrInfrastructure, err)
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
	command.Dir = job.Directory
	command.Env = append(os.Environ(), "DOCKHAND_REQUEST="+requestPath)
	command.Stdout, command.Stderr = log, log
	runErr := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	data, err = os.ReadFile(request.Result)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s wrote no result file (%v); see %s", engine.ErrInfrastructure, p.Label, runErr, logPath)
	}
	if err != nil {
		return err
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("%w: %s wrote an unreadable result file, %s: %w", engine.ErrInfrastructure, p.Label, request.Result, err)
	}
	if result.Version != Version {
		return fmt.Errorf("%w: %s wrote a version %d result file, %s; dockhand reads version %d", engine.ErrInfrastructure, p.Label, result.Version, request.Result, Version)
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
			return fmt.Errorf("%w: %s: %w", engine.ErrInfrastructure, p.Label, err)
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
		switch result.Phase {
		case model.PhaseLint, model.PhaseFetch, model.PhaseChecksum, model.PhaseInstall, model.PhaseTest:
		default:
			return model.TargetResult{}, fmt.Errorf("%s failed at unknown phase %q", id, got.Phase)
		}
	default:
		return model.TargetResult{}, fmt.Errorf("%s has unknown outcome %q", id, got.Outcome)
	}
	switch result.Tests {
	case "":
		result.Tests = model.TestsNone
	case model.TestsPassed, model.TestsFailed, model.TestsTimedOut, model.TestsNone, model.TestsSkipped:
	default:
		return model.TargetResult{}, fmt.Errorf("%s has unknown tests outcome %q", id, got.Tests)
	}
	if result.Log == "" {
		result.Log = fallbackLog
	}
	return result, nil
}
