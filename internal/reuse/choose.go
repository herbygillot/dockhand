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
// says it needs built first there, and its earlier builds there, newest
// first.
type Target struct {
	model.PlanTarget
	DependsOn []model.TargetID
	Earlier   []Candidate
}

// Choose decides which of an environment's targets reuse an earlier build
// (decision 28), given the environment's identity now and the revision's
// trees by path (Paths). A target reuses its newest earlier build whose
// result stands, by the check's test policy (stands), and whose inputs are
// what it would read now (Current). The rest build, and so does any
// target one of them needs: a reused build isn't in the guest to be
// installed, so MacPorts would give its dependents upstream's archive of
// the port as master has it, or build it unrecorded. A target needs those
// the plan says it does, and those active as its newest earlier build ran,
// which it may reach through ports the branch doesn't change.
//
// The chosen builds are returned by target; the targets without one
// build.
func Choose(targets []Target, identity string, trees map[string]model.ObjectID, stands func(model.TargetResult) bool) map[model.TargetID]Candidate {
	chosen := map[model.TargetID]Candidate{}
	for _, target := range targets {
		for _, c := range target.Earlier {
			if stands(c.Result) && Current(c.Inputs, identity, target.PlanTarget, trees) {
				chosen[target.ID] = c
				break
			}
		}
	}
	// A target that builds takes what it needs with it, and that what it
	// needs, until nothing more is taken.
	for taken := true; taken; {
		taken = false
		for _, target := range targets {
			if _, reused := chosen[target.ID]; reused {
				continue
			}
			for _, need := range needs(target, targets) {
				if _, reused := chosen[need]; reused {
					delete(chosen, need)
					taken = true
				}
			}
		}
	}
	return chosen
}

// needs are the targets one needs installed as it builds: those the plan
// names, and those among the ports active as its newest earlier build ran.
// A port's name is its target's, in any case, as MacPorts reads names.
func needs(target Target, targets []Target) []model.TargetID {
	needs := slices.Clone(target.DependsOn)
	if len(target.Earlier) == 0 {
		return needs
	}
	for _, port := range target.Earlier[0].Inputs.Active {
		for _, other := range targets {
			if strings.EqualFold(port.Name, string(other.ID)) && !slices.Contains(needs, other.ID) {
				needs = append(needs, other.ID)
			}
		}
	}
	return needs
}
