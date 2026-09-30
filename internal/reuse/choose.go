package reuse

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Candidate is an earlier build that may stand for a target's build now:
// its result, what it read, and the execution that built it.
type Candidate struct {
	Result model.TargetResult
	Inputs model.TargetInputs
	Origin model.GuestExecution
}

// Target is one target an environment has left to build: what the plan
// says it needs built first there, the source its build is expected to
// fetch where it is fetched with Git there, and its earlier builds there,
// newest first.
type Target struct {
	model.PlanTarget
	DependsOn []model.TargetID
	Git       *model.GitSource
	Earlier   []Candidate
}

// Choice is what an environment's targets reuse: an earlier build for each
// target that reuses one, and among them those a target that builds needs,
// which the guest installs from their kept archives.
type Choice struct {
	Reused   map[model.TargetID]Candidate
	Installs []model.TargetID
}

// Choose decides which of an environment's targets reuse an earlier build
// (decision 28), given the environment's identity now and the revision's
// trees by path (Paths). A target reuses its newest earlier build whose
// result stands, by the check's test policy (stands), and whose inputs are
// what it would read now (Current). The rest build.
//
// A target fetched with Git is reused only for the commit its plan expects
// (Current), and what was built against another commit's build of it, or
// against a build in that case in turn, builds again too
// (AgainstOtherSources).
//
// A reused target that one that builds needs (Needs) must be in the guest.
// Where its archive is kept (available), the guest installs it from that
// archive; otherwise it builds too, since MacPorts would give its
// dependents upstream's archive of the port as master has it, or build it
// unrecorded. A target that builds for that reason takes what it needs in
// turn.
func Choose(targets []Target, identity string, trees map[string]model.ObjectID, stands func(model.TargetResult) bool, available func(Candidate) bool) Choice {
	choice := Choice{Reused: map[model.TargetID]Candidate{}}
	for _, target := range targets {
		for _, c := range target.Earlier {
			if stands(c.Result) && Current(c.Inputs, identity, target.PlanTarget, target.Git, trees) {
				choice.Reused[target.ID] = c
				break
			}
		}
	}
	for id := range AgainstOtherSources(targets, choice.Reused) {
		delete(choice.Reused, id)
	}
	ids := make([]model.TargetID, len(targets))
	for i, target := range targets {
		ids[i] = target.ID
	}
	// A target that builds takes what it needs and can't be installed,
	// and that what it needs, until nothing more is taken.
	for taken := true; taken; {
		taken = false
		for _, target := range targets {
			if _, reused := choice.Reused[target.ID]; reused {
				continue
			}
			for _, need := range Needs(target, ids) {
				if c, reused := choice.Reused[need]; reused && !available(c) {
					delete(choice.Reused, need)
					taken = true
				}
			}
		}
	}
	for _, target := range targets {
		if _, reused := choice.Reused[target.ID]; reused {
			continue
		}
		for _, need := range Needs(target, ids) {
			if _, reused := choice.Reused[need]; reused && !slices.Contains(choice.Installs, need) {
				choice.Installs = append(choice.Installs, need)
			}
		}
	}
	return choice
}

// Needs are the targets, among those given, that one needs installed as it
// builds: those the plan names, and those among the ports active as its
// newest earlier build ran, which it may reach through ports the branch
// doesn't change. A port's name is its target's, in any case, as MacPorts
// reads names.
func Needs(target Target, among []model.TargetID) []model.TargetID {
	if len(target.Earlier) == 0 {
		return slices.Clone(target.DependsOn)
	}
	return needs(target.DependsOn, target.Earlier[0].Inputs.Active, among)
}

// needs are the targets, among those given, that a build needs: those the
// plan names, and those among the ports active as it ran.
func needs(dependsOn []model.TargetID, active []model.ActivePort, among []model.TargetID) []model.TargetID {
	needed := slices.Clone(dependsOn)
	for _, port := range active {
		for _, other := range among {
			if strings.EqualFold(port.Name, string(other)) && !slices.Contains(needed, other) {
				needed = append(needed, other)
			}
		}
	}
	return needed
}

// AgainstOtherSources are the targets whose chosen earlier build (chosen)
// was built against another source of a Git-fetched target than its plan
// expects now (batch 20): the build had active an archive of a Git-fetched
// target it needs that no build of the commit expected made
// (builtAgainst), or it needs a target whose chosen build is in that case,
// since it read that build in turn. What a build needs is what the plan
// says (DependsOn) and the ports active as it ran, among targets; a build
// that didn't say which were active can't be established to have had the
// commit expected. The same tree of a Git-fetched port isn't the same
// port, as the same tree of any other is, so such a build stands for none
// now: reuse builds it again (Choose), and an earlier check's result of it
// isn't evidence for the commit expected now.
func AgainstOtherSources(targets []Target, chosen map[model.TargetID]Candidate) map[model.TargetID]bool {
	ids := make([]model.TargetID, len(targets))
	byID := map[model.TargetID]Target{}
	for i, target := range targets {
		ids[i], byID[target.ID] = target.ID, target
	}
	against := map[model.TargetID]bool{}
	for taken := true; taken; {
		taken = false
		for _, target := range targets {
			c, ok := chosen[target.ID]
			if !ok || against[target.ID] {
				continue
			}
			for _, need := range needs(target.DependsOn, c.Inputs.Active, ids) {
				if dependency := byID[need]; against[need] || dependency.Git != nil && !builtAgainst(c, dependency) {
					against[target.ID], taken = true, true
					break
				}
			}
		}
	}
	return against
}

// builtAgainst reports whether an earlier build had active an archive of a
// Git-fetched target that a build of it from the commit its plan expects
// now made: one of its earlier builds that recorded fetching that commit.
func builtAgainst(c Candidate, dependency Target) bool {
	for _, port := range c.Inputs.Active {
		if !strings.EqualFold(port.Name, string(dependency.ID)) || port.Archive == "" {
			continue
		}
		for _, earlier := range dependency.Earlier {
			if earlier.Result.Archive == port.Archive && dependency.Git.BuiltBy(earlier.Inputs.Fetched) {
				return true
			}
		}
	}
	return false
}
