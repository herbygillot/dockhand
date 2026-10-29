package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// The pull request's description is prdescription's (the architecture
// review's finding 5): the engine establishes what it can claim, in its
// own words where they're its own, and publishes what it composes. What
// submitting again does to each part keeps its names here, for the
// commands that say it.
type (
	DescriptionSections = prdescription.Sections
	SectionOutcome      = prdescription.Outcome
	NewPort             = prdescription.NewPort
)

const (
	SectionRefreshed = prdescription.Refreshed
	SectionCurrent   = prdescription.Current
	SectionKept      = prdescription.Kept
	SectionAbsent    = prdescription.Absent
)

// bodyFacts is what the description can claim, each from evidence.
type bodyFacts struct {
	Commits  []git.HistoryCommit
	Evidence *Evidence
	NoCheck  bool
	// Accepted are the failed ports accepted, each with why
	// (SubmitPlan.Accepted).
	Accepted map[string]AcceptedFailure
	Types    []string
	// Updated is true when dockhand wrote every commit and one is an
	// update it made: a new release, which the template calls an
	// enhancement unless Types says otherwise.
	Updated bool
	// RulesPassed and Squashed come from the commit rules.
	RulesPassed, Squashed bool
	// Searched is true when other open pull requests were looked for;
	// Others are what was found.
	Searched bool
	Others   []forge.PullRequestSummary
	// TestedBinaries and TestedVariants are the person's own statements.
	TestedBinaries, TestedVariants bool
	SkipNotification               bool
	// NewPorts are the ports the branch adds, as its Portfiles say them,
	// for a reviewer who has never heard of one.
	NewPorts []NewPort
	// Reviewed are the person's acceptances of what held the submission,
	// as DecisionWords says each (the person's voice).
	Reviewed []string
	// Note is the person's own note (model.Branch.Note), which the
	// Description gives after what dockhand wrote there.
	Note string
	// UncoveredCI are the releases MacPorts' CI builds on that no check
	// built on.
	UncoveredCI []string
	// HandEdits are the branch's hand edits, as its submitted tree has
	// them (Engine.handEdits).
	HandEdits []HandEdit
	// LeftOut are the dependents each rebuild left out with --except
	// (leftOut).
	LeftOut []string
	// HTTPS are the links each port's edits switched to https
	// (httpsSwitches).
	HTTPS []prdescription.HTTPSSwitch
	// Installed are the check's install findings as lines, with the
	// dependents the branch rebuilds (InstallFinding.Lines).
	Installed []string
}

// leftOut are the lines naming the dependents each rebuild of the
// branch's edits left out with --except, a line a rebuild, as its
// revision bumps recorded them: "Left out of the rebuild for jq 1.8.1
// with `--except`: **gron**." Only the terminal said them before (the
// docs dig's I41).
func leftOut(edits []model.Edit) []string {
	var lines []string
	said := map[string]bool{}
	for _, edit := range edits {
		if len(edit.Excepted) == 0 {
			continue
		}
		// A bump's subject names its rebuild; an update's own edit keeps
		// them where every dependent was left out, "jq: update to 1.8.1".
		_, rebuild, _ := strings.Cut(edit.Subject, ": ")
		if edit.Kind == model.EditUpdate {
			rebuild = "rebuild for " + edit.Port + " " + strings.TrimPrefix(rebuild, "update to ")
		}
		if said[rebuild] {
			continue
		}
		said[rebuild] = true
		var names []string
		for _, name := range edit.Excepted {
			names = append(names, "**"+name+"**")
		}
		lines = append(lines, fmt.Sprintf("Left out of the %s with `--except`: %s.", rebuild, prose.And(names)))
	}
	return lines
}

// httpsSwitches are the plain-HTTP links each directory's edits switched to
// https, as tidy's commit body says them (httpsLine), named by the port.
func httpsSwitches(edits []model.Edit) []prdescription.HTTPSSwitch {
	var switches []prdescription.HTTPSSwitch
	var said []string
	for _, edit := range edits {
		if len(edit.HTTPS) == 0 || slices.Contains(said, edit.Directory) {
			continue
		}
		said = append(said, edit.Directory)
		switches = append(switches, prdescription.HTTPSSwitch{Port: edit.Port, Line: httpsLine(edits, edit.Directory)})
	}
	return switches
}

// description is what the facts let the description claim, as
// prdescription composes it: the evidence's reports and table, in the
// engine's words, and the checklist's answers.
func (facts bodyFacts) description() prdescription.Facts {
	described := prdescription.Facts{Note: facts.Note, Reviewed: facts.Reviewed, Types: facts.Types, Updated: facts.Updated, NewPorts: facts.NewPorts, LeftOut: facts.LeftOut, HTTPS: facts.HTTPS,
		SkipNotification: facts.SkipNotification, Version: buildinfo.Current().Tag()}
	for _, hand := range facts.HandEdits {
		described.HandEdits = append(described.HandEdits, "**"+hand.Directory+"**: "+hand.Line())
	}
	for _, commit := range facts.Commits {
		described.Commits = append(described.Commits, prdescription.Commit{ID: commit.ID, Message: commit.Message})
	}
	evidence := facts.Evidence
	switch {
	case facts.NoCheck:
		described.TestedOn.NoCheck = true
	case evidence == nil:
		described.TestedOn.Pending = true
	default:
		described.TestedOn.UncoveredCI = prose.And(facts.UncoveredCI)
		described.TestedOn.Installed = facts.Installed
		checks := evidence.Checks()
		// An environment where nothing was built or reused, as one where
		// every port is excluded, has no report, and isn't named: it
		// wasn't tested, and the table says why.
		for i, environment := range evidence.Plan.Environments {
			for _, observation := range evidence.Observations(i) {
				built, reusedIn := evidence.Built(i, observation.Runs)
				described.TestedOn.Reports = append(described.TestedOn.Reports, report(environment, observation.Observed, built, checks, reusedIn))
			}
		}
		described.TestedOn.Reports = sharedRuns(described.TestedOn.Reports)
		shown := evidence.Shown()
		for _, i := range shown {
			described.TestedOn.Columns = append(described.TestedOn.Columns, EnvironmentHeading(evidence.Plan.Environments[i], evidence.Plan.Environments))
		}
		for _, target := range evidence.Targets {
			row := prdescription.Row{Port: string(target.Target.ID)}
			for _, i := range shown {
				result := target.Outcomes[i]
				row.Cells = append(row.Cells, prdescription.Cell{Words: EvidenceWords(*evidence, target, i, acceptedFor(facts, target.Target.Target.Name)), Reason: result.Reason()})
			}
			described.TestedOn.Rows = append(described.TestedOn.Rows, row)
		}
	}
	built := evidence != nil && !facts.NoCheck && allBuilt(*evidence, facts.Accepted)
	answers := prdescription.Verification{RulesPassed: facts.RulesPassed, Squashed: facts.Squashed, Searched: facts.Searched, Built: built,
		AsksTests: evidence == nil || testsDeclared(evidence), TestedBinaries: facts.TestedBinaries, Variants: facts.TestedVariants}
	answers.TestsPassed = built && testsPassed(*evidence)
	for _, pr := range facts.Others {
		answers.Others = append(answers.Others, pr.Number)
	}
	// A --variants each check that passed answers the variants item, and
	// says which it built; otherwise it's the person's statement.
	if evidence != nil && !facts.NoCheck {
		if port, builds, passed := evidence.VariantsBuilt(); passed {
			answers.Variants, answers.VariantsNote = true, fmt.Sprintf("(Dockhand built %s with each of %s added to the default variants)", port, strings.Join(builds, ", "))
		}
	}
	described.Verification = answers
	return described
}

// pullRequestBody writes the description in the template's sections,
// ticking only what the facts establish.
func pullRequestBody(facts bodyFacts) string {
	return prdescription.Compose(facts.description())
}

// report is one environment's report as the description gives it: what
// it observed, the release and tools the environment states, for what it
// didn't report, who built it, and the runs behind it, each with its
// check.
func report(environment model.Environment, observed model.Observed, runs []model.GuestExecution, checks map[model.RunID]string, reusedIn map[model.ExecutionID]string) prdescription.Report {
	described := prdescription.Report{Observed: observed, Architecture: environment.Platform.Architecture, Tools: environment.DeveloperTools, Provider: providerWords(environment, observed)}
	if environment.Platform != (model.Platform{}) {
		described.Release = strings.TrimPrefix(describePlace(model.Environment{Platform: environment.Platform}), " ")
	}
	for _, run := range runs {
		described.Runs = append(described.Runs, prdescription.Run{ID: string(run.ID), Ref: run.ProviderRef, Check: checks[run.Run], ReusedIn: reusedIn[run.ID]})
	}
	return described
}

// sharedRuns are the reports with those of one provider run that built
// several environments as one, as a GitHub workflow run builds each
// release of its matrix: the releases its runners reported together,
// "macOS 14, 15", with the run named once.
func sharedRuns(reports []prdescription.Report) []prdescription.Report {
	refs := func(report prdescription.Report) []string {
		var refs []string
		for _, run := range report.Runs {
			if run.Ref != "" && !slices.Contains(refs, run.Ref) {
				refs = append(refs, run.Ref)
			}
		}
		slices.Sort(refs)
		return refs
	}
	shared := func(a, b prdescription.Report) bool {
		return a.Provider == b.Provider && a.Tools == b.Tools && a.Observed.MacOS == "" && b.Observed.MacOS == "" &&
			len(a.Observed.Builders) > 0 && len(b.Observed.Builders) > 0 && len(refs(a)) > 0 && slices.Equal(refs(a), refs(b))
	}
	var merged []prdescription.Report
	for _, report := range reports {
		i := slices.IndexFunc(merged, func(m prdescription.Report) bool { return shared(m, report) })
		if i < 0 {
			merged = append(merged, report)
			continue
		}
		merged[i].Observed.Builders = append(slices.Clone(merged[i].Observed.Builders), report.Observed.Builders...)
		merged[i].Runs = append(slices.Clone(merged[i].Runs), report.Runs...)
	}
	return merged
}

// allBuilt reports whether every target passed or was accepted.
func allBuilt(evidence Evidence, accepted map[string]AcceptedFailure) bool {
	for _, target := range evidence.Failed() {
		if _, ok := accepted[target.Target.Target.Name]; !ok {
			return false
		}
	}
	return len(evidence.Targets) > 0
}

func testsDeclared(evidence *Evidence) bool {
	if evidence == nil {
		return false
	}
	for _, target := range evidence.Targets {
		for _, result := range target.Outcomes {
			switch result.Tests {
			case model.TestsPassed, model.TestsFailed, model.TestsTimedOut:
				return true
			}
		}
	}
	return false
}

func testsPassed(evidence Evidence) bool {
	for _, target := range evidence.Targets {
		for _, result := range target.Outcomes {
			if result.Tests == model.TestsFailed || result.Tests == model.TestsTimedOut {
				return false
			}
		}
	}
	return true
}

// acceptedFor is why a port's failure was accepted, as its cells say it
// (SubmitPlan.Accepted); the zero value where it wasn't accepted.
func acceptedFor(facts bodyFacts, port string) AcceptedFailure {
	return facts.Accepted[port]
}
