package command

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
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
	// whole, while a command goes on from one step to the next, is its one
	// result, which each step's result is gathered into (linkSteps).
	whole gatherer
}

// A gatherer is a command's result that the steps it goes on to report
// into, so that the command has one shape wherever it stops: what a step
// didn't reach is left out.
type gatherer interface {
	gather(step any)
	result() any
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
	if !s.json() {
		return
	}
	if whole := s.mode.whole; whole != nil {
		whole.gather(result)
		s.mode.result = whole.result()
		return
	}
	s.mode.result = result
}

// linkSteps makes whole the command's one result, with each later step's
// inside it, so a script reads one shape wherever the command stopped:
// update --submit's and bump's are the update's, submit --check's the
// submission's, and check's the check's. A command already gathering keeps
// its own, as update --submit keeps it through submit --check.
func (s Streams) linkSteps(whole gatherer) {
	if s.json() && s.mode.whole == nil {
		s.mode.whole = whole
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
	// Stopped is true for a run recorded as running that no live process
	// drives; the state says running, as the record does.
	Stopped bool `json:"stopped,omitempty"`
	// BaselineOf is the ID of the check a baseline looks into; absent for
	// a check of the branch itself.
	BaselineOf string `json:"baseline_of,omitempty"`
}

func runView(run model.Run) runJSON {
	return runJSON{Name: run.Name(), ID: string(run.ID), State: string(run.State), Detail: run.Detail, Origin: string(run.Origin), CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt,
		BaselineOf: string(run.BaselineOf)}
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
	Provider       string `json:"provider"`
	OS             string `json:"os,omitempty"`
	Version        string `json:"version,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
	DeveloperTools string `json:"developer_tools,omitempty"`
}

func environmentView(environment model.Environment) environmentJSON {
	return environmentJSON{Provider: environment.Provider, OS: environment.Platform.OS, Version: environment.Platform.Version, Architecture: environment.Platform.Architecture,
		DeveloperTools: string(environment.DeveloperTools)}
}

type targetJSON struct {
	Name      string   `json:"name"`
	Portfile  string   `json:"portfile"`
	Subport   string   `json:"subport,omitempty"`
	Kind      string   `json:"kind"`
	Role      string   `json:"role"`
	DependsOn []string `json:"depends_on,omitempty"`
	// NeedsXcode are the environments where the target needs Xcode.
	NeedsXcode []environmentJSON `json:"needs_xcode,omitempty"`
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
	// Detail is why the target stopped, in its provider's words.
	Detail string `json:"detail,omitempty"`
	// ReusedFrom is the provider run that built it, where this result is
	// an earlier build's, reused.
	ReusedFrom string `json:"reused_from,omitempty"`
	// Builders are each builder's part, where the provider's run has
	// several, as MacPorts' workflow has a runner for each macOS release.
	Builders []builderJSON `json:"builders,omitempty"`
	// Remade is true where a result was recorded before its environment
	// was made again, and so no longer stands for it: the outcome reads
	// not_run.
	Remade bool   `json:"remade,omitempty"`
	Log    string `json:"log,omitempty"`
}

type planJSON struct {
	ID           string            `json:"id"`
	Environments []environmentJSON `json:"environments"`
	Tests        string            `json:"tests"`
	// Only, Also, and Fresh are what the person asked: --only's changed
	// ports, --also's unchanged ones, and --fresh.
	Only  []string `json:"only"`
	Also  []string `json:"also"`
	Fresh bool     `json:"fresh"`
	// Variants are --variants', as MacPorts writes them; EachVariant is
	// --variants each.
	Variants    string `json:"variants,omitempty"`
	EachVariant bool   `json:"each_variant,omitempty"`
	// Targets are the plan's, each with its dependencies in any
	// environment and the environments where it needs Xcode.
	Targets []targetJSON `json:"targets"`
	// Omitted are the changed targets --only left out, which submission
	// still requires.
	Omitted []targetJSON `json:"omitted"`
	// Builds are each environment's own order and dependencies.
	Builds     []buildJSON     `json:"builds"`
	Exclusions []exclusionJSON `json:"exclusions"`
	Unmet      []unmetJSON     `json:"unmet"`
	Unresolved []exclusionJSON `json:"unresolved"`
}

// buildJSON is what a plan builds in one environment, in its order, and
// what each target needs built first there.
type buildJSON struct {
	Environment environmentJSON     `json:"environment"`
	Order       []string            `json:"order"`
	DependsOn   map[string][]string `json:"depends_on,omitempty"`
	// Git are the targets fetched with Git there, and what each one's
	// build must fetch.
	Git map[string]gitSourceJSON `json:"git,omitempty"`
}

// gitSourceJSON is what a Git-fetched target's build is expected to fetch:
// its repository and git.branch, and the commit git.branch named when the
// check was planned, or the abbreviation it begins with, or why neither is
// known.
type gitSourceJSON struct {
	URL          string    `json:"url"`
	Branch       string    `json:"branch,omitempty"`
	Commit       string    `json:"commit,omitempty"`
	Abbreviation string    `json:"abbreviation,omitempty"`
	Unresolved   string    `json:"unresolved,omitempty"`
	ResolvedAt   time.Time `json:"resolved_at"`
}

func gitSourceView(source model.GitSource) gitSourceJSON {
	return gitSourceJSON{URL: source.URL, Branch: source.Ref, Commit: string(source.Commit), Abbreviation: source.Abbreviation, Unresolved: source.Unresolved, ResolvedAt: source.ResolvedAt}
}

// unmetJSON is a target an environment can't build, and what it needs.
type unmetJSON struct {
	Target      string          `json:"target"`
	Environment environmentJSON `json:"environment"`
	Needs       string          `json:"needs"`
	Through     string          `json:"through,omitempty"`
}

type exclusionJSON struct {
	Target string `json:"target"`
	// Platform is the environment the target is excluded in.
	Platform *environmentJSON `json:"platform,omitempty"`
	Reason   string           `json:"reason"`
}

func planView(plan model.Plan) planJSON {
	view := planJSON{ID: string(plan.ID), Tests: string(plan.Tests), Environments: []environmentJSON{}, Only: append([]string{}, plan.Only...), Also: append([]string{}, plan.Also...), Fresh: plan.Fresh,
		Variants: plan.Variants, EachVariant: plan.EachVariant, Targets: []targetJSON{}, Omitted: []targetJSON{}, Exclusions: []exclusionJSON{}, Unmet: []unmetJSON{}, Unresolved: []exclusionJSON{}}
	for _, environment := range plan.Environments {
		view.Environments = append(view.Environments, environmentView(environment))
	}
	for _, target := range plan.Targets {
		view.Targets = append(view.Targets, targetView(plan, target))
	}
	for _, target := range plan.Omitted {
		view.Omitted = append(view.Omitted, targetView(plan, target))
	}
	view.Builds = []buildJSON{}
	for _, planned := range plan.Builds {
		environment := environmentView(planned.Environment)
		build := buildJSON{Environment: environment, Order: []string{}}
		for _, id := range planned.Order {
			build.Order = append(build.Order, string(id))
			for _, dependency := range planned.Dependencies[id] {
				if build.DependsOn == nil {
					build.DependsOn = map[string][]string{}
				}
				build.DependsOn[string(id)] = append(build.DependsOn[string(id)], string(dependency))
			}
			if source, ok := planned.Git[id]; ok {
				if build.Git == nil {
					build.Git = map[string]gitSourceJSON{}
				}
				build.Git[string(id)] = gitSourceView(source)
			}
		}
		view.Builds = append(view.Builds, build)
		for _, exclusion := range planned.Exclusions {
			view.Exclusions = append(view.Exclusions, exclusionJSON{Target: exclusion.Target.Name, Platform: &environment, Reason: exclusion.Reason})
		}
		for _, unmet := range planned.Unmet {
			view.Unmet = append(view.Unmet, unmetJSON{Target: string(unmet.Target), Environment: environment, Needs: string(unmet.Needs), Through: string(unmet.Through)})
		}
	}
	for _, unresolved := range plan.Unresolved {
		view.Unresolved = append(view.Unresolved, exclusionJSON{Target: unresolved.Target.Name, Reason: unresolved.Reason})
	}
	return view
}

func targetView(plan model.Plan, target model.PlanTarget) targetJSON {
	view := targetJSON{Name: string(target.ID), Portfile: target.Target.Portfile, Subport: target.Target.Subport, Kind: string(target.Kind), Role: string(target.Role)}
	for _, environment := range plan.Environments {
		for _, dependency := range plan.DependsOnIn(environment, target.ID) {
			if !slices.Contains(view.DependsOn, string(dependency)) {
				view.DependsOn = append(view.DependsOn, string(dependency))
			}
		}
		if plan.NeedsXcodeIn(environment, target.ID) {
			view.NeedsXcode = append(view.NeedsXcode, environmentView(environment))
		}
	}
	return view
}

// evidenceView is a finished run's targets with their results.
func evidenceView(evidence engine.Evidence) []targetJSON {
	targets := []targetJSON{}
	for _, target := range evidence.Targets {
		view := targetView(evidence.Plan, target.Target)
		passed := target.Passed
		view.Passed = &passed
		for _, result := range target.Outcomes {
			view.Results = append(view.Results, resultJSON{Outcome: string(result.Outcome), Phase: string(result.Phase), Tests: string(result.Tests), Log: result.Log, Detail: result.Detail, ReusedFrom: string(result.ReusedFrom),
				Builders: builderViews(result.Builders),
				Excluded: result.Kind == engine.CellExcluded, Remade: result.Kind == engine.CellRemade})
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
	// Baseline is the baseline check.baseline ran after this check failed.
	Baseline *checkJSON `json:"baseline,omitempty"`
}

// gather takes a check's result, or the baseline's that looks into it.
func (c *checkJSON) gather(step any) {
	if step, ok := step.(checkJSON); ok {
		if c.Run != nil && step.Run != nil && step.Run.BaselineOf == c.Run.ID {
			c.Baseline = &step
			return
		}
		step.Baseline = c.Baseline
		*c = step
	}
}

func (c *checkJSON) result() any { return *c }

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
	Name      string `json:"name"`
	GitBranch string `json:"git_branch"`
	ID        string `json:"id"`
	State     string `json:"state"`
	Worktree  string `json:"worktree"`
	Managed   bool   `json:"managed"`
	Base      string `json:"base"`
	Head      string `json:"head,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
	// Cleaned is a merged branch whose Git branch clean removed.
	Cleaned     bool     `json:"cleaned,omitempty"`
	Commits     int      `json:"commits"`
	Edited      []string `json:"edited"`
	Directories []string `json:"directories"`
	Ports       []string `json:"ports"`
	// Releases are where the branch's updates found their versions.
	Releases    []releaseJSON    `json:"releases"`
	Latest      *latestJSON      `json:"latest_check"`
	Active      []runJSON        `json:"active_checks"`
	PullRequest *pullRequestJSON `json:"pull_request"`
	// Held and Assessment are, for a branch serve prepared, what its files'
	// recorded assessments hold it for, and how far they're recorded:
	// available, incomplete, or pending, which a check or a submission
	// collects.
	Held       []string `json:"held,omitempty"`
	Assessment string   `json:"assessment,omitempty"`
}

func branchView(status engine.BranchStatus) branchJSON {
	branch := status.Branch
	view := branchJSON{Name: branch.ShortName(), GitBranch: branch.Name, ID: string(branch.ID), State: string(branch.State), Worktree: branch.Worktree, Managed: branch.Managed,
		Base: string(branch.Base), Head: status.Head, Missing: status.Missing, Cleaned: status.Cleaned(), Commits: status.Commits,
		Edited: nonNil(status.Edited), Directories: nonNil(status.Scope.Ports), Ports: nonNil(status.Scope.PortNames()), Active: []runJSON{}, Releases: []releaseJSON{},
		Held: status.Held, Assessment: string(status.Assessment)}
	for _, found := range status.Releases {
		release := found.Release
		view.Releases = append(view.Releases, releaseJSON{Port: found.Port, Version: release.Version, Forge: release.Forge, Repository: release.Repository,
			Tag: release.Tag, Commit: release.Commit, Distfiles: release.Archive})
	}
	for _, run := range status.Active {
		active := runView(run)
		active.Stopped = status.Stopped != nil && status.Stopped.ID == run.ID
		view.Active = append(view.Active, active)
	}
	if status.Latest != nil && status.LatestRevision != nil {
		latest := latestJSON{Run: runView(*status.Latest), Revision: revisionView(*status.LatestRevision), Current: status.Current, Targets: []targetJSON{}}
		if status.Evidence != nil {
			latest.Targets = evidenceView(*status.Evidence)
		}
		view.Latest = &latest
	}
	if pr := branch.PullRequest; pr != nil {
		view.PullRequest = &pullRequestJSON{Repository: pr.Repository, Number: pr.Number, URL: github.PullRequestURL(pr.Repository, pr.Number),
			Head: pr.Head, Pushed: string(pr.Pushed), Draft: pr.Draft}
		if observed := pr.Observed; observed != nil {
			at := observed.At
			view.PullRequest.State, view.PullRequest.Review, view.PullRequest.Checks, view.PullRequest.Failing, view.PullRequest.ObservedAt = observed.State, observed.Review, observed.Checks, observed.Failing, &at
		}
	}
	return view
}

// builderJSON is one builder's part of a result.
type builderJSON struct {
	Builder string `json:"builder"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase,omitempty"`
	Tests   string `json:"tests,omitempty"`
	Log     string `json:"log,omitempty"`
}

func builderViews(parts []model.BuilderResult) []builderJSON {
	var views []builderJSON
	for _, part := range parts {
		views = append(views, builderJSON{Builder: part.Builder, Outcome: string(part.Outcome), Phase: string(part.Phase), Tests: string(part.Tests), Log: part.Log})
	}
	return views
}

// releaseJSON is where an update found its version: a forge's tag at an
// upstream commit, or the port's distfiles.
type releaseJSON struct {
	Port       string `json:"port"`
	Version    string `json:"version"`
	Forge      string `json:"forge,omitempty"`
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Distfiles  bool   `json:"distfiles,omitempty"`
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
	// Serve is serve's state as a line, and ServeState as fields.
	Serve      string     `json:"serve,omitempty"`
	ServeState *serveJSON `json:"serve_state,omitempty"`
}

// serveJSON is serve's state as fields, beside its line.
type serveJSON struct {
	Running           bool `json:"running"`
	PID               int  `json:"pid,omitempty"`
	OpensPullRequests bool `json:"opens_pull_requests,omitempty"`
	Queue             int  `json:"queue"`
	Stopped           int  `json:"stopped"`
}

func serveView(s engine.ServeState) serveJSON {
	return serveJSON{Running: s.Running, PID: s.PID, OpensPullRequests: s.OpensPullRequests, Queue: s.Queue, Stopped: s.Stopped}
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

type queueJSON struct {
	Serve      string          `json:"serve"`
	ServeState serveJSON       `json:"serve_state"`
	Runs       []queuedRunJSON `json:"runs"`
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
