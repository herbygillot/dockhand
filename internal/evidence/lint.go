package evidence

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
)

// LintFinding is what port lint warned of for a target in one
// environment (the docs dig's I13): its lines, each a notice, as MacPorts'
// CI shows them to a reviewer. Holds says they count as ✗, for a port the
// branch adds, whose submission they hold until fixed or accepted;
// NotNitpicked is a port the branch adds whose lint ran without
// --nitpick, as MacPorts' CI lints, so its nitpicks weren't read.
type LintFinding struct {
	Target       model.TargetID
	Port         string
	Environment  model.Environment
	Lines        []string
	Holds        bool
	NotNitpicked bool
}

// LintFindings are the evidence's lint findings, in its targets' and
// environments' order: each recorded result whose lint warned, or of a
// port the branch adds that lint didn't nitpick; nothing where lint's
// output wasn't read, for a port the branch changes.
func LintFindings(ev Evidence) []LintFinding {
	var findings []LintFinding
	for _, target := range ev.Targets {
		for _, cell := range target.Outcomes {
			if cell.Kind != CellRecorded || cell.Outcome == model.OutcomeBlocked {
				continue
			}
			finding := LintFinding{Target: target.Target.ID, Port: target.Target.Target.Name, Environment: cell.Environment, Lines: cell.Lint}
			finding.Holds = target.Target.Added && len(cell.Lint) > 0
			finding.NotNitpicked = target.Target.Added && !cell.Nitpicked
			if len(finding.Lines) > 0 || finding.NotNitpicked {
				findings = append(findings, finding)
			}
		}
	}
	return findings
}

// LintHolds reports whether lint warned of a port the branch adds, in some
// environment: a new port has no baseline to compare with, so its
// warnings count as ✗ (design v3 §8's rules table).
func (t TargetEvidence) LintHolds() bool {
	return t.Target.Added && slices.ContainsFunc(t.Outcomes, func(c Cell) bool { return c.Kind == CellRecorded && len(c.Lint) > 0 })
}
