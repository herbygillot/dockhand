package branchstate

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// AcceptanceProblem is why a port submit --accept names can't be
// accepted: it wasn't checked, it passed with nothing lint warned of a new
// port, no check of these files built it, or it's a changed port, whose
// failure is shared as a draft. A port
// may have several builds, its variant builds beside its default one: a
// build's own name always takes it, and a port's bare name takes a variant
// build where it's the one that failed (evidence.AcceptTarget); where
// several of the port's builds failed, the bare name is refused, naming
// each by its build (#235). atBase is keyed by build, as failsAtBase makes
// it.
func AcceptanceProblem(ev evidence.Evidence, accept []string, atBase map[string][]string, standing bool) error {
	for _, port := range accept {
		target, ok := ev.AcceptTarget(port)
		if !ok {
			if failed := ev.FailedBuildsOf(port); len(failed) > 1 {
				var names []string
				for _, build := range failed {
					names = append(names, "--accept '"+ev.AcceptName(build)+"'")
				}
				return fmt.Errorf("--accept %s: %s failed in %d builds in %s, and each is accepted by its build's name. Name the builds to accept: %s", port, port, len(failed), ev.Run.Name(), strings.Join(names, " "))
			}
			// No build of the port failed: say what its builds did, one
			// that asks for a check before one that passed.
			builds := ev.BuildsOf(port)
			if i := slices.IndexFunc(builds, evidence.TargetEvidence.Failing); i >= 0 {
				target, ok = builds[i], true
			} else if len(builds) > 0 {
				target, ok = builds[0], true
			}
		}
		switch {
		case !ok:
			return fmt.Errorf("--accept %s: %s did not check this port", port, ev.Run.Name())
		case target.Passed && target.LintHolds() && !standing:
			// A new port's lint warnings, which --accept takes (the docs
			// dig's I13).
		case target.Passed:
			return fmt.Errorf("--accept %s: this port passed in %s; there is no failure to accept", port, ev.Run.Name())
		case target.Missing() || !target.Failing():
			return fmt.Errorf("--accept %s: no check of these files built this port, so there is no failure to accept. Run dockhand check", port)
		case standing:
			// A standing acceptance takes a dependency's failure in an
			// environment, whatever port needs it, and nothing else.
			if _, ok := EnvironmentFailures(ev, target); !ok {
				return fmt.Errorf("--accept %s --standing: a standing acceptance applies only to a dependency install failure with a recorded dependency version, and %s failed otherwise in %s", port, port, ev.Run.Name())
			}
		case !target.Acceptable() && len(atBase[string(target.Target.ID)]) == 0:
			return fmt.Errorf("--accept %s: %s has changed. Its failure can be accepted only if the baseline fails the same way. Run dockhand check --baseline to compare, or use --draft to share the failing change", port, port)
		}
	}
	return nil
}

// PublicationBlocks applies the publication rule to the evidence: every
// changed target checked and every substantive one passed, and every
// other failure accepted, each refusal a block of its kind. atBase is
// keyed by build, as AcceptanceProblem reads it.
func PublicationBlocks(ev evidence.Evidence, accepted []string, atBase map[string][]string, standing map[string]AcceptedFailure) []Block {
	blocked := BlockedByAccepted(ev, slices.Concat(accepted, slices.Collect(maps.Keys(standing))))
	var blocks []Block
	run := ev.Run.Name()
	for _, target := range ev.Failed() {
		name := ev.AcceptName(target)
		if slices.Contains(blocked, name) {
			continue
		}
		unmet, needs := target.Unmet()
		remade := target.Remade()
		switch {
		case needs && target.Missing():
			blocks = append(blocks, Block{Kind: BlockUnmet, Port: name, Unmet: unmet})
		case len(remade) > 0 && target.Missing():
			t := target
			blocks = append(blocks, Block{Kind: BlockRemade, Port: name, Target: &t})
		case target.Missing():
			blocks = append(blocks, Block{Kind: BlockUnchecked, Port: name, Unbuilt: unbuilt(target)})
		case standing[name].Kind != "":
			// Its every failure is a dependency's accepted standing.
		case !target.Acceptable() && len(atBase[name]) > 0 && !ev.Accepts(accepted, target):
			// A failure master has too is no regression, the usual case for
			// a MacPorts pull request: a person accepts it, never bump or
			// serve, and the pull request says so (pgdog 0.1.62 on macOS 12,
			// field testing; the person's word, 2026-10-17).
			blocks = append(blocks, Block{Kind: BlockFailsAtBase, Port: name, Run: run, Baselines: atBase[name]})
		case !target.Acceptable() && len(atBase[name]) == 0:
			blocks = append(blocks, Block{Kind: BlockFailed, Port: name, Run: run})
		case !ev.Accepts(accepted, target):
			blocks = append(blocks, Block{Kind: BlockNotAccepted, Port: name, Run: run, Planned: target.Target})
		}
	}
	return append(blocks, LintBlocks(ev, accepted)...)
}

// ReadsDecisions reports whether a standing of the evidence reads the
// person's decisions: a failure's acceptance, an environment's standing
// one, and a new port's lint acceptance, which a passed build holds for
// too. Status loaded them only where a target failed, so it said a branch
// wasn't ready that submit took (Codex's review of 8e314b96, finding 6).
func ReadsDecisions(ev evidence.Evidence) bool {
	return len(ev.Failed()) > 0 || slices.ContainsFunc(evidence.LintFindings(ev), func(f evidence.LintFinding) bool { return f.Holds })
}

// LintBlocks are the ports the branch adds that lint warned of and a
// person hasn't accepted at this commit, each a block with what lint said
// once: a new port has no baseline, so its warnings count as ✗, its build
// answering all the same (the docs dig's I13). A port that failed is
// refused for its failure first.
func LintBlocks(ev evidence.Evidence, accepted []string) []Block {
	var blocks []Block
	for _, finding := range evidence.LintFindings(ev) {
		if !finding.Holds || slices.Contains(accepted, finding.Port) {
			continue
		}
		i := slices.IndexFunc(ev.Targets, func(t evidence.TargetEvidence) bool { return t.Target.ID == finding.Target })
		if ev.Targets[i].Failing() {
			continue
		}
		if j := slices.IndexFunc(blocks, func(b Block) bool { return b.Port == finding.Port }); j >= 0 {
			for _, line := range finding.Lines {
				if !slices.Contains(blocks[j].Lint, line) {
					blocks[j].Lint = append(blocks[j].Lint, line)
				}
			}
			continue
		}
		blocks = append(blocks, Block{Kind: BlockLint, Port: finding.Port, Run: ev.Run.Name(), Lint: slices.Clone(finding.Lines)})
	}
	return blocks
}

// AcceptedFailure is why a failed port's failure is accepted, a kind
// where a sentence doubled as one (R3-30): its baseline failing alike,
// with the baselines that did, the cause not established for an extra or
// a revision bump, or blocked only by accepted failures. The zero value
// accepts nothing.
type AcceptedFailure struct {
	Kind AcceptedKind
	// Baselines are the baselines that fail it alike, for AcceptedAtBase.
	Baselines []string
	// Dependencies are the dependencies whose failures a standing
	// acceptance takes, for AcceptedStanding.
	Dependencies []model.FailedDependency
}

// AcceptedKind is the kind of reason a failure was accepted for.
type AcceptedKind string

const (
	// AcceptedAtBase is a failure the base has too: a finished baseline
	// reproduces it, in the same environment and at the same phase.
	AcceptedAtBase AcceptedKind = "at-base"
	// AcceptedUnestablished is an extra's or a revision bump's failure
	// accepted where nothing showed whose it was.
	AcceptedUnestablished AcceptedKind = "cause-not-established"
	// AcceptedBlocked is a port blocked only by accepted failures,
	// accepted with them.
	AcceptedBlocked AcceptedKind = "blocked-by-accepted"
	// AcceptedStanding is a port whose every failure is a dependency's a
	// person accepted standing, at the dependency's version on the image
	// it failed on (model.FindingEnvironmentFailure).
	AcceptedStanding AcceptedKind = "standing"
)

// EnvironmentFailure is one failure of a port that is a dependency's,
// naming the dependency and its version, and the standing decision that
// would accept it, by its environment's image.
type EnvironmentFailure struct {
	Dependency model.FailedDependency
	Decision   model.Decision
}

// EnvironmentFailures are a failed port's failures, where each is a
// dependency's that names the dependency and its version; false where any
// failure isn't one, or the port has none.
func EnvironmentFailures(ev evidence.Evidence, target evidence.TargetEvidence) ([]EnvironmentFailure, bool) {
	var failures []EnvironmentFailure
	for _, cell := range target.Outcomes {
		if cell.Kind != evidence.CellRecorded || cell.Outcome == model.OutcomePassed || cell.Outcome == model.OutcomeBlocked {
			continue
		}
		identity := ev.Executions[cell.Execution].Identity
		if cell.Outcome != model.OutcomeFailed || cell.Phase != model.PhaseDependencies || cell.Dependency.Name == "" || cell.Dependency.Version == "" || identity == "" {
			return nil, false
		}
		failures = append(failures, EnvironmentFailure{Dependency: cell.Dependency, Decision: model.Decision{Finding: model.FindingEnvironmentFailure,
			Subject: cell.Dependency.Name, Evidence: model.EnvironmentEvidence(cell.Dependency, identity), Answer: model.Accepted}})
	}
	return failures, len(failures) > 0
}

// StandingAccepted are the failed ports whose every failure is a
// dependency's a standing decision accepts, at its version on its image:
// later checks say so, and submit takes it without a baseline, until the
// dependency or the image changes (the person's choice, 2026-10-18).
func StandingAccepted(ev evidence.Evidence, standing []model.Decision) map[string]AcceptedFailure {
	accepted := map[string]AcceptedFailure{}
	for _, target := range ev.Failed() {
		failures, ok := EnvironmentFailures(ev, target)
		why := AcceptedFailure{Kind: AcceptedStanding}
		for _, failure := range failures {
			if !slices.ContainsFunc(standing, func(d model.Decision) bool {
				return d.Subject == failure.Decision.Subject && d.Evidence == failure.Decision.Evidence
			}) {
				ok = false
				break
			}
			if !slices.Contains(why.Dependencies, failure.Dependency) {
				why.Dependencies = append(why.Dependencies, failure.Dependency)
			}
		}
		if ok {
			accepted[ev.AcceptName(target)] = why
		}
	}
	return accepted
}

// AcceptedReasons are the failed ports accepted, each with why.
func AcceptedReasons(ev evidence.Evidence, accepted []string, atBase map[string][]string, standing map[string]AcceptedFailure) map[string]AcceptedFailure {
	reasons := map[string]AcceptedFailure{}
	for _, target := range ev.Failed() {
		name := ev.AcceptName(target)
		switch {
		case !ev.Accepts(accepted, target):
		case len(atBase[name]) > 0:
			reasons[name] = AcceptedFailure{Kind: AcceptedAtBase, Baselines: atBase[name]}
		default:
			reasons[name] = AcceptedFailure{Kind: AcceptedUnestablished}
		}
	}
	maps.Copy(reasons, standing)
	for _, name := range BlockedByAccepted(ev, slices.Concat(accepted, slices.Collect(maps.Keys(standing)))) {
		reasons[name] = AcceptedFailure{Kind: AcceptedBlocked}
	}
	return reasons
}

// BlockedByAccepted are the ports that failed only as blocked, in each
// environment by failed dependencies all of which are accepted, or blocked
// so in turn: accepted with them, unnamed. py-textual-fastdatatable's stub,
// blocked by its five py3NN subports whose failures the base has too, was
// refused under --accept of the five, and as a changed port of its own
// (field testing at 56e328ea).
func BlockedByAccepted(ev evidence.Evidence, accepted []string) []string {
	ok := map[model.TargetID]bool{}
	for _, target := range ev.Targets {
		if ev.Accepts(accepted, target) {
			ok[target.Target.ID] = true
		}
	}
	var found []string
	for changed := true; changed; {
		changed = false
		for _, target := range ev.Failed() {
			if ok[target.Target.ID] || target.Missing() {
				continue
			}
			blocked, clear := false, true
			for n, cell := range target.Outcomes {
				// A release the profile or the Portfile left out is no
				// result, though its cell reads not run: check-416's stub
				// was refused for minmax's skipped releases (field testing
				// at 3cf14522).
				if cell.Kind == evidence.CellExcluded || cell.Outcome == model.OutcomePassed {
					continue
				}
				if cell.Outcome != model.OutcomeBlocked {
					clear = false
				}
				if !clear {
					break
				}
				build, _ := ev.Plan.In(ev.Plan.Environments[n])
				for _, dependency := range build.Dependencies[target.Target.ID] {
					if !ok[dependency] && dependencyFailed(ev, dependency, n) {
						clear = false
					}
				}
				blocked = true
			}
			if blocked && clear {
				ok[target.Target.ID] = true
				found = append(found, ev.AcceptName(target))
				changed = true
			}
		}
	}
	return found
}

// dependencyFailed reports whether a target didn't pass in an environment
// it wasn't left out of.
func dependencyFailed(ev evidence.Evidence, id model.TargetID, environment int) bool {
	for _, target := range ev.Targets {
		if target.Target.ID == id {
			cell := target.Outcomes[environment]
			return cell.Kind != evidence.CellExcluded && cell.Outcome != model.OutcomePassed
		}
	}
	return false
}

// Words are why a failure was accepted, as a cell and the preview say it:
// "fails at the base too (check-394)", "cause not established", or
// "blocked only by accepted failures"; empty where it wasn't accepted.
func (a AcceptedFailure) Words() string {
	switch a.Kind {
	case AcceptedAtBase:
		return "fails at the base too (" + strings.Join(a.Baselines, ", ") + ")"
	case AcceptedUnestablished:
		return "cause not established"
	case AcceptedBlocked:
		return "blocked only by accepted failures"
	case AcceptedStanding:
		var failing []string
		for _, dependency := range a.Dependencies {
			failing = append(failing, dependency.Name+" "+dependency.Version)
		}
		return "standing: " + prose.And(failing) + " fails on this image"
	}
	return ""
}
