package view

import (
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
)

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

func RunView(run model.Run) runJSON {
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

func RevisionView(revision model.Revision) revisionJSON {
	return revisionJSON{ID: string(revision.ID), Kind: string(revision.Kind), Description: engine.Describe(revision), Tree: string(revision.Source.Tree), Base: string(revision.Source.Base), Commit: string(revision.Source.Commit)}
}

type environmentJSON struct {
	Provider       string `json:"provider"`
	OS             string `json:"os,omitempty"`
	Version        string `json:"version,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
	DeveloperTools string `json:"developer_tools,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	// Note is what a plan says of the environment beside it, as of a local
	// prefix other than /opt/local (engine.PrefixNote); a plan's alone.
	Note string `json:"note,omitempty"`
}

func EnvironmentView(environment model.Environment) environmentJSON {
	return environmentJSON{Provider: environment.Provider, OS: environment.Platform.OS, Version: environment.Platform.Version, Architecture: environment.Platform.Architecture,
		DeveloperTools: string(environment.DeveloperTools), Prefix: environment.Prefix}
}

type TargetJSON struct {
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
	// Profile is the build profile that named them, empty where --on did.
	Profile string `json:"profile,omitempty"`
	Tests   string `json:"tests"`
	// Only, Also, Fresh, and Keep are what the person asked: --only's
	// changed ports, --also's unchanged ones, --fresh, and --keep.
	Only  []string `json:"only"`
	Also  []string `json:"also"`
	Fresh bool     `json:"fresh"`
	Keep  bool     `json:"keep"`
	// Variants are --variants', as MacPorts writes them; EachVariant is
	// --variants each.
	Variants    string `json:"variants,omitempty"`
	EachVariant bool   `json:"each_variant,omitempty"`
	// Targets are the plan's, each with its dependencies in any
	// environment and the environments where it needs Xcode.
	Targets []TargetJSON `json:"targets"`
	// Omitted are the changed targets --only left out, which submission
	// still requires.
	Omitted []TargetJSON `json:"omitted"`
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
	Git map[string]GitSourceJSON `json:"git,omitempty"`
	// OmittedGit are the targets --only left out (the plan's omitted)
	// that are fetched with Git there, and the commit each one's tag
	// names as the check is planned: the check doesn't build them, but an
	// earlier check's result of one stands only where it fetched that
	// commit.
	OmittedGit map[string]GitSourceJSON `json:"omitted_git,omitempty"`
	// Tags are the targets there fetched as a GitHub tag's archive, built
	// or left out by --only, and the commit each one's tag named as the
	// check was planned: a later check doesn't reuse a build past a tag
	// that moved, and a submission is held on one.
	Tags map[string]GitSourceJSON `json:"tags,omitempty"`
}

// GitSourceJSON is what a Git-fetched target's build is expected to fetch:
// its repository and git.branch, and the commit git.branch named when the
// check was planned, or the abbreviation it begins with, or why neither is
// known.
type GitSourceJSON struct {
	URL          string    `json:"url"`
	Branch       string    `json:"branch,omitempty"`
	Commit       string    `json:"commit,omitempty"`
	Abbreviation string    `json:"abbreviation,omitempty"`
	Unresolved   string    `json:"unresolved,omitempty"`
	ResolvedAt   time.Time `json:"resolved_at"`
}

func GitSourceView(source model.GitSource) GitSourceJSON {
	return GitSourceJSON{URL: source.URL, Branch: source.Ref, Commit: string(source.Commit), Abbreviation: source.Abbreviation, Unresolved: source.Unresolved, ResolvedAt: source.ResolvedAt}
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

func PlanView(plan model.Plan) planJSON {
	viewed := planJSON{ID: string(plan.ID), Profile: plan.Profile, Tests: string(plan.Tests), Environments: []environmentJSON{}, Only: append([]string{}, plan.Only...), Also: append([]string{}, plan.Also...), Fresh: plan.Fresh, Keep: plan.Keep,
		Variants: plan.Variants, EachVariant: plan.EachVariant, Targets: []TargetJSON{}, Omitted: []TargetJSON{}, Exclusions: []exclusionJSON{}, Unmet: []unmetJSON{}, Unresolved: []exclusionJSON{}}
	for _, environment := range plan.Environments {
		viewed.Environments = append(viewed.Environments, EnvironmentView(environment))
		viewed.Environments[len(viewed.Environments)-1].Note = engine.PrefixNote(environment)
	}
	for _, target := range plan.Targets {
		viewed.Targets = append(viewed.Targets, targetView(plan, target))
	}
	for _, target := range plan.Omitted {
		viewed.Omitted = append(viewed.Omitted, targetView(plan, target))
	}
	viewed.Builds = []buildJSON{}
	for _, planned := range plan.Builds {
		environment := EnvironmentView(planned.Environment)
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
					build.Git = map[string]GitSourceJSON{}
				}
				build.Git[string(id)] = GitSourceView(source)
			}
		}
		for _, target := range plan.Omitted {
			if source, ok := planned.Git[target.ID]; ok {
				if build.OmittedGit == nil {
					build.OmittedGit = map[string]GitSourceJSON{}
				}
				build.OmittedGit[string(target.ID)] = GitSourceView(source)
			}
		}
		for id, tag := range planned.Tags {
			if build.Tags == nil {
				build.Tags = map[string]GitSourceJSON{}
			}
			build.Tags[string(id)] = GitSourceView(tag)
		}
		viewed.Builds = append(viewed.Builds, build)
		for _, exclusion := range planned.Exclusions {
			viewed.Exclusions = append(viewed.Exclusions, exclusionJSON{Target: exclusion.Target.Name, Platform: &environment, Reason: exclusion.Reason})
		}
		for _, unmet := range planned.Unmet {
			viewed.Unmet = append(viewed.Unmet, unmetJSON{Target: string(unmet.Target), Environment: environment, Needs: string(unmet.Needs), Through: string(unmet.Through)})
		}
	}
	for _, unresolved := range plan.Unresolved {
		viewed.Unresolved = append(viewed.Unresolved, exclusionJSON{Target: unresolved.Target.Name, Reason: unresolved.Reason})
	}
	return viewed
}

func targetView(plan model.Plan, target model.PlanTarget) TargetJSON {
	viewed := TargetJSON{Name: string(target.ID), Portfile: target.Target.Portfile, Subport: target.Target.Subport, Kind: string(target.Kind), Role: string(target.Role)}
	for _, environment := range plan.Environments {
		for _, dependency := range plan.DependsOnIn(environment, target.ID) {
			if !slices.Contains(viewed.DependsOn, string(dependency)) {
				viewed.DependsOn = append(viewed.DependsOn, string(dependency))
			}
		}
		if plan.NeedsXcodeIn(environment, target.ID) {
			viewed.NeedsXcode = append(viewed.NeedsXcode, EnvironmentView(environment))
		}
	}
	return viewed
}

// EvidenceView is a finished run's targets with their results.
func EvidenceView(evidence engine.Evidence) []TargetJSON {
	targets := []TargetJSON{}
	for _, target := range evidence.Targets {
		viewed := targetView(evidence.Plan, target.Target)
		passed := target.Passed
		viewed.Passed = &passed
		for _, result := range target.Outcomes {
			viewed.Results = append(viewed.Results, resultJSON{Outcome: string(result.Outcome), Phase: string(result.Phase), Tests: string(result.Tests), Log: logWhere(result.Log), Detail: result.Detail, ReusedFrom: string(result.ReusedFrom),
				Builders: builderViews(result.Builders),
				Excluded: result.Kind == engine.CellExcluded, Remade: result.Kind == engine.CellRemade})
		}
		targets = append(targets, viewed)
	}
	return targets
}

// CheckJSON is check's, wait's, and retry's result.
type CheckJSON struct {
	Branch   string        `json:"branch"`
	Revision *revisionJSON `json:"revision,omitempty"`
	Plan     *planJSON     `json:"plan,omitempty"`
	Run      *runJSON      `json:"run"`
	// Targets are the finished run's results; empty until it finishes.
	Targets []TargetJSON `json:"targets"`
	// Baseline is the baseline check.baseline ran after this check failed.
	Baseline *CheckJSON `json:"baseline,omitempty"`
	// Altered are the kept archives found changed since they were kept,
	// which no guest was given, each said as check says it.
	Altered []string `json:"altered_archives,omitempty"`
	// Lint, Links, and Installed are what the finished check's report
	// says beside its results, each line as it says it: what lint warned
	// of, what its builds link and don't declare, and what they installed
	// against the port's earlier build.
	Lint      []linkLineJSON `json:"lint,omitempty"`
	Links     []linkLineJSON `json:"links,omitempty"`
	Installed []linkLineJSON `json:"installed,omitempty"`
}

// Gather takes a check's result, or the baseline's that looks into it.
func (c *CheckJSON) Gather(step any) {
	if step, ok := step.(CheckJSON); ok {
		if c.Run != nil && step.Run != nil && step.Run.BaselineOf == c.Run.ID {
			c.Baseline = &step
			return
		}
		step.Baseline = c.Baseline
		*c = step
	}
}

func (c *CheckJSON) Result() any { return *c }

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
	// Current is true when it checked exactly the files as they are now,
	// with Included, the untracked files it took in with --include, as
	// they are on disk now.
	Current  bool         `json:"current"`
	Included []string     `json:"included,omitempty"`
	Targets  []TargetJSON `json:"targets"`
}

type BranchJSON struct {
	Name      string `json:"name"`
	GitBranch string `json:"git_branch"`
	ID        string `json:"id"`
	State     string `json:"state"`
	Worktree  string `json:"worktree"`
	Managed   bool   `json:"managed"`
	Base      string `json:"base"`
	Head      string `json:"head,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
	// Renamed is the name a branch had before a rename by hand this
	// status followed.
	Renamed string `json:"renamed_from,omitempty"`
	// Cleaned is a merged branch whose Git branch clean removed.
	Cleaned     bool     `json:"cleaned,omitempty"`
	Commits     int      `json:"commits"`
	Edited      []string `json:"edited"`
	Directories []string `json:"directories"`
	Ports       []string `json:"ports"`
	// Changed are the ports the branch changes, each directory's changed
	// subports where its change record says, and Notes say why a port is
	// its directory's, read from the text instead.
	Changed []string `json:"changed_ports"`
	// StartedFor are the ports it was started for, which it may not
	// change yet.
	StartedFor []string          `json:"started_for,omitempty"`
	Notes      map[string]string `json:"port_notes,omitempty"`
	// Releases are where the branch's updates found their versions.
	Releases []releaseJSON `json:"releases"`
	// Standing is what the branch's checks establish of its files as they
	// are now (branchstate.Standing), the one verdict status words, where
	// the JSON gave the runs and left it to be judged again (R3-34).
	Standing string `json:"check_standing"`
	// Latest is the branch's newest finished check, null until one has
	// finished: a check still running is among Active, and isn't the
	// latest until it ends, where null read as no check while one ran.
	Latest      *latestJSON      `json:"latest_check"`
	Active      []runJSON        `json:"active_checks"`
	PullRequest *pullRequestJSON `json:"pull_request"`
	// Held and Assessment are, for a branch serve prepared, what its files'
	// recorded assessments hold it for, and how far they're recorded:
	// available, incomplete, or pending, which a check or a submission
	// collects.
	Held       []string `json:"held,omitempty"`
	Assessment string   `json:"assessment,omitempty"`
	// HeldConcerns are Held, typed, each with where it came from and the
	// rule that raised it, beside held's words.
	HeldConcerns []ConcernJSON `json:"concerns,omitempty"`
	// HandEdits are the branch's hand edits, each its directory, the
	// refusal dockhand left it for, and whether the attempt was adjusted.
	HandEdits []handEditJSON `json:"hand_edits,omitempty"`
	// Detail is what status's text details of a branch shown alone.
	Detail *branchDetailJSON `json:"detail,omitempty"`
	// Moved are the Git-fetched ports whose release's tag named another
	// commit when the update chose it than when the latest check, of the
	// files as they are now, planned it (rule release-moved), which hold a
	// submission nobody looks over. Whether a tag names another commit now
	// (source-moved) takes the network, which status never reads.
	Moved []ConcernJSON `json:"moved,omitempty"`
	// OnMaster is the master dockhand last fetched where it has every
	// change the branch's files make: its work landed by another route.
	OnMaster string `json:"on_master,omitempty"`
}

// ConcernJSON is what a submission nobody looks over waits on a person's
// look for: where it came from, upstream, the commit rules, or another
// pull request, where submit gives it; the port, the rule that raised it,
// what it's about, such as the commit the check planned, how it stands
// against the base, and its words.
type ConcernJSON struct {
	Origin  string `json:"origin,omitempty"`
	Port    string `json:"port"`
	Rule    string `json:"rule"`
	Source  string `json:"source,omitempty"`
	Path    string `json:"path,omitempty"`
	Subject string `json:"subject,omitempty"`
	Class   string `json:"class,omitempty"`
	Detail  string `json:"detail"`
}

// concernsView is concerns as JSON, an empty list for none.
func concernsView(concerns []model.Concern) []ConcernJSON {
	viewed := []ConcernJSON{}
	for _, concern := range concerns {
		viewed = append(viewed, ConcernJSON{Origin: string(concern.Origin), Port: concern.Port, Rule: concern.Rule, Source: concern.Source, Path: concern.Path, Subject: concern.Subject,
			Class: string(concern.Class), Detail: concern.Detail})
	}
	return viewed
}

func BranchView(status engine.BranchStatus) BranchJSON {
	branch := status.Branch
	viewed := BranchJSON{Name: branch.ShortName(), GitBranch: branch.Name, ID: string(branch.ID), State: string(branch.State), Worktree: branch.Worktree, Managed: branch.Managed,
		Base: string(branch.Base), Head: status.Head, Missing: status.Missing, Renamed: status.Renamed, Cleaned: status.Cleaned(), Commits: status.Commits,
		Edited: NonNil(status.Edited), Directories: NonNil(status.Scope.Ports), Ports: NonNil(status.Scope.PortNames()), Active: []runJSON{}, Releases: []releaseJSON{},
		Held: engine.HoldLines(status.Held), Assessment: string(status.Assessment), OnMaster: string(status.OnMaster), StartedFor: branch.Ports,
		Standing: status.Check().Standing().String()}
	if len(status.Held) > 0 {
		viewed.HeldConcerns = concernsView(status.Held)
	}
	for _, hand := range status.HandEdits {
		viewed.HandEdits = append(viewed.HandEdits, handEditJSON{Directory: hand.Directory, Refused: hand.Words, Adjusted: hand.Adjusted})
	}
	viewed.Changed, viewed.Notes = status.ChangedPorts()
	viewed.Changed = NonNil(viewed.Changed)
	if len(viewed.Notes) == 0 {
		viewed.Notes = nil
	}
	for _, found := range status.Releases {
		release := found.Release
		viewed.Releases = append(viewed.Releases, releaseJSON{Port: found.Port, Version: release.Version, Forge: release.Forge, Repository: release.Repository,
			Tag: release.Tag, Commit: release.Commit, Distfiles: release.Archive})
	}
	for _, moved := range status.Moved {
		viewed.Moved = append(viewed.Moved, ConcernJSON{Port: moved.Port, Rule: moved.Rule, Subject: moved.Subject, Detail: moved.Detail})
	}
	for _, run := range status.Active {
		active := RunView(run)
		active.Stopped = status.Stopped != nil && status.Stopped.ID == run.ID
		viewed.Active = append(viewed.Active, active)
	}
	if status.Latest != nil && status.LatestRevision != nil {
		latest := latestJSON{Run: RunView(*status.Latest), Revision: RevisionView(*status.LatestRevision), Current: status.Current, Included: status.Included, Targets: []TargetJSON{}}
		if status.Evidence != nil {
			latest.Targets = EvidenceView(*status.Evidence)
		}
		viewed.Latest = &latest
	}
	if pr := branch.PullRequest; pr != nil {
		viewed.PullRequest = &pullRequestJSON{Repository: pr.Repository, Number: pr.Number, URL: github.PullRequestURL(pr.Repository, pr.Number),
			Head: pr.Head, Pushed: string(pr.Pushed), Draft: pr.Draft}
		if observed := pr.Observed; observed != nil {
			at := observed.At
			viewed.PullRequest.State, viewed.PullRequest.Review, viewed.PullRequest.Checks, viewed.PullRequest.Failing, viewed.PullRequest.ObservedAt = string(observed.State), string(observed.Review), string(observed.Checks), observed.Failing, &at
		}
	}
	return viewed
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
		views = append(views, builderJSON{Builder: part.Builder, Outcome: string(part.Outcome), Phase: string(part.Phase), Tests: string(part.Tests), Log: logWhere(part.Log)})
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

func AttentionView(rows []Attention) []attentionJSON {
	views := []attentionJSON{}
	for _, row := range rows {
		views = append(views, attentionJSON{Kind: string(row.Kind), Branch: row.Branch, What: row.What, Next: row.Next})
	}
	return views
}

// branchDetailJSON is what status's text says of a branch shown alone
// beside its row, as its text says it, each line its mark and words: the
// person's decisions, a maintainer's instructions in the comments, what
// lint warned of, what the latest check's builds link and don't declare,
// and what they installed and configure found against the earlier build.
// The JSON said only the row, where the text said each (Codex's review of
// 8e314b96, finding 9).
type branchDetailJSON struct {
	Decided      []string       `json:"decided"`
	Instructions []string       `json:"instructions"`
	Lint         []linkLineJSON `json:"lint"`
	Links        []linkLineJSON `json:"links"`
	Installed    []linkLineJSON `json:"installed"`
}

// linkLineJSON is a detail line: its mark, ✓, !, or ✗, and its words.
type linkLineJSON struct {
	Mark  string `json:"mark"`
	Words string `json:"words"`
}

// BranchDetail is what status's text details of a branch shown alone, from
// the report the text reads too.
func BranchDetail(status engine.BranchStatus, report engine.StatusReport) *branchDetailJSON {
	detail := &branchDetailJSON{Decided: []string{}, Instructions: NonNil(report.Instructions[status.Branch.ID]), Lint: []linkLineJSON{}, Links: []linkLineJSON{}, Installed: []linkLineJSON{}}
	for _, decision := range status.Decided {
		detail.Decided = append(detail.Decided, engine.DecisionWords(decision))
	}
	if status.Latest != nil {
		detail.Lint = DetailLines(LintLines(report.Lint[status.Latest.ID]))
		detail.Links = DetailLines(LinkLines(report.Links[status.Latest.ID]))
		detail.Installed = DetailLines(InstalledLines(report.Installed[status.Latest.ID]))
	}
	return detail
}

// DetailLines are a report's lines as JSON: each its mark and words.
func DetailLines(from []LinkLine) []linkLineJSON {
	out := []linkLineJSON{}
	for _, line := range from {
		out = append(out, linkLineJSON(line))
	}
	return out
}

type StatusJSON struct {
	Attention []attentionJSON `json:"attention"`
	Branches  []BranchJSON    `json:"branches,omitempty"`
	// Serve is serve's state as a line, and ServeState as fields.
	Serve      string     `json:"serve,omitempty"`
	ServeState *serveJSON `json:"serve_state,omitempty"`
	// Queue is the checks queued and running, oldest first, beside the
	// branches: what queue listed.
	Queue *[]queuedRunJSON `json:"queue,omitempty"`
	// Processes are the dockhand processes working on the checkout.
	Processes *[]processJSON `json:"processes,omitempty"`
	// Changed are what checks are building in of the person's own, and
	// what checks left changed (engine.Changes).
	Changed []changeJSON `json:"changed,omitempty"`
}

// changeJSON is something of the person's a check changed: a MacPorts
// prefix on this Mac, the check building there now, or how to put back
// what it left changed.
type changeJSON struct {
	Check    string `json:"check"`
	Place    string `json:"place"`
	Building bool   `json:"building,omitempty"`
	Remedy   string `json:"remedy,omitempty"`
	// Holder is who holds the prefix, "check-12 (pid 4711)".
	Holder string `json:"holder,omitempty"`
}

// ChangesView is engine.Changes as fields.
func ChangesView(changes []buildenv.Change) []changeJSON {
	var viewed []changeJSON
	for _, change := range changes {
		viewed = append(viewed, changeJSON{Check: change.Check, Place: change.Place, Building: change.Building, Remedy: change.Remedy, Holder: change.Holder})
	}
	return viewed
}

// ChangeWords say something of the person's a check changed, as status
// says it: a check building in a prefix now, whose put-back undoes an
// upgrade made meanwhile, or one a check left changed, with how to put it
// back.
func ChangeWords(change buildenv.Change) string {
	if change.Building {
		// Who holds the prefix, as a check waiting on it says.
		held := ""
		if change.Holder != "" {
			held = ", held by " + change.Holder
		}
		return fmt.Sprintf("%s is building in %s%s. Your port commands targeting this prefix must wait; upgrades made there in the meantime will be undone when the prefix is restored.", change.Check, change.Place, held)
	}
	return fmt.Sprintf("%s stopped before it could restore %s. Run %s to restore it, or let the next check there restore it.", change.Check, change.Place, change.Remedy)
}

// processJSON is a dockhand process working on the checkout.
type processJSON struct {
	PID     int      `json:"pid"`
	Kind    string   `json:"kind"`
	Build   string   `json:"build"`
	Started string   `json:"started"`
	Leads   bool     `json:"leads"`
	Runs    []string `json:"runs"`
	// Expects is the schema it expects where the database is at another,
	// Database.
	Expects  int `json:"expects_schema,omitempty"`
	Database int `json:"database_schema,omitempty"`
}

func ProcessesView(processes []engine.Process) *[]processJSON {
	views := []processJSON{}
	for _, process := range processes {
		viewed := processJSON{PID: process.Session.PID, Kind: string(process.Session.Kind), Build: process.Session.Version, Started: process.Session.StartedAt.UTC().Format(time.RFC3339),
			Leads: process.Leads, Runs: []string{}, Expects: process.Expects, Database: process.Database}
		for _, run := range process.Runs {
			viewed.Runs = append(viewed.Runs, run.Name())
		}
		views = append(views, viewed)
	}
	return &views
}

// serveJSON is serve's state as fields, beside its line.
type serveJSON struct {
	Running           bool   `json:"running"`
	PID               int    `json:"pid,omitempty"`
	OpensPullRequests bool   `json:"opens_pull_requests,omitempty"`
	Build             string `json:"build,omitempty"`
	NotActing         string `json:"not_acting,omitempty"`
	LoginProblem      string `json:"login_problem,omitempty"`
	Queue             int    `json:"queue"`
	Stopped           int    `json:"stopped"`
}

func ServeView(s engine.ServeState) serveJSON {
	return serveJSON{Running: s.Running, PID: s.PID, OpensPullRequests: s.OpensPullRequests, Build: s.Build, NotActing: s.NotActing, LoginProblem: s.LoginProblem, Queue: s.Queue, Stopped: s.Stopped}
}

func NonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

type queuedRunJSON struct {
	runJSON
	// Progress is a running check's newest progress line; absent for one
	// queued.
	Progress     string            `json:"progress,omitempty"`
	Branch       string            `json:"branch"`
	Revision     string            `json:"revision"`
	Environments []environmentJSON `json:"environments"`
	// VMs are the Tart VMs a running check runs in, and TartHome the home
	// that reaches them; absent when it runs in none.
	VMs      []string `json:"vms,omitempty"`
	TartHome string   `json:"tart_home,omitempty"`
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

func DiffView(diff engine.BranchDiff, patch bool) diffJSON {
	viewed := diffJSON{Branch: diff.Status.Branch.ShortName(), Base: string(diff.Status.Branch.Base), Edited: NonNil(diff.Status.Edited), Other: NonNil(diff.Other), Files: NonNil(diff.Files)}
	viewed.Ports = []portChangeJSON{}
	for _, port := range diff.Ports {
		viewed.Ports = append(viewed.Ports, portChangeJSON{Directory: port.Directory, Change: string(port.Change())})
	}
	if patch {
		viewed.Patch = string(diff.Patch)
	}
	return viewed
}

type impactJSON struct {
	Branch  string   `json:"branch"`
	Changed []string `json:"changed_directories"`
	// Defined are the ports each changed directory defines at the base,
	// as its index names them, devel/libuv's libuv and libuv-devel; absent
	// where it couldn't be read.
	Defined         map[string][]string `json:"defined,omitempty"`
	LookedFor       []string            `json:"dependents_of"`
	Dependents      []dependentJSON     `json:"dependents"`
	DependentsError string              `json:"dependents_error,omitempty"`
	Shared          []sharedJSON        `json:"shared_files"`
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

func ImpactView(impact engine.Impact) impactJSON {
	viewed := impactJSON{Branch: impact.Diff.Status.Branch.ShortName(), Changed: NonNil(impact.Diff.Status.Scope.Ports), Defined: impact.Defined, LookedFor: NonNil(impact.Of),
		Dependents: []dependentJSON{}, DependentsError: impact.Unread, Shared: []sharedJSON{}}
	for _, dependent := range impact.Dependents {
		viewed.Dependents = append(viewed.Dependents, dependentJSON{Name: dependent.Name, Directory: dependent.Directory, On: NonNil(dependent.On), Phases: NonNil(dependent.Phases)})
	}
	for _, shared := range impact.Shared {
		viewed.Shared = append(viewed.Shared, sharedJSON{Path: shared.Path, PortGroup: shared.PortGroup, Users: NonNil(shared.Users)})
	}
	return viewed
}

// logWhere is where a log is now, kept compressed or as written; empty for
// none.
func logWhere(path string) string {
	if path == "" {
		return ""
	}
	return buildlog.Where(path)
}

// handEditJSON is a hand edit (engine.HandEdit).
type handEditJSON struct {
	Directory string `json:"directory"`
	Refused   string `json:"refused"`
	Adjusted  bool   `json:"adjusted"`
}
