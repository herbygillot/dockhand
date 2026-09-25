package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// JSONVersion is the version of the --json envelope and the results in it.
const JSONVersion = 1

// jsonAnnotation marks a command that can report its result as JSON.
const jsonAnnotation = "dockhand.json"

// supportsJSON marks cmd as able to report its result with --json.
func supportsJSON(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[jsonAnnotation] = "yes"
}

// outputMode is how one command line reports: as text, or as one JSON
// envelope written when it ends.
type outputMode struct {
	json    bool
	command string
	result  any
}

// switchWriter is standard output, silenced while --json holds the
// output for the envelope.
type switchWriter struct {
	w      io.Writer
	silent bool
}

func (s *switchWriter) Write(p []byte) (int, error) {
	if s.silent {
		return len(p), nil
	}
	return s.w.Write(p)
}

// json reports whether the command line asked for --json.
func (s Streams) json() bool { return s.mode != nil && s.mode.json }

// emit keeps a command's result for the --json envelope.
func (s Streams) emit(result any) {
	if s.json() {
		s.mode.result = result
	}
}

// envelope is what --json writes (Design v3 §12).
type envelope struct {
	Version  int     `json:"version"`
	Command  string  `json:"command"`
	ExitCode int     `json:"exit_code"`
	Error    *string `json:"error"`
	Result   any     `json:"result"`
}

func writeEnvelope(out io.Writer, mode *outputMode, err error) error {
	value := envelope{Version: JSONVersion, Command: mode.command, Result: mode.result}
	if err != nil {
		value.ExitCode = ExitCode(err)
		if message := err.Error(); message != "" {
			value.Error = &message
		}
	}
	data, marshalErr := json.MarshalIndent(value, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	_, writeErr := out.Write(append(data, '\n'))
	return writeErr
}

// asksForJSON reports whether a command line asks for --json, before its
// flags are parsed, so a line that fails to parse still gets its envelope.
func asksForJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}

type runJSON struct {
	Name       string     `json:"name"`
	ID         string     `json:"id"`
	State      string     `json:"state"`
	Detail     string     `json:"detail,omitempty"`
	Origin     string     `json:"origin"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func runView(run model.Run) runJSON {
	return runJSON{Name: run.Name(), ID: string(run.ID), State: string(run.State), Detail: run.Detail, Origin: string(run.Origin), CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt}
}

type revisionJSON struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Tree        string `json:"tree"`
	Base        string `json:"base"`
	Commit      string `json:"commit,omitempty"`
}

func revisionView(revision model.Revision) revisionJSON {
	return revisionJSON{ID: string(revision.ID), Kind: string(revision.Kind), Description: engine.Describe(revision), Tree: string(revision.Source.Tree), Base: string(revision.Source.Base), Commit: string(revision.Source.Commit)}
}

type environmentJSON struct {
	Provider     string `json:"provider"`
	OS           string `json:"os,omitempty"`
	Version      string `json:"version,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

func environmentView(environment model.Environment) environmentJSON {
	return environmentJSON{Provider: environment.Provider, OS: environment.Platform.OS, Version: environment.Platform.Version, Architecture: environment.Platform.Architecture}
}

type targetJSON struct {
	Name      string   `json:"name"`
	Portfile  string   `json:"portfile"`
	Subport   string   `json:"subport,omitempty"`
	Kind      string   `json:"kind"`
	Role      string   `json:"role"`
	DependsOn []string `json:"depends_on,omitempty"`
	// Results are the target's outcome in each environment, in the plan's
	// order; empty in a plan that has not run.
	Results []resultJSON `json:"results,omitempty"`
	Passed  *bool        `json:"passed,omitempty"`
}

type resultJSON struct {
	Outcome  string `json:"outcome"`
	Phase    string `json:"phase,omitempty"`
	Tests    string `json:"tests,omitempty"`
	Excluded bool   `json:"excluded,omitempty"`
	Log      string `json:"log,omitempty"`
}

type planJSON struct {
	ID           string            `json:"id"`
	Environments []environmentJSON `json:"environments"`
	Tests        string            `json:"tests"`
	Targets      []targetJSON      `json:"targets"`
	Exclusions   []exclusionJSON   `json:"exclusions"`
	Unresolved   []exclusionJSON   `json:"unresolved"`
}

type exclusionJSON struct {
	Target   string           `json:"target"`
	Platform *environmentJSON `json:"platform,omitempty"`
	Reason   string           `json:"reason"`
}

func planView(plan model.Plan) planJSON {
	view := planJSON{ID: string(plan.ID), Tests: string(plan.Tests), Environments: []environmentJSON{}, Targets: []targetJSON{}, Exclusions: []exclusionJSON{}, Unresolved: []exclusionJSON{}}
	for _, environment := range plan.Environments {
		view.Environments = append(view.Environments, environmentView(environment))
	}
	for _, target := range plan.Targets {
		view.Targets = append(view.Targets, targetView(target))
	}
	for _, exclusion := range plan.Exclusions {
		platform := environmentView(model.Environment{Platform: exclusion.Platform})
		view.Exclusions = append(view.Exclusions, exclusionJSON{Target: exclusion.Target.Name, Platform: &platform, Reason: exclusion.Reason})
	}
	for _, unresolved := range plan.Unresolved {
		view.Unresolved = append(view.Unresolved, exclusionJSON{Target: unresolved.Target.Name, Reason: unresolved.Reason})
	}
	return view
}

func targetView(target model.PlanTarget) targetJSON {
	view := targetJSON{Name: string(target.ID), Portfile: target.Target.Portfile, Subport: target.Target.Subport, Kind: string(target.Kind), Role: string(target.Role)}
	for _, dependency := range target.DependsOn {
		view.DependsOn = append(view.DependsOn, string(dependency))
	}
	return view
}

// evidenceView is a finished run's targets with their results.
func evidenceView(evidence engine.Evidence) []targetJSON {
	targets := []targetJSON{}
	for _, target := range evidence.Targets {
		view := targetView(target.Target)
		passed := target.Passed
		view.Passed = &passed
		for i, result := range target.Outcomes {
			view.Results = append(view.Results, resultJSON{Outcome: string(result.Outcome), Phase: string(result.Phase), Tests: string(result.Tests), Log: result.Log,
				Excluded: engine.Excluded(evidence.Plan, target.Target, evidence.Plan.Environments[i].Platform)})
		}
		targets = append(targets, view)
	}
	return targets
}

// checkJSON is check's, wait's, and retry's result.
type checkJSON struct {
	Branch   string        `json:"branch"`
	Revision *revisionJSON `json:"revision,omitempty"`
	Plan     *planJSON     `json:"plan,omitempty"`
	Run      *runJSON      `json:"run"`
	// Targets are the finished run's results; empty until it finishes.
	Targets []targetJSON `json:"targets"`
}

type pullRequestJSON struct {
	Repository string     `json:"repository"`
	Number     int        `json:"number"`
	URL        string     `json:"url"`
	Head       string     `json:"head"`
	Pushed     string     `json:"pushed"`
	Draft      bool       `json:"draft"`
	State      string     `json:"state,omitempty"`
	Review     string     `json:"review,omitempty"`
	Checks     string     `json:"checks,omitempty"`
	Failing    []string   `json:"failing,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

type latestJSON struct {
	Run      runJSON      `json:"run"`
	Revision revisionJSON `json:"revision"`
	// Current is true when it checked exactly the files as they are now.
	Current bool         `json:"current"`
	Targets []targetJSON `json:"targets"`
}

type branchJSON struct {
	Name        string           `json:"name"`
	GitBranch   string           `json:"git_branch"`
	ID          string           `json:"id"`
	State       string           `json:"state"`
	Worktree    string           `json:"worktree"`
	Managed     bool             `json:"managed"`
	Base        string           `json:"base"`
	Head        string           `json:"head,omitempty"`
	Missing     bool             `json:"missing,omitempty"`
	Commits     int              `json:"commits"`
	Edited      []string         `json:"edited"`
	Directories []string         `json:"directories"`
	Ports       []string         `json:"ports"`
	Latest      *latestJSON      `json:"latest_check"`
	Active      []runJSON        `json:"active_checks"`
	PullRequest *pullRequestJSON `json:"pull_request"`
}

func branchView(status engine.BranchStatus) branchJSON {
	branch := status.Branch
	view := branchJSON{Name: branch.ShortName(), GitBranch: branch.Name, ID: string(branch.ID), State: string(branch.State), Worktree: branch.Worktree, Managed: branch.Managed,
		Base: string(branch.Base), Head: status.Head, Missing: status.Missing, Commits: status.Commits,
		Edited: nonNil(status.Edited), Directories: nonNil(status.Scope.Ports), Ports: nonNil(status.Scope.PortNames()), Active: []runJSON{}}
	for _, run := range status.Active {
		view.Active = append(view.Active, runView(run))
	}
	if status.Latest != nil && status.LatestRevision != nil {
		latest := latestJSON{Run: runView(*status.Latest), Revision: revisionView(*status.LatestRevision), Current: status.Current, Targets: []targetJSON{}}
		if status.Evidence != nil {
			latest.Targets = evidenceView(*status.Evidence)
		}
		view.Latest = &latest
	}
	if pr := branch.PullRequest; pr != nil {
		view.PullRequest = &pullRequestJSON{Repository: pr.Repository, Number: pr.Number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", pr.Repository, pr.Number),
			Head: pr.Head, Pushed: string(pr.Pushed), Draft: pr.Draft}
		if observed := pr.Observed; observed != nil {
			at := observed.At
			view.PullRequest.State, view.PullRequest.Review, view.PullRequest.Checks, view.PullRequest.Failing, view.PullRequest.ObservedAt = observed.State, observed.Review, observed.Checks, observed.Failing, &at
		}
	}
	return view
}

type attentionJSON struct {
	// Kind is failed (✗), needs_you (!), or ready (·).
	Kind   string `json:"kind"`
	Branch string `json:"branch"`
	What   string `json:"what"`
	Next   string `json:"next"`
}

func attentionView(rows []attention) []attentionJSON {
	kinds := map[string]string{"✗": "failed", "!": "needs_you", "·": "ready"}
	views := []attentionJSON{}
	for _, row := range rows {
		views = append(views, attentionJSON{Kind: kinds[row.mark], Branch: row.branch, What: row.what, Next: row.next})
	}
	return views
}

type statusJSON struct {
	Attention []attentionJSON `json:"attention"`
	Branches  []branchJSON    `json:"branches,omitempty"`
	Serve     string          `json:"serve,omitempty"`
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

type queueJSON struct {
	Serve string          `json:"serve"`
	Runs  []queuedRunJSON `json:"runs"`
}

type queuedRunJSON struct {
	runJSON
	Branch       string            `json:"branch"`
	Revision     string            `json:"revision"`
	Environments []environmentJSON `json:"environments"`
}

type diffJSON struct {
	Branch string           `json:"branch"`
	Base   string           `json:"base"`
	Edited []string         `json:"edited"`
	Ports  []portChangeJSON `json:"ports"`
	Other  []string         `json:"not_built_by_ci"`
	Files  []string         `json:"files"`
	Patch  string           `json:"patch,omitempty"`
}

// portChangeJSON is one changed port directory: changed, revision_only,
// new_port, or removed.
type portChangeJSON struct {
	Directory string `json:"directory"`
	Change    string `json:"change"`
}

func diffView(diff engine.BranchDiff, patch bool) diffJSON {
	view := diffJSON{Branch: diff.Status.Branch.ShortName(), Base: string(diff.Status.Branch.Base), Edited: nonNil(diff.Status.Edited), Other: nonNil(diff.Other), Files: nonNil(diff.Files)}
	view.Ports = []portChangeJSON{}
	for _, port := range diff.Ports {
		view.Ports = append(view.Ports, portChangeJSON{Directory: port.Directory, Change: strings.ReplaceAll(portChangeWords(port), " ", "_")})
	}
	if patch {
		view.Patch = string(diff.Patch)
	}
	return view
}

type impactJSON struct {
	Branch          string          `json:"branch"`
	Changed         []string        `json:"changed_directories"`
	LookedFor       []string        `json:"dependents_of"`
	Dependents      []dependentJSON `json:"dependents"`
	DependentsError string          `json:"dependents_error,omitempty"`
	Shared          []sharedJSON    `json:"shared_files"`
}

type dependentJSON struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory"`
	On        []string `json:"on"`
	Phases    []string `json:"phases"`
}

type sharedJSON struct {
	Path      string   `json:"path"`
	PortGroup string   `json:"port_group,omitempty"`
	Users     []string `json:"loaded_by"`
}

func impactView(impact engine.Impact) impactJSON {
	view := impactJSON{Branch: impact.Diff.Status.Branch.ShortName(), Changed: nonNil(impact.Diff.Status.Scope.Ports), LookedFor: nonNil(impact.Of),
		Dependents: []dependentJSON{}, DependentsError: impact.Unread, Shared: []sharedJSON{}}
	for _, dependent := range impact.Dependents {
		view.Dependents = append(view.Dependents, dependentJSON{Name: dependent.Name, Directory: dependent.Directory, On: nonNil(dependent.On), Phases: nonNil(dependent.Phases)})
	}
	for _, shared := range impact.Shared {
		view.Shared = append(view.Shared, sharedJSON{Path: shared.Path, PortGroup: shared.PortGroup, Users: nonNil(shared.Users)})
	}
	return view
}
