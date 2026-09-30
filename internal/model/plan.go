package model

import (
	"fmt"
	"slices"
	"time"
)

// PlanID identifies a frozen verification plan.
type PlanID string

// TargetID identifies a target within a plan.
type TargetID string

// TargetKind decides which publication rule applies to a target.
type TargetKind string

const (
	// Substantive is every target not proven revision-only; unknown is
	// substantive (decision 44).
	Substantive TargetKind = "substantive"
	// RevisionOnly is a target whose Portfile diff touches nothing but
	// revision declarations, with no files/ change, proven from the source.
	RevisionOnly TargetKind = "revision-only"
	// Unchanged is a target the branch does not change: an extra from --also.
	Unchanged TargetKind = "unchanged"
)

// TargetRole is why a target is in the plan.
type TargetRole string

const (
	// Changed targets derive from the diff against the base.
	Changed TargetRole = "changed"
	// Also targets are unchanged ports built against the branch on request.
	Also TargetRole = "also"
	// Prerequisite targets are changed ports that selected coverage needs:
	// with --only appB, the changed libA appB depends on. They are built,
	// never substituted by an old binary.
	Prerequisite TargetRole = "prerequisite"
)

// PlanTarget is one target of a plan, as the branch sees it: which port,
// where, and why the plan has it. What it needs in an environment, and
// whether it is built there, is that environment's (EnvironmentPlan).
type PlanTarget struct {
	ID     TargetID
	Target Target
	// Directory is the port directory relative to the tree root.
	Directory string
	Kind      TargetKind
	Role      TargetRole
}

// Exclusion is a port a plan doesn't build in one environment, and needn't
// pass there, with the reason: as MacPorts CI would exclude it, or because
// the environment's evaluation doesn't define it.
type Exclusion struct {
	Target Target
	Reason string
}

// Unresolved is a changed target whose evaluation failed. A plan with one
// cannot run; the target is never dropped as ineligible.
type Unresolved struct {
	Target Target
	Reason string
}

// TestPolicy says whether port tests run and whether they decide.
type TestPolicy string

const (
	// TestsDeclared runs declared tests and reports them without deciding,
	// as MacPorts CI does.
	TestsDeclared TestPolicy = "declared"
	TestsRequired TestPolicy = "required"
	TestsSkip     TestPolicy = "skip"
)

// Valid reports whether the policy is one of the three.
func (p TestPolicy) Valid() bool {
	return p == TestsDeclared || p == TestsRequired || p == TestsSkip
}

// Judge decides a target's recorded outcome from what its provider
// reported, under this test policy (Design v3 §7). Providers report facts,
// the build's outcome and the tests' outcome, and the runner judges every
// result by this rule as it records it, whichever provider built it: tests
// that failed or timed out fail a port that built only when the policy
// requires them, at the test phase. A port that declares no tests passes
// under any policy, and the tests' own outcome is always kept.
//
// Tart's guest program applies the same rule while it builds, since it
// must not build a port against a dependency that failed its required
// tests; the verdict recorded is this one, so the two can't disagree.
func (p TestPolicy) Judge(result TargetResult) TargetResult {
	if result.Outcome != OutcomePassed || p != TestsRequired {
		return result
	}
	if result.Tests.Failed() {
		result.Outcome, result.Phase = OutcomeFailed, PhaseTest
	}
	return result
}

// Stands reports whether an earlier passed result stands for a check under
// this policy, reused rather than built again (decision 28): it still
// passes when judged under it, as a result whose tests failed doesn't
// under required, and its tests ran unless this policy skips them.
func (p TestPolicy) Stands(result TargetResult) bool {
	if p.Judge(result).Outcome != OutcomePassed {
		return false
	}
	return p == TestsSkip || result.Tests != TestsSkipped
}

// Environment is one provider and platform a plan is checked on.
type Environment struct {
	Provider string
	Platform Platform
	// DeveloperTools are what builds there have, when the provider states
	// them: Tart's release image, with Xcode when the release has an
	// Xcode image, else the Command Line Tools alone. Unstated for a
	// provider whose builders have their own.
	DeveloperTools DeveloperTools `json:",omitempty"`
}

// Requirement is something a target needs of the environment it builds
// in, beyond what every environment has.
type Requirement string

// RequiresXcode is Xcode, not only the Command Line Tools.
const RequiresXcode Requirement = "Xcode"

// Unmet is a target an environment can't build, because the environment
// lacks what the target needs: Xcode, where there are only the Command
// Line Tools. The target needs it itself, or through a prerequisite: a
// changed port the plan builds before it, from source, never from an
// archive. Unlike an exclusion, which MacPorts CI makes too, the target
// still needs a check somewhere that has what it needs. Unlike a failure,
// it says nothing about the port.
type Unmet struct {
	Target      TargetID
	Environment Environment
	Needs       Requirement
	// Through is the prerequisite that needs it; empty when the target
	// needs it itself.
	Through TargetID `json:",omitempty"`
}

// EnvironmentPlan is what a check builds in one environment, from that
// environment's own evaluation of the ports: which of the plan's targets
// it builds, in its own dependency order, what they need there, and why it
// doesn't build the rest.
type EnvironmentPlan struct {
	Environment Environment
	// Order is the plan's targets built here, in this environment's own
	// dependency order. Unmet ones are in it: they are planned here, and
	// never sent to its provider.
	Order []TargetID
	// Dependencies are what each target in Order needs built first here,
	// among Order.
	Dependencies map[TargetID][]TargetID `json:",omitempty"`
	// NeedsXcode are the targets in Order that need Xcode here, not only
	// the Command Line Tools: their use_xcode, as MacPorts decides it with
	// the environment's tools.
	NeedsXcode []TargetID `json:",omitempty"`
	// Untested are the targets in Order that declare no tests here, their
	// test.run off as MacPorts evaluates it: they pass whatever the test
	// policy, which a plan requiring tests says of them.
	Untested []TargetID `json:",omitempty"`
	// Unmet are the targets in Order this environment can't build, decided
	// when the check is accepted.
	Unmet []Unmet `json:",omitempty"`
	// Exclusions are the ports the plan doesn't build here, and that
	// needn't pass here, those --only left out included.
	Exclusions []Exclusion `json:",omitempty"`
	// Git are the targets in Order fetched with Git here, each with the
	// source its build is expected to fetch: the commit its git.branch
	// named when the check was planned (batch 20). The changed targets
	// --only left out that would be built here have theirs too: the check
	// doesn't build them, but an earlier check's result of one stands only
	// where it fetched that commit, and a submission compares that commit
	// with what the tag names then.
	Git map[TargetID]GitSource `json:",omitempty"`
}

// Builds reports whether the environment's plan has the target in its
// order: built here, or planned here and unmet.
func (p EnvironmentPlan) Builds(id TargetID) bool { return slices.Contains(p.Order, id) }

// Plan freezes what a check of one revision covers. What the branch
// changes and what the person selected are the plan's own: Targets,
// Omitted, Only, and Also. What each environment builds, in what order,
// and why not the rest, is that environment's plan, in Builds.
type Plan struct {
	ID       PlanID
	Revision RevisionID
	// Environments are all required: several mean every one must pass.
	Environments []Environment
	// Targets are what the check builds in some environment, in the
	// environments' orders merged: the first environment's, and what each
	// other one adds, after what comes before it there.
	Targets []PlanTarget
	// Builds are each environment's own plan, one for each of
	// Environments, found by the whole environment.
	Builds     []EnvironmentPlan
	Unresolved []Unresolved
	// Omitted are the changed targets --only left out. The check doesn't
	// build them, but submission still requires them wherever they aren't
	// excluded: a narrowed check never shrinks what submit requires
	// (Design v3 §7).
	Omitted []PlanTarget `json:",omitempty"`
	// Only and Also record the selection as given, for status and the PR.
	Only  []string
	Also  []string
	Tests TestPolicy
	// Fresh builds every target, reusing no earlier build's result
	// (decision 28).
	Fresh bool `json:",omitempty"`
	// Variants are the variants --variants built its one port with, as
	// MacPorts writes them, in place of its defaults; EachVariant is
	// --variants each, which built it with its defaults and then with
	// each variant it declares.
	Variants    string `json:",omitempty"`
	EachVariant bool   `json:",omitempty"`
	CreatedAt   time.Time
}

// In is an environment's plan.
func (p Plan) In(environment Environment) (EnvironmentPlan, bool) {
	for _, build := range p.Builds {
		if build.Environment == environment {
			return build, true
		}
	}
	return EnvironmentPlan{}, false
}

// Runnable reports whether the plan may be checked: nothing is unresolved
// and there is something to build somewhere.
func (p Plan) Runnable() bool {
	if len(p.Unresolved) > 0 {
		return false
	}
	for _, build := range p.Builds {
		for _, id := range build.Order {
			if _, unmet := p.UnmetIn(build.Environment, id); !unmet {
				return true
			}
		}
	}
	return false
}

// Excludes reports whether the plan leaves a target out in an environment,
// where it is not built and not required to pass.
func (p Plan) Excludes(target PlanTarget, environment Environment) bool {
	_, excluded := p.ExclusionIn(environment, target.ID)
	return excluded
}

// ExclusionIn is why an environment doesn't build a target, when it
// doesn't: by the target's identity, so a port's variant build is its
// own.
func (p Plan) ExclusionIn(environment Environment, id TargetID) (Exclusion, bool) {
	build, _ := p.In(environment)
	for _, exclusion := range build.Exclusions {
		if exclusion.Target.ID() == id {
			return exclusion, true
		}
	}
	return Exclusion{}, false
}

// UnmetIn is why an environment can't build a target, when it can't.
func (p Plan) UnmetIn(environment Environment, id TargetID) (Unmet, bool) {
	build, _ := p.In(environment)
	for _, unmet := range build.Unmet {
		if unmet.Target == id {
			return unmet, true
		}
	}
	return Unmet{}, false
}

// DependsOnIn is what a target needs built first in one environment.
func (p Plan) DependsOnIn(environment Environment, id TargetID) []TargetID {
	build, _ := p.In(environment)
	return build.Dependencies[id]
}

// NeedsXcodeIn reports whether a target needs Xcode in an environment.
func (p Plan) NeedsXcodeIn(environment Environment, id TargetID) bool {
	build, _ := p.In(environment)
	return slices.Contains(build.NeedsXcode, id)
}

// GitIn is the source a target's build in an environment is expected to
// fetch, where it is fetched with Git there.
func (p Plan) GitIn(environment Environment, id TargetID) (GitSource, bool) {
	build, _ := p.In(environment)
	source, ok := build.Git[id]
	return source, ok
}

// Target finds a plan target by ID.
func (p Plan) Target(id TargetID) (PlanTarget, bool) {
	for _, target := range p.Targets {
		if target.ID == id {
			return target, true
		}
	}
	return PlanTarget{}, false
}

// Validate checks the plan's structure: unique targets, kinds that fit
// roles, one plan for each environment, and in each, an order that builds
// only the plan's targets, each after what it needs there.
func (p Plan) Validate() error {
	switch {
	case p.ID == "" || p.Revision == "":
		return invalid("plan %q has no ID or revision", p.ID)
	case len(p.Environments) == 0:
		return invalid("plan %s has no environment", p.ID)
	case p.CreatedAt.IsZero():
		return invalid("plan %s has no creation time", p.ID)
	}
	if !p.Tests.Valid() {
		return invalid("plan %s has unknown test policy %q", p.ID, p.Tests)
	}
	environments := map[Environment]bool{}
	for _, environment := range p.Environments {
		if environment.Provider == "" || environments[environment] {
			return invalid("plan %s repeats or leaves unnamed an environment", p.ID)
		}
		environments[environment] = true
	}
	seen := map[TargetID]bool{}
	for _, target := range p.Targets {
		if target.ID == "" || seen[target.ID] {
			return invalid("plan %s repeats or leaves unnamed a target", p.ID)
		}
		if target.Target.Name == "" || target.Directory == "" {
			return invalid("plan %s target %s has no port or directory", p.ID, target.ID)
		}
		// A target is known by its port's name and its variants, which the
		// environments' plans and every result name it by.
		if target.ID != target.Target.ID() {
			return invalid("plan %s target %s is %s", p.ID, target.ID, target.Target.ID())
		}
		switch target.Role {
		case Changed, Prerequisite:
			if target.Kind != Substantive && target.Kind != RevisionOnly {
				return invalid("plan %s target %s is changed but kind %q", p.ID, target.ID, target.Kind)
			}
		case Also:
			if target.Kind != Unchanged {
				return invalid("plan %s target %s is an extra but kind %q", p.ID, target.ID, target.Kind)
			}
		default:
			return invalid("plan %s target %s has unknown role %q", p.ID, target.ID, target.Role)
		}
		seen[target.ID] = true
	}
	for _, target := range p.Omitted {
		if target.ID == "" || seen[target.ID] || target.Role != Changed {
			return invalid("plan %s omits %q, which is planned, repeated, or not a changed target", p.ID, target.ID)
		}
		if target.ID != target.Target.ID() {
			return invalid("plan %s omits %s, which is %s", p.ID, target.ID, target.Target.ID())
		}
		seen[target.ID] = true
	}
	if len(p.Builds) != len(p.Environments) {
		return invalid("plan %s has plans for %d environments, not %d", p.ID, len(p.Builds), len(p.Environments))
	}
	built := map[TargetID]bool{}
	for _, build := range p.Builds {
		if !environments[build.Environment] {
			return invalid("plan %s has a plan for an environment it doesn't name, or two for one", p.ID)
		}
		environments[build.Environment] = false
		if err := build.validate(p); err != nil {
			return err
		}
		for _, id := range build.Order {
			built[id] = true
		}
	}
	for _, target := range p.Targets {
		if !built[target.ID] {
			return invalid("plan %s target %s is built in no environment", p.ID, target.ID)
		}
	}
	return nil
}

// validate checks one environment's plan against the plan it is part of.
func (b EnvironmentPlan) validate(p Plan) error {
	platform := b.Environment.Platform
	where := fmt.Sprintf("%s on %s %s %s %s", p.ID, b.Environment.Provider, platform.OS, platform.Version, platform.Architecture)
	earlier := map[TargetID]bool{}
	for _, id := range b.Order {
		if _, ok := p.Target(id); !ok || earlier[id] {
			return invalid("plan %s builds %s, which isn't its target or comes twice", where, id)
		}
		for _, need := range b.Dependencies[id] {
			if !earlier[need] {
				return invalid("plan %s target %s needs %s, which is not built before it there", where, id, need)
			}
		}
		earlier[id] = true
	}
	for id := range b.Dependencies {
		if !earlier[id] {
			return invalid("plan %s has dependencies for %s, which it doesn't build", where, id)
		}
	}
	for _, id := range b.NeedsXcode {
		if !earlier[id] {
			return invalid("plan %s says %s needs Xcode, and doesn't build it", where, id)
		}
	}
	for _, unmet := range b.Unmet {
		if !earlier[unmet.Target] || unmet.Environment != b.Environment {
			return invalid("plan %s can't meet %s, which it doesn't build", where, unmet.Target)
		}
	}
	for _, exclusion := range b.Exclusions {
		if earlier[exclusion.Target.ID()] {
			return invalid("plan %s both builds and excludes %s", where, exclusion.Target.ID())
		}
	}
	for id, source := range b.Git {
		omitted := slices.ContainsFunc(p.Omitted, func(target PlanTarget) bool { return target.ID == id })
		if _, excluded := p.ExclusionIn(b.Environment, id); !earlier[id] && (!omitted || excluded) {
			return invalid("plan %s has a Git source for %s, which it neither builds nor would build but for --only", where, id)
		}
		if problem := source.problem(); problem != "" {
			return invalid("plan %s: %s's Git source %s", where, id, problem)
		}
	}
	return nil
}
