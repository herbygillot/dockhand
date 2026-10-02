package command

import (
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
)

// The --json results of the commands that change things. Each says what
// was done, or with a plan or preview, what would be.

type branchRefJSON struct {
	Name      string `json:"name"`
	GitBranch string `json:"git_branch"`
	Worktree  string `json:"worktree"`
	Base      string `json:"base"`
}

func branchRef(branch model.Branch) branchRefJSON {
	return branchRefJSON{Name: branch.ShortName(), GitBranch: branch.Name, Worktree: branch.Worktree, Base: string(branch.Base)}
}

type versionJSON struct {
	Version  string `json:"version"`
	Revision int    `json:"revision"`
}

type updateJSON struct {
	Branch    branchRefJSON `json:"branch"`
	Started   bool          `json:"started"`
	Port      string        `json:"port"`
	Before    versionJSON   `json:"before"`
	After     versionJSON   `json:"after"`
	Current   bool          `json:"current"`
	Applied   bool          `json:"applied"`
	Files     []string      `json:"files"`
	Distfiles int           `json:"distfiles"`
	Subject   string        `json:"subject"`
	Patches   []string      `json:"patch_problems"`
	// PatchesApplied counts the port's patches checked that apply.
	PatchesApplied int `json:"patches_applied"`
	// Unchecked are the patches no check reached before the build, which
	// applies them.
	Unchecked []string `json:"unchecked_patches"`
	Diff      string   `json:"diff,omitempty"`
	// Revbumped are the dependents --revbump-dependents bumped, or would.
	Revbumped []string `json:"revbumped,omitempty"`
	// Stealth is a checksum refresh's stealth update.
	Stealth *stealthJSON `json:"stealth,omitempty"`
	// Upstream is what comparing the old and new upstream archives found,
	// as update prints it; absent when they weren't compared.
	Upstream *upstreamJSON `json:"upstream,omitempty"`
	// Others are the port's other open pull requests, where the update
	// looked, and OthersProblem why it couldn't.
	Regenerated   []regeneratedJSON `json:"regenerated,omitempty"`
	Others        []otherJSON       `json:"others,omitempty"`
	OthersProblem string            `json:"others_problem,omitempty"`
	// PlainHTTP are the port's URLs over plain HTTP, and whether each
	// answers over HTTPS.
	PlainHTTP []plainHTTPJSON `json:"plain_http,omitempty"`
	// Tidy, Check, and Submit are the steps update --submit and bump go on
	// to, each as tidy, check, and submit report it, as far as they went.
	Tidy   *tidyJSON   `json:"tidy,omitempty"`
	Check  *checkJSON  `json:"check,omitempty"`
	Submit *submitJSON `json:"submit,omitempty"`
}

// gather puts one step's result in an update's that went on.
func (u *updateJSON) gather(step any) {
	switch step := step.(type) {
	case updateJSON:
		step.Tidy, step.Check, step.Submit = u.Tidy, u.Check, u.Submit
		*u = step
	case tidyJSON:
		u.Tidy = &step
	case checkJSON:
		u.Check = &step
	case submitJSON:
		u.Submit = &step
	}
}

func (u *updateJSON) result() any { return *u }

type upstreamJSON struct {
	Changes []upstreamChangeJSON `json:"changes"`
	// Problem says why the archives could not be compared.
	Problem string `json:"problem,omitempty"`
	// Held is whether the comparison holds the update for a person's look
	// before serve or bump submits it: a change a build can't catch, or
	// archives it couldn't compare.
	Held bool `json:"held"`
	// Coverage is what the comparison set apart, and why.
	Coverage []model.Coverage `json:"coverage,omitempty"`
}

// upstreamView is a comparison under the JSON's upstream key, whose
// changes are worded as under the text's Upstream heading.
func upstreamView(comparison model.UpstreamComparison) upstreamJSON {
	view := upstreamJSON{Changes: []upstreamChangeJSON{}, Problem: comparison.Problem, Held: comparison.Held(), Coverage: comparison.Coverage}
	for _, change := range comparison.Changes {
		view.Changes = append(view.Changes, upstreamChangeJSON{Kind: change.Kind, Path: change.Path, Message: underUpstream(change).Message, Hold: change.Hold,
			Rule: change.Rule, Subject: change.Subject, Class: string(change.Class)})
	}
	return view
}

// portUpstreamJSON is what comparing a port's upstream archives found
// when the branch updated it.
type portUpstreamJSON struct {
	Port string `json:"port"`
	upstreamJSON
}

type upstreamChangeJSON struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Hold    bool   `json:"hold"`
	// Rule, with the path and subject, is what identifies it; class is
	// how the candidate stands against its base on it.
	Rule    string `json:"rule,omitempty"`
	Subject string `json:"subject,omitempty"`
	Class   string `json:"class,omitempty"`
}

type stealthJSON struct {
	Distfiles      []stealthDistfileJSON `json:"distfiles"`
	Revbumped      bool                  `json:"revbumped"`
	RevbumpProblem string                `json:"revbump_problem,omitempty"`
	DistSubdir     string                `json:"dist_subdir,omitempty"`
	Problem        string                `json:"problem,omitempty"`
}

type stealthDistfileJSON struct {
	Name string       `json:"name"`
	Was  checksumJSON `json:"was"`
	Now  checksumJSON `json:"now"`
}

type checksumJSON struct {
	RMD160 string `json:"rmd160,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// regeneratedJSON is a dependency block an update wrote again: its
// entries, and how many aren't as they were.
type regeneratedJSON struct {
	Option  string `json:"option"`
	Count   int    `json:"count"`
	Changed int    `json:"changed"`
}

// otherJSON is another open pull request for a port.
type otherJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url,omitempty"`
}

func updateView(branch model.Branch, started bool, update engine.Update, plan bool) updateJSON {
	view := updateJSON{Branch: branchRef(branch), Started: started, Port: update.Port,
		Before: versionJSON{update.Before.Version, update.Before.Revision}, After: versionJSON{update.After.Version, update.After.Revision},
		Current: update.Current, Applied: update.Applied, Files: nonNil(update.Files), Distfiles: update.Distfiles, Subject: update.Subject, Patches: nonNil(update.PatchProblems), PatchesApplied: update.PatchesApplied,
		Unchecked: nonNil(update.PatchesUnchecked)}
	if plan {
		view.Diff = update.Diff
	}
	if upstream := update.Upstream; upstream != nil {
		comparison := upstreamView(*upstream)
		view.Upstream = &comparison
	}
	for _, block := range update.Regenerated {
		view.Regenerated = append(view.Regenerated, regeneratedJSON{Option: block.Option, Count: block.Count, Changed: block.Changed})
	}
	for _, pr := range update.Others {
		view.Others = append(view.Others, otherJSON{Number: pr.Number, Title: pr.Title, URL: pr.URL})
	}
	view.OthersProblem = update.OthersProblem
	for _, url := range update.PlainHTTP {
		view.PlainHTTP = append(view.PlainHTTP, plainHTTPJSON{Option: url.Option, URL: url.URL, HTTPS: url.HTTPS, Answers: url.Answers})
	}
	if stealth := update.Stealth; stealth != nil {
		view.Stealth = &stealthJSON{Revbumped: stealth.Revbumped, RevbumpProblem: stealth.RevbumpProblem, DistSubdir: stealth.DistSubdir, Problem: stealth.Problem, Distfiles: []stealthDistfileJSON{}}
		for _, d := range stealth.Distfiles {
			view.Stealth.Distfiles = append(view.Stealth.Distfiles, stealthDistfileJSON{Name: d.Name,
				Was: checksumJSON{d.Was.RMD160, d.Was.SHA256, d.Was.Size}, Now: checksumJSON{d.Now.RMD160, d.Now.SHA256, d.Now.Size}})
		}
	}
	return view
}

type revbumpJSON struct {
	Branch  branchRefJSON   `json:"branch"`
	Started bool            `json:"started"`
	Subject string          `json:"subject"`
	Applied bool            `json:"applied"`
	Ports   []revbumpedJSON `json:"ports"`
}

type revbumpedJSON struct {
	Port   string `json:"port"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

type findingJSON struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Commit   string `json:"commit,omitempty"`
	Where    string `json:"where,omitempty"`
	Message  string `json:"message"`
}

func findingsView(findings []commitrules.Finding) []findingJSON {
	views := []findingJSON{}
	for _, f := range findings {
		views = append(views, findingJSON{Code: f.Code, Severity: string(f.Severity), Commit: f.Commit, Where: f.Where, Message: f.Message})
	}
	return views
}

type tidyCommitJSON struct {
	Subject   string   `json:"subject"`
	Message   string   `json:"message"`
	Paths     []string `json:"paths"`
	Ports     []string `json:"ports"`
	Combines  []string `json:"combines"`
	Working   bool     `json:"includes_uncommitted_edits"`
	Author    string   `json:"author"`
	FromEdits bool     `json:"from_dockhand_edits"`
	// Created is a new port create wrote, edited by hand since.
	Created  bool     `json:"created,omitempty"`
	Notes    []string `json:"notes"`
	Blocking []string `json:"blocking"`
}

type tidyJSON struct {
	Branch      string           `json:"branch"`
	Keep        bool             `json:"keep"`
	Unambiguous bool             `json:"unambiguous"`
	Commits     []tidyCommitJSON `json:"commits"`
	Findings    []findingJSON    `json:"findings"`
	// Warnings are what MacPorts' commit rules warn of in commits it keeps.
	Warnings []findingJSON `json:"warnings"`
	// Applied is what applying made, when it did.
	Applied *tidyAppliedJSON `json:"applied"`
}

type tidyAppliedJSON struct {
	Checkpoint string   `json:"checkpoint"`
	Commits    []string `json:"commits"`
	// Kept is how many of the leading commits are the branch's own, as
	// they were.
	Kept int `json:"kept,omitempty"`
}

func tidyView(plan engine.TidyPlan) tidyJSON {
	view := tidyJSON{Branch: plan.Branch.ShortName(), Keep: plan.Keep, Unambiguous: plan.Unambiguous(), Commits: []tidyCommitJSON{}, Findings: findingsView(plan.Findings), Warnings: findingsView(plan.Warnings)}
	for _, group := range plan.Groups {
		commit := tidyCommitJSON{Subject: group.Subject(), Message: group.Message, Paths: nonNil(group.Paths), Ports: nonNil(group.Ports), Combines: []string{},
			Working: group.Working, FromEdits: group.FromEdits, Created: group.Created, Notes: nonNil(group.Notes), Blocking: nonNil(group.Blocking)}
		if group.Author.Name != "" {
			commit.Author = group.Author.Name + " <" + group.Author.Email + ">"
		}
		for _, combined := range group.Combines {
			commit.Combines = append(commit.Combines, combined.ID)
		}
		view.Commits = append(view.Commits, commit)
	}
	return view
}

type submitJSON struct {
	Branch      string         `json:"branch"`
	Title       string         `json:"title"`
	Commit      string         `json:"commit"`
	Commits     int            `json:"commits"`
	From        string         `json:"from"`
	To          string         `json:"to"`
	Push        string         `json:"push"`
	Checks      string         `json:"checks"`
	Findings    []findingJSON  `json:"findings"`
	Blocking    []string       `json:"blocking"`
	LeftOut     []string       `json:"left_out"`
	Body        string         `json:"body"`
	PullRequest *submittedJSON `json:"pull_request"`
	// Upstream is what comparing the upstream archives found for each of
	// the branch's updates that compared them.
	Upstream []portUpstreamJSON `json:"upstream"`
	// ModifiedBuilds are the commits whose Generated-By names a dockhand
	// built from uncommitted source.
	ModifiedBuilds []string `json:"modified_builds"`
	// UnfoundBuilds are the other builds the commits' Generated-By name
	// that nobody else can find: dockhand's repository on GitHub doesn't
	// have what each was built from, or it recorded nothing. BuildsProblem
	// says why GitHub couldn't be asked.
	UnfoundBuilds []unfoundBuildJSON `json:"unfound_builds"`
	BuildsProblem string             `json:"builds_problem,omitempty"`
	// Held are why a submission nobody looked over, bump's, waits for a
	// person's look.
	Held []string `json:"held,omitempty"`
	// Check is the check submit --check ran, as far as it went.
	Check *checkJSON `json:"check,omitempty"`
}

// gather takes the submission's result, or its check's.
func (s *submitJSON) gather(step any) {
	switch step := step.(type) {
	case submitJSON:
		step.Check = s.Check
		*s = step
	case checkJSON:
		s.Check = &step
	}
}

func (s *submitJSON) result() any { return *s }

// unfoundBuildJSON is a build the commits name in Generated-By that
// nobody else can find.
type unfoundBuildJSON struct {
	Build   string   `json:"build"`
	Commits []string `json:"commits"`
	// Commit is the dockhand commit GitHub was asked for, in full or as
	// the build abbreviates it, and Release the tag; neither, for a build
	// that recorded no commit.
	Commit  string `json:"commit,omitempty"`
	Release string `json:"release,omitempty"`
}

type submittedJSON struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Created bool   `json:"created"`
	Pushed  bool   `json:"pushed"`
	Draft   bool   `json:"draft"`
}

func submitView(plan engine.SubmitPlan) submitJSON {
	view := submitJSON{Branch: plan.Branch.ShortName(), Title: plan.Title, Commit: plan.Commit, Commits: len(plan.Commits), From: plan.Head(), To: plan.Repository + ":" + engine.UpstreamBranch,
		Push: pushWords(plan), Checks: checkWords(plan), Findings: findingsView(plan.Findings), Blocking: nonNil(plan.Blocking), LeftOut: nonNil(plan.LeftOut), Body: plan.Body,
		Upstream: []portUpstreamJSON{}, ModifiedBuilds: nonNil(plan.ModifiedBuilds), UnfoundBuilds: []unfoundBuildJSON{}, BuildsProblem: plan.BuildsProblem}
	for _, found := range plan.Upstream {
		view.Upstream = append(view.Upstream, portUpstreamJSON{Port: found.Port, upstreamJSON: upstreamView(found.Comparison)})
	}
	for _, build := range plan.UnfoundBuilds {
		view.UnfoundBuilds = append(view.UnfoundBuilds, unfoundBuildJSON{Build: build.Build, Commits: build.Commits, Commit: build.Source.Commit, Release: build.Source.Release})
	}
	return view
}

type cleanStepJSON struct {
	What    string `json:"what"`
	Why     string `json:"why,omitempty"`
	Kept    string `json:"kept,omitempty"`
	Removed bool   `json:"removed"`
}

type cleanBranchJSON struct {
	Branch string          `json:"branch"`
	Merged string          `json:"merged_at"`
	Steps  []cleanStepJSON `json:"steps"`
	// Superseded says what master has of an unmerged branch's ports, for a
	// look before git branch -D removes it.
	Superseded string `json:"superseded,omitempty"`
}

type leftoverJSON struct {
	Provider string `json:"provider"`
	Ref      string `json:"ref"`
	What     string `json:"what"`
	Check    string `json:"check,omitempty"`
	Kept     string `json:"kept,omitempty"`
	Removed  bool   `json:"removed"`
}

func leftoversView(leftovers []engine.Leftover) []leftoverJSON {
	views := []leftoverJSON{}
	for _, leftover := range leftovers {
		view := leftoverJSON{Provider: leftover.Provider, Ref: leftover.Ref, What: leftover.What, Kept: leftover.Kept, Removed: leftover.Done}
		if leftover.Run != nil {
			view.Check = leftover.Run.Name()
		}
		views = append(views, view)
	}
	return views
}

func cleanView(plans []engine.CleanBranch) []cleanBranchJSON {
	views := []cleanBranchJSON{}
	for _, plan := range plans {
		view := cleanBranchJSON{Branch: plan.Branch.ShortName(), Merged: string(plan.Merged), Steps: []cleanStepJSON{}, Superseded: plan.Superseded}
		for _, step := range plan.Steps {
			view.Steps = append(view.Steps, cleanStepJSON{What: step.What, Why: step.Why, Kept: step.Kept, Removed: step.Done})
		}
		views = append(views, view)
	}
	return views
}

type reviewJSON struct {
	Number     int           `json:"number"`
	Title      string        `json:"title"`
	URL        string        `json:"url"`
	Head       string        `json:"head"`
	Commits    int           `json:"commits"`
	Ports      []string      `json:"ports"`
	Permission string        `json:"permission"`
	Summary    string        `json:"summary"`
	Findings   []findingJSON `json:"findings"`
	Resolved   []string      `json:"resolved"`
	Body       string        `json:"body"`
	Posted     string        `json:"posted"`
	PostedURL  string        `json:"posted_url,omitempty"`
}

func reviewView(report engine.ReviewReport) reviewJSON {
	view := reviewJSON{Number: report.Ref.Number, Title: report.Title, URL: report.Ref.URL, Head: report.Head, Commits: len(report.Commits), Ports: nonNil(report.Ports),
		Permission: report.Permission, Summary: report.Summary(), Findings: findingsView(report.Findings), Resolved: []string{}, Body: report.Markdown()}
	for _, finding := range report.Resolved {
		view.Resolved = append(view.Resolved, finding.Message+" ["+finding.Code+"]")
	}
	return view
}

type logsJSON struct {
	Run        runJSON             `json:"run"`
	Executions []executionLogsJSON `json:"executions"`
}

type executionLogsJSON struct {
	// ID is the provider run's, tart_7y62p4sigena6xlr, and Reference its
	// provider's own name for it: a VM clone, a workflow run's URL.
	ID          string          `json:"id"`
	Reference   string          `json:"reference,omitempty"`
	Environment environmentJSON `json:"environment"`
	Attempt     int             `json:"attempt"`
	// Identity is what the environment was as the run began, by its
	// provider's words, as evidence compares it later; empty where the
	// provider can't say.
	Identity string `json:"identity,omitempty"`
	State    string `json:"state"`
	Detail   string `json:"detail,omitempty"`
	// Reused is true for a run that built nothing, reusing earlier builds'
	// results, each of which names the run that built it.
	Reused  bool            `json:"reused,omitempty"`
	Results []resultLogJSON `json:"results"`
}

type resultLogJSON struct {
	Target     string `json:"target"`
	Outcome    string `json:"outcome"`
	Phase      string `json:"phase,omitempty"`
	Log        string `json:"log,omitempty"`
	ReusedFrom string `json:"reused_from,omitempty"`
	// Git is what a Git-fetched target's build was to fetch, and Fetched
	// the commit it recorded fetching, absent where its provider didn't
	// say.
	Git     *gitSourceJSON `json:"git,omitempty"`
	Fetched string         `json:"fetched,omitempty"`
	// Steps are where each step of the build began in its log, in the
	// order they ran; absent where its provider didn't record them.
	Steps []logStepJSON `json:"steps,omitempty"`
}

// logStepJSON is where a step of a build began in its log: the line its
// output starts on, counting from 1.
type logStepJSON struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

func logStepViews(steps []model.LogStep) []logStepJSON {
	var views []logStepJSON
	for _, step := range steps {
		views = append(views, logStepJSON{Name: string(step.Name), Line: step.Line})
	}
	return views
}

// portLogJSON is logs --port's result: the port's whole log, and where
// each step of its build began in it.
type portLogJSON struct {
	Run  runJSON `json:"run"`
	Port string  `json:"port"`
	Log  string  `json:"log"`
	// Steps are where each step of the build began in the log, in the
	// order they ran; absent where its provider didn't record them.
	Steps []logStepJSON `json:"steps,omitempty"`
	// OwnBuild is the line the port's own build begins on, after its
	// dependencies' installs, where the text output starts; absent where
	// the steps don't say.
	OwnBuild int `json:"own_build,omitempty"`
	// Execution is the provider run whose build this is; Elsewhere, the
	// check's other builds of the port, where it built it in more than one
	// place.
	Execution string                 `json:"execution,omitempty"`
	Elsewhere []portLogElsewhereJSON `json:"elsewhere,omitempty"`
	Text      string                 `json:"text"`
}

type portLogElsewhereJSON struct {
	Execution   string          `json:"execution"`
	Environment environmentJSON `json:"environment"`
	Outcome     string          `json:"outcome"`
	Tests       string          `json:"tests,omitempty"`
}

func portLogView(run model.Run, result model.TargetResult, data []byte) portLogJSON {
	view := portLogJSON{Run: runView(run), Port: string(result.Target), Log: logWhere(result.Log), Steps: logStepViews(result.Steps), Text: string(data)}
	if own, ok := result.OwnBuild(); ok {
		view.OwnBuild = own.Line
	}
	return view
}

func logsView(logs engine.RunLogs) logsJSON {
	view := logsJSON{Run: runView(logs.Run), Executions: []executionLogsJSON{}}
	for _, execution := range logs.Executions {
		x := execution.Execution
		entry := executionLogsJSON{ID: string(x.ID), Reference: x.ProviderRef, Environment: environmentView(x.Environment), Attempt: x.Attempt, Identity: x.Identity, State: string(x.State), Detail: x.Detail,
			Reused: x.Reused, Results: []resultLogJSON{}}
		for _, result := range execution.Results {
			view := resultLogJSON{Target: string(result.Target), Outcome: string(result.Outcome), Phase: string(result.Phase), Log: logWhere(result.Log),
				ReusedFrom: string(result.ReusedFrom), Steps: logStepViews(result.Steps)}
			if fetch, ok := execution.Git[result.Target]; ok {
				source := gitSourceView(fetch.Expected)
				view.Git, view.Fetched = &source, string(fetch.Fetched)
			}
			entry.Results = append(entry.Results, view)
		}
		view.Executions = append(view.Executions, entry)
	}
	return view
}

type outdatedPortJSON struct {
	Port     string `json:"port"`
	Current  string `json:"current"`
	Newest   string `json:"newest"`
	Outdated bool   `json:"outdated"`
	// Uncertain are the versions that compare newer than the current one
	// but were tagged on commits older than its own, newest first:
	// whether the port is outdated is a person's call, and newest is the
	// first of them. Absent for a port that is current or outdated.
	Uncertain []setAsideJSON `json:"uncertain,omitempty"`
	Problem   string         `json:"problem,omitempty"`
}

// setAsideJSON is a version that compares newer than a port's own but was
// tagged on an older commit than the port's own tag, predates.
type setAsideJSON struct {
	Tag string `json:"tag"`
	// Version is the port's version at the tag, and Source the version the
	// tag spells, which dockhand update <port> <source> takes.
	Version  string `json:"version"`
	Source   string `json:"source"`
	Predates string `json:"predates"`
}

func setAsideView(aside []engine.SetAside) []setAsideJSON {
	var views []setAsideJSON
	for _, version := range aside {
		views = append(views, setAsideJSON{Tag: version.Tag, Version: version.Version, Source: version.Source, Predates: version.Predates})
	}
	return views
}

func outdatedView(report engine.OutdatedReport) map[string]any {
	ports := []outdatedPortJSON{}
	for _, port := range report.Ports {
		ports = append(ports, outdatedPortJSON{Port: port.Port, Current: port.Current, Newest: port.Newest, Outdated: port.Outdated, Uncertain: setAsideView(port.Uncertain), Problem: port.Problem})
	}
	return map[string]any{"master": report.Master, "ports": ports}
}

type preparedJSON struct {
	Port     string                    `json:"port"`
	Branch   string                    `json:"branch"`
	Before   string                    `json:"before"`
	After    string                    `json:"after"`
	Tidied   bool                      `json:"tidied"`
	Run      string                    `json:"run,omitempty"`
	Problem  string                    `json:"problem,omitempty"`
	Upstream *model.UpstreamComparison `json:"upstream,omitempty"`
}

func preparedView(prepared []engine.PreparedUpdate) map[string]any {
	views := []preparedJSON{}
	for _, done := range prepared {
		view := preparedJSON{Port: done.Planned.Port.Port, Branch: engine.BranchName(done.Planned.Name), Before: done.Update.Before.String(), After: done.Update.After.String(),
			Tidied: done.Tidied, Problem: done.Problem, Upstream: done.Update.Upstream}
		if done.Run != nil {
			view.Run = done.Run.Name()
		}
		views = append(views, view)
	}
	return map[string]any{"prepared": views}
}

// plainHTTPJSON is a URL a port names over plain HTTP, its https form, and
// whether that answers.
type plainHTTPJSON struct {
	Option  string `json:"option"`
	URL     string `json:"url"`
	HTTPS   string `json:"https"`
	Answers bool   `json:"https_answers"`
}
