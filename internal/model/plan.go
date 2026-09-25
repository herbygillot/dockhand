package model

import "time"

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

// PlanTarget is one target the plan builds.
type PlanTarget struct {
	ID     TargetID
	Target Target
	// Directory is the port directory relative to the tree root.
	Directory string
	Kind      TargetKind
	Role      TargetRole
	// DependsOn lists the plan's targets this one needs built first.
	DependsOn []TargetID
}

// Exclusion is a changed target the plan does not build on one platform,
// with the reason, as MacPorts CI would exclude it.
type Exclusion struct {
	Target   Target
	Platform Platform
	Reason   string
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

// Environment is one provider and platform a plan is checked on.
type Environment struct {
	Provider string
	Platform Platform
}

// Plan freezes what a check of one revision covers.
type Plan struct {
	ID       PlanID
	Revision RevisionID
	// Environments are all required: several mean every one must pass.
	Environments []Environment
	// Targets are in dependency order.
	Targets    []PlanTarget
	Exclusions []Exclusion
	Unresolved []Unresolved
	// Omitted are the changed targets --only left out. The check doesn't
	// build them, but submission still requires them: a narrowed check
	// never shrinks what submit requires (Design v3 §7).
	Omitted []PlanTarget `json:",omitempty"`
	// Only and Also record the selection as given, for status and the PR.
	Only      []string
	Also      []string
	Tests     TestPolicy
	CreatedAt time.Time
}

// Runnable reports whether the plan may be checked: nothing is unresolved
// and there is something to build somewhere.
func (p Plan) Runnable() bool { return len(p.Unresolved) == 0 && len(p.Targets) > 0 }

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
// roles, and dependencies that come earlier in the order.
func (p Plan) Validate() error {
	switch {
	case p.ID == "" || p.Revision == "":
		return invalid("plan %q has no ID or revision", p.ID)
	case len(p.Environments) == 0:
		return invalid("plan %s has no environment", p.ID)
	case p.CreatedAt.IsZero():
		return invalid("plan %s has no creation time", p.ID)
	}
	switch p.Tests {
	case TestsDeclared, TestsRequired, TestsSkip:
	default:
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
		for _, dependency := range target.DependsOn {
			if !seen[dependency] {
				return invalid("plan %s target %s depends on %s, which does not come before it", p.ID, target.ID, dependency)
			}
		}
		seen[target.ID] = true
	}
	for _, target := range p.Omitted {
		if target.ID == "" || seen[target.ID] || target.Role != Changed {
			return invalid("plan %s omits %q, which is planned, repeated, or not a changed target", p.ID, target.ID)
		}
		seen[target.ID] = true
	}
	return nil
}
