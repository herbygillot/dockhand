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
	Diff      string        `json:"diff,omitempty"`
	// Revbumped are the dependents --revbump-dependents bumped, or would.
	Revbumped []string `json:"revbumped,omitempty"`
}

func updateView(branch model.Branch, started bool, update engine.Update, plan bool) updateJSON {
	view := updateJSON{Branch: branchRef(branch), Started: started, Port: update.Port,
		Before: versionJSON{update.Before.Version, update.Before.Revision}, After: versionJSON{update.After.Version, update.After.Revision},
		Current: update.Current, Applied: update.Applied, Files: nonNil(update.Files), Distfiles: update.Distfiles, Subject: update.Subject, Patches: nonNil(update.PatchProblems)}
	if plan {
		view.Diff = update.Diff
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
	Notes     []string `json:"notes"`
	Blocking  []string `json:"blocking"`
}

type tidyJSON struct {
	Branch      string           `json:"branch"`
	Keep        bool             `json:"keep"`
	Unambiguous bool             `json:"unambiguous"`
	Commits     []tidyCommitJSON `json:"commits"`
	Findings    []findingJSON    `json:"findings"`
	// Applied is what applying made, when it did.
	Applied *tidyAppliedJSON `json:"applied"`
}

type tidyAppliedJSON struct {
	Checkpoint string   `json:"checkpoint"`
	Commits    []string `json:"commits"`
}

func tidyView(plan engine.TidyPlan) tidyJSON {
	view := tidyJSON{Branch: plan.Branch.ShortName(), Keep: plan.Keep, Unambiguous: plan.Unambiguous(), Commits: []tidyCommitJSON{}, Findings: findingsView(plan.Findings)}
	for _, group := range plan.Groups {
		commit := tidyCommitJSON{Subject: group.Subject(), Message: group.Message, Paths: nonNil(group.Paths), Ports: nonNil(group.Ports), Combines: []string{},
			Working: group.Working, FromEdits: group.FromEdits, Notes: nonNil(group.Notes), Blocking: nonNil(group.Blocking)}
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
}

type submittedJSON struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Created bool   `json:"created"`
	Pushed  bool   `json:"pushed"`
	Draft   bool   `json:"draft"`
}

func submitView(plan engine.SubmitPlan) submitJSON {
	return submitJSON{Branch: plan.Branch.ShortName(), Title: plan.Title, Commit: plan.Commit, Commits: len(plan.Commits), From: plan.Head(), To: plan.Repository + ":" + engine.UpstreamBranch,
		Push: pushWords(plan), Checks: checkWords(plan), Findings: findingsView(plan.Findings), Blocking: nonNil(plan.Blocking), LeftOut: nonNil(plan.LeftOut), Body: plan.Body}
}

type cleanStepJSON struct {
	What    string `json:"what"`
	Kept    string `json:"kept,omitempty"`
	Removed bool   `json:"removed"`
}

type cleanBranchJSON struct {
	Branch string          `json:"branch"`
	Merged string          `json:"merged_at"`
	Steps  []cleanStepJSON `json:"steps"`
}

func cleanView(plans []engine.CleanBranch) []cleanBranchJSON {
	views := []cleanBranchJSON{}
	for _, plan := range plans {
		view := cleanBranchJSON{Branch: plan.Branch.ShortName(), Merged: string(plan.Merged), Steps: []cleanStepJSON{}}
		for _, step := range plan.Steps {
			view.Steps = append(view.Steps, cleanStepJSON{What: step.What, Kept: step.Kept, Removed: step.Done})
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
	Environment environmentJSON `json:"environment"`
	Attempt     int             `json:"attempt"`
	State       string          `json:"state"`
	Detail      string          `json:"detail,omitempty"`
	Results     []resultLogJSON `json:"results"`
}

type resultLogJSON struct {
	Target  string `json:"target"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase,omitempty"`
	Log     string `json:"log,omitempty"`
}

func logsView(logs engine.RunLogs) logsJSON {
	view := logsJSON{Run: runView(logs.Run), Executions: []executionLogsJSON{}}
	for _, execution := range logs.Executions {
		x := execution.Execution
		entry := executionLogsJSON{Environment: environmentView(x.Environment), Attempt: x.Attempt, State: string(x.State), Detail: x.Detail, Results: []resultLogJSON{}}
		for _, result := range execution.Results {
			entry.Results = append(entry.Results, resultLogJSON{Target: string(result.Target), Outcome: string(result.Outcome), Phase: string(result.Phase), Log: result.Log})
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
	Problem  string `json:"problem,omitempty"`
}

func outdatedView(report engine.OutdatedReport) map[string]any {
	ports := []outdatedPortJSON{}
	for _, port := range report.Ports {
		ports = append(ports, outdatedPortJSON{Port: port.Port, Current: port.Current, Newest: port.Newest, Outdated: port.Outdated, Problem: port.Problem})
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
