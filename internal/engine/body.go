package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/version"
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
	Accepted []string
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
	// Note is the person's own note (model.Branch.Note), which the
	// Description gives after what dockhand wrote there.
	Note string
}

// description is what the facts let the description claim, as
// prdescription composes it: the evidence's reports and table, in the
// engine's words, and the checklist's answers.
func (facts bodyFacts) description() prdescription.Facts {
	described := prdescription.Facts{Note: facts.Note, Types: facts.Types, Updated: facts.Updated, NewPorts: facts.NewPorts,
		SkipNotification: facts.SkipNotification, Version: version.Current().Tag()}
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
		for _, environment := range evidence.Plan.Environments {
			described.TestedOn.Columns = append(described.TestedOn.Columns, EnvironmentHeading(environment, evidence.Plan.Environments))
		}
		for _, target := range evidence.Targets {
			row := prdescription.Row{Port: string(target.Target.ID)}
			for i, result := range target.Outcomes {
				row.Cells = append(row.Cells, prdescription.Cell{Words: EvidenceWords(*evidence, target, i, slices.Contains(facts.Accepted, target.Target.Target.Name)), Reason: result.Reason()})
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
			answers.Variants, answers.VariantsNote = true, fmt.Sprintf("(dockhand built %s with each of %s over its defaults)", port, strings.Join(builds, ", "))
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

// ownedSections are the part of the description dockhand keeps up to date:
// Tested on through Verification.
func ownedSections(facts bodyFacts) string {
	return prdescription.Owned(facts.description())
}

// report is one environment's report as the description gives it: what
// it observed, the release and tools the environment states, for what it
// didn't report, who built it, and the runs behind it, each with its
// check.
func report(environment model.Environment, observed model.Observed, runs []model.GuestExecution, checks map[model.RunID]string, reusedIn map[model.ExecutionID]string) prdescription.Report {
	described := prdescription.Report{Observed: observed, Architecture: environment.Platform.Architecture, Tools: environment.DeveloperTools, Provider: providerWords(environment.Provider)}
	if environment.Platform != (model.Platform{}) {
		described.Release = strings.TrimPrefix(describePlace(model.Environment{Platform: environment.Platform}), " ")
	}
	for _, run := range runs {
		described.Runs = append(described.Runs, prdescription.Run{ID: string(run.ID), Ref: run.ProviderRef, Check: checks[run.Run], ReusedIn: reusedIn[run.ID]})
	}
	return described
}

// dockhandUpdate reports whether every commit carries dockhand's
// Generated-By line, which tidy writes only on a port's commit made of
// dockhand's own edits, and one of them is an update dockhand recorded.
func dockhandUpdate(commits []git.HistoryCommit, edits []model.Edit) bool {
	updated := false
	for _, commit := range commits {
		_, rest, _ := strings.Cut(strings.TrimSpace(commit.Message), "\n")
		_, trailers := splitTrailers(strings.TrimSpace(rest))
		if !slices.ContainsFunc(trailers, commitmsg.IsAttribution) {
			return false
		}
		updated = updated || slices.ContainsFunc(edits, func(edit model.Edit) bool {
			return edit.Kind == model.EditUpdate && edit.Subject == commit.Subject()
		})
	}
	return updated
}

func providerWords(provider string) string {
	switch provider {
	case buildenv.Tart:
		return provider + ": built in a clean VM"
	case buildenv.Prefix:
		return provider + ": built in a MacPorts prefix on the author's Mac"
	case buildenv.GitHub:
		return provider + ": MacPorts' CI workflow in the author's fork"
	}
	return provider + ": built by the author's own command"
}

// allBuilt reports whether every target passed or was accepted.
func allBuilt(evidence Evidence, accepted []string) bool {
	for _, target := range evidence.Failed() {
		if !slices.Contains(accepted, target.Target.Target.Name) {
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
