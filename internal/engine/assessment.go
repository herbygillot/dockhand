package engine

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/comparison"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// An assessment is a revision's (the assessment design, D): each port a
// revision's files change is assessed against the revision's captured
// base, whoever changed it, so its net change is what's assessed, never
// the edits that made it. A license changed and changed back holds
// nothing, and a hand edit after a clean comparison is assessed again.
//
// Collecting one fetches and evaluates, so only commands acting on a
// request collect: update, a check's driver, submit, and serve. Status
// reads what's recorded.

// revisionAssessments are what upstream's change means for each port a
// revision's files change against the base it was captured on: those
// recorded that still apply, and, where collect, those made now for the
// rest, which are recorded. What collecting one couldn't do is its
// comparison's Problem, never an error: the assessment is incomplete, and
// holds as what couldn't be checked does. One recorded incomplete is kept
// for its files, as an update's comparison always was; their next version
// is assessed afresh.
func (e *Engine) revisionAssessments(ctx context.Context, branch model.BranchID, base, tree model.ObjectID, collect bool) ([]model.Assessment, error) {
	analysis, err := e.analyze(ctx, base, tree)
	if err != nil {
		return nil, err
	}
	// A revision's change records and assessments are read in one view,
	// and what's made of them is written in one transaction (SQL-O7's
	// remainder, the docs dig's I44).
	// seen are every assessment of the revision read, under any policy,
	// recorded those under this one.
	var recorded, seen []model.Assessment
	var owner model.Branch
	var records map[string]model.ChangeRecord
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if owner, err = r.Branch(branch); err != nil {
			return err
		}
		if records, err = recordedChanges(r, branch, base, tree); err != nil {
			return err
		}
		revision, err := r.Assessments(store.AssessmentFilter{Branch: branch, Tree: tree, Base: base})
		seen = revision
		for _, a := range revision {
			if a.Policy == assess.Policy {
				recorded = append(recorded, a)
			}
		}
		return err
	}); err != nil {
		return nil, err
	}
	// Assessing evaluates the revision's Portfiles, which the trust rule
	// lets unattended work do of the person's alone.
	if !collect || !e.mayEvaluate(ctx, owner, "assessment") {
		return recorded, nil
	}
	read := maps.Clone(records)
	changes, err := e.makeChanges(ctx, owner, base, tree, records)
	if err != nil {
		return nil, err
	}
	found, made, err := e.makeAssessments(ctx, branch, base, analysis.BaseTree, tree, analysis.Paths, recorded, records)
	switch {
	case errors.Is(err, errNoPlanner):
		// Only an engine given no evaluator can't plan archives: it
		// collects nothing, and reads what's recorded, the change records
		// it made kept.
		found, made = recorded, nil
	case err != nil:
		return nil, err
	}
	if len(changes)+len(made) > 0 {
		if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			if err := recordChanges(tx, branch, base, tree, read, changes); err != nil {
				return err
			}
			// An assessment is recorded where the revision's of its port
			// is still the one read before it was made, as a change record
			// is (recordChanges).
			now, err := tx.Assessments(store.AssessmentFilter{Branch: branch, Tree: tree, Base: base})
			if err != nil {
				return err
			}
			for _, a := range made {
				i := slices.IndexFunc(now, func(c model.Assessment) bool { return c.Port == a.Port })
				j := slices.IndexFunc(seen, func(c model.Assessment) bool { return c.Port == a.Port })
				if i >= 0 && (j < 0 || !now[i].At.Equal(seen[j].At)) {
					continue
				}
				if err := tx.RecordAssessment(a); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return append(found, made...), nil
}

// revisionComparisons are a revision's assessments, by port, as a
// submission and status show them.
func (e *Engine) revisionComparisons(ctx context.Context, branch model.Branch, tree model.ObjectID, collect bool) ([]PortComparison, error) {
	assessments, err := e.revisionAssessments(ctx, branch.ID, branch.Base, tree, collect)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(assessments, func(a, b model.Assessment) int { return macports.ComparePortNames(a.Port, b.Port) })
	var comparisons []PortComparison
	for _, a := range assessments {
		// A subport whose source didn't change is no part of what
		// upstream's change means (field testing, 2026-10-02).
		if !SourceUnchanged(a.Comparison) {
			comparisons = append(comparisons, PortComparison{Port: a.Port, Comparison: a.Comparison})
		}
	}
	return comparisons, nil
}

// archivePlanner is what says what a port fetches: MacPorts' evaluator,
// unless one was given.
func (e *Engine) archivePlanner() (ArchivePlanner, error) {
	ports, err := e.capabilities()
	if err != nil {
		return nil, err
	}
	if ports.Archives == nil {
		return nil, errNoPlanner
	}
	return ports.Archives, nil
}

// makeAssessments makes the assessments of each port a revision's changed
// paths change, against its base, recording none: those recorded that
// apply are found, and the rest made, an incomplete one made again where
// another try may meet what kept it so; errNoPlanner where the engine,
// given no evaluator, can't plan archives. Only the subports the
// revision's change records say it changes are assessed, every one of a
// directory with no record. Review assesses a pull request this way,
// which no branch records, by its text.
func (e *Engine) makeAssessments(ctx context.Context, branch model.BranchID, base, baseTree, tree model.ObjectID, changed []string, recorded []model.Assessment, records map[string]model.ChangeRecord) (found, made []model.Assessment, err error) {
	have := func(port string) (model.Assessment, bool) {
		i := slices.IndexFunc(recorded, func(a model.Assessment) bool { return a.Port == port })
		if i < 0 {
			return model.Assessment{}, false
		}
		return recorded[i], true
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, nil, err
	}
	planner, err := e.archivePlanner()
	if err != nil {
		return nil, nil, err
	}
	sources := [2]model.Source{{Commit: base, Tree: baseTree, Base: base}, {Tree: tree, Base: base}}
	for _, directory := range macports.ScopeOf(changed).Ports {
		exists := [2]bool{}
		// The revision's Portfile, which the comparison reads as the
		// collector's caller gives it.
		var portfileText []byte
		for i, source := range sources {
			file, text, err := e.Repo.File(ctx, string(source.Tree), directory+"/Portfile")
			if err != nil {
				return nil, nil, err
			}
			exists[i] = file.Exists
			if i == 1 {
				portfileText = text
			}
		}
		if !exists[1] {
			// A port removed or moved away is assessed where it now is,
			// if anywhere.
			continue
		}
		ports, err := reader.Ports(ctx, sources[1], directory, model.Environment{}, nil)
		if err != nil && slices.ContainsFunc(recorded, func(a model.Assessment) bool { return a.Directory == directory }) {
			// What was recorded of it stands, as an update recorded it.
			for _, a := range recorded {
				if a.Directory == directory {
					found = append(found, a)
				}
			}
			continue
		}
		if err != nil {
			made = append(made, model.Assessment{Branch: branch, Tree: tree, Base: base, Port: directory, Directory: directory, Policy: assess.Policy, Build: assessmentBuild(), At: e.now(),
				Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}, Problem: "its ports couldn't be read: " + err.Error()}})
			continue
		}
		var basePorts []macports.PortInfo
		if exists[0] {
			// Read only to find a new subport's sibling; where it can't
			// be, each subport is evaluated by its own name, as before.
			basePorts, _ = reader.Ports(ctx, sources[0], directory, model.Environment{}, nil)
		}
		for _, port := range ports {
			// A subport the revision's record says it didn't change is no
			// part of what upstream's change means: terraform-1.16's
			// branch compared upstream for its 16 other subports.
			if !recordedChange(records, directory, port.Name) {
				continue
			}
			// One recorded incomplete for what another try may meet, a
			// network's failure or a forge's rate limit, is tried again;
			// so is an archive a server refused, as a 404 does, once a day
			// has passed, since it may be served again (v0.4.0's known
			// issues: it held until a file was edited).
			if a, ok := have(port.Name); ok && !e.lookAgain(a) {
				// Said, since whether one was made again or reused wasn't
				// (the rust and cargo run).
				progress.VerboseReport(ctx, "%s: the assessment recorded for these files under policy %d stands", port.Name, a.Policy)
				found = append(found, a)
				continue
			}
			made = append(made, model.Assessment{Branch: branch, Tree: tree, Base: base, Port: port.Name, Directory: directory, Policy: assess.Policy, Build: assessmentBuild(), At: e.now(),
				Comparison: e.collector(planner).Revision(ctx, sources, directory, [2]string{macports.CounterpartIn(basePorts, port.Name), port.Name}, exists[0], portfileText)})
		}
	}
	return found, made, nil
}

// refusedAgainAfter is how long an archive a server refused is held on
// before an assessment fetches it again.
const refusedAgainAfter = 24 * time.Hour

// lookAgain reports whether a recorded assessment is made again rather
// than reused: one that couldn't compare for what another try may meet
// at once, or for a server's refusal a day old, and one another build
// made that couldn't compare at all, since most of what field testing
// fixes turns such a refusal into a reading, which raising assess.Policy
// for it was a habit rather than a rule to bring (the docs dig's I26). A
// complete one stands across builds.
func (e *Engine) lookAgain(a model.Assessment) bool {
	kind := refusal.Kind(a.Comparison.ProblemKind)
	if a.Comparison.Problem != "" && a.Build != assessmentBuild() {
		return true
	}
	return kind.Transient() || kind == refusal.UpstreamMissing && e.now().Sub(a.At) >= refusedAgainAfter
}

// assessmentBuild is this dockhand's build, as an assessment records it.
func assessmentBuild() string { return buildinfo.Current().String() }

// errNoPlanner is an engine that can't plan archives, given no evaluator.
var errNoPlanner = errors.New("assessing a revision needs MacPorts' evaluator")

// SourceUnchanged reports a comparison of a port whose source the change
// left as the base had it, which says nothing of upstream.
func SourceUnchanged(compared model.UpstreamComparison) bool {
	return slices.ContainsFunc(compared.Coverage, func(c model.Coverage) bool { return c.Policy == comparison.PolicySourceUnchanged })
}

// NothingCompared reports a comparison of a port that fetches nothing,
// which had no upstream source to compare, and says so in its coverage.
func NothingCompared(compared model.UpstreamComparison) bool {
	return len(compared.Changes) == 0 && compared.Problem == "" &&
		slices.ContainsFunc(compared.Coverage, func(c model.Coverage) bool { return c.Policy == comparison.PolicyNotCompared })
}

// ComparisonLine is one line of what comparing a branch's upstream
// archives found, under an Upstream heading: whether it holds, the ports it
// was found for, and its words.
type ComparisonLine struct {
	Hold  bool
	Ports []string
	Words string
}

// ComparisonLines are what the comparisons found, a line each, the same
// finding of several ports once with each port named: a change a passing
// build can't catch, archives that couldn't be compared, which hold, and
// what was read and checked, so silence isn't taken for not looking (rust
// 1.99.0, batch 23). Whether a line holds is its own field, where submit
// sliced a "! " back off the words it had put on them.
func ComparisonLines(comparisons []PortComparison) []ComparisonLine {
	var lines []ComparisonLine
	add := func(port string, hold bool, words string) {
		for i := range lines {
			if lines[i].Hold == hold && lines[i].Words == words {
				lines[i].Ports = append(lines[i].Ports, port)
				return
			}
		}
		lines = append(lines, ComparisonLine{Hold: hold, Ports: []string{port}, Words: words})
	}
	for _, c := range Grouped(comparisons) {
		if c.Comparison.Problem != "" {
			add(c.Port, true, "archives not compared: "+c.Comparison.Problem)
		}
		for _, change := range c.Comparison.Changes {
			add(c.Port, change.Hold, UnderUpstream(change).Message)
		}
		if words := CoverageWords(c.Comparison); words != "" {
			add(c.Port, false, words)
		}
	}
	return lines
}
