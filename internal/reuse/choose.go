package reuse

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Candidate is an earlier build that may stand for a target's build now:
// its result, what it read, and the execution that built it, with the
// commit the GitHub tag its archive was fetched by named when that check
// was planned (EnvironmentPlan.Tags), where it recorded one.
type Candidate struct {
	Result model.TargetResult
	Inputs model.TargetInputs
	Origin model.GuestExecution
	Tagged *model.GitSource
}

// Target is one target an environment has left to build: what the plan
// says it needs built first there, the source its build is expected to
// fetch where it is fetched with Git there, the GitHub tag its archive is
// fetched by and the commit it names now, and its earlier builds there,
// newest first.
type Target struct {
	model.PlanTarget
	DependsOn []model.TargetID
	Git       *model.GitSource
	Tag       *model.GitSource
	Earlier   []Candidate
	// TagShownMoved is a move of the target's tag that a build outside
	// Earlier shows, as another earlier check's in evidence, which a
	// reading that isn't known doesn't stand beside (unseated).
	TagShownMoved bool
}

// TagMoved reports whether the GitHub tag a target's archive is fetched by
// names another commit now than when an earlier build's check was planned
// (D-N5): the archive fetched now isn't the one it built, though the
// Portfile is the same. It is false where either commit isn't known, as
// for a build before plans recorded tags, and for a build at another tag,
// or of another repository: jlv 1.9.2's build, its tag v1.9.2, read as
// v1.7.1's having moved, for a revbump at 1.7.1 on another branch
// (rc7's G16, check-943).
func (t Target) TagMoved(c Candidate) bool {
	return model.SourceAgainst(c.Tagged, t.Tag) == model.SourceMoved
}

// tagMovedAny reports whether any of a target's earlier builds says its
// tag moved, or another build outside them does (TagShownMoved), which a
// build that recorded no tag, or one whose reading isn't known, can't tell
// of itself.
func (t Target) tagMovedAny() bool {
	return t.TagShownMoved || slices.ContainsFunc(t.Earlier, t.TagMoved)
}

// unseated reports whether an earlier build's tag reading stands for
// nothing now: its tag moved (TagMoved), or another build shows the tag
// moved and this one can't say which commit it built, having recorded no
// tag or one whose reading against the target's isn't known (the source
// comparison design's "reuses, unless another build of the target shows the
// tag moved"; Codex's review of #192, finding 2).
func (t Target) unseated(c Candidate) bool {
	if t.TagMoved(c) {
		return true
	}
	return t.tagMovedAny() && (c.Tagged == nil || model.SourceAgainst(c.Tagged, t.Tag) == model.SourceUnknown)
}

// Choice is what an environment's targets reuse: an earlier build for each
// target that reuses one, and among them those a target that builds needs,
// which the guest installs from their kept archives.
type Choice struct {
	Reused   map[model.TargetID]Candidate
	Installs []model.TargetID
	// Carried are, of the targets reused, those whose build stands for a
	// revision that differs from what it read only in options no build
	// reads, or moves, with each port judged (Carry).
	Carried map[model.TargetID][]model.InertPort
}

// Choose decides which of an environment's targets reuse an earlier build
// (decision 28), given the environment's identity now and what the
// revision holds by path (Objects of Paths). A target reuses its newest earlier build whose
// result stands, by the check's test policy (stands), and whose inputs are
// what it would read now (Current, with judge), and whose GitHub tag
// hasn't moved since (TagMoved); one whose check recorded no tag isn't
// reused where another build of the target says the tag moved, since it
// can't say which commit it built. The rest build.
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
func Choose(targets []Target, identity string, trees map[string]model.ObjectID, judge Judge, stands func(model.TargetResult) bool, available func(Candidate) bool) Choice {
	choice := Choice{Reused: map[model.TargetID]Candidate{}, Carried: map[model.TargetID][]model.InertPort{}}
	for _, target := range targets {
		for _, c := range target.Earlier {
			if !stands(c.Result) || target.unseated(c) {
				continue
			}
			if carried, ok := Carry(c.Inputs, c.Origin.Run, identity, target.PlanTarget, target.Git, trees, judge); ok {
				choice.Reused[target.ID] = c
				if len(carried) > 0 {
					choice.Carried[target.ID] = carried
				}
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
	for id := range choice.Carried {
		if _, reused := choice.Reused[id]; !reused {
			delete(choice.Carried, id)
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
// expects now (batch 20), or against an archive fetched by a GitHub tag
// that has moved since (builtAgainstMovedTag): the build had active an archive of a Git-fetched
// target it needs that no build of the commit expected made
// (builtAgainst), or it needs a target whose chosen build is in that case,
// since it read that build in turn. What a build needs is what the plan
// says (DependsOn) and the ports active as it ran, among targets; a build
// that didn't say which were active can't be established to have had the
// commit expected. The same tree of a Git-fetched port isn't the same
// port, as the same tree of any other is, so such a build stands for none
// now: reuse builds it again (Choose), and an earlier check's result of it
// isn't evidence for the commit expected now. Each is given with the
// dependency it was found against (Against), for its words.
func AgainstOtherSources(targets []Target, chosen map[model.TargetID]Candidate) map[model.TargetID]Against {
	ids := make([]model.TargetID, len(targets))
	byID := map[model.TargetID]Target{}
	for i, target := range targets {
		ids[i], byID[target.ID] = target.ID, target
	}
	against := map[model.TargetID]Against{}
	for taken := true; taken; {
		taken = false
		for _, target := range targets {
			c, ok := chosen[target.ID]
			if _, found := against[target.ID]; !ok || found {
				continue
			}
			for _, need := range needs(target.DependsOn, c.Inputs.Active, ids) {
				_, indirect := against[need]
				if dependency := byID[need]; indirect || dependency.Git != nil && !builtAgainst(c, dependency) || builtAgainstMovedTag(c, dependency) {
					against[target.ID], taken = Against{Dependency: need, Indirect: indirect}, true
					break
				}
			}
		}
	}
	return against
}

// Against is the dependency an earlier build was found built against
// another source of (AgainstOtherSources): Dependency, and Indirect where
// it was the dependency's own build that was, which this one read in turn.
type Against struct {
	Dependency model.TargetID
	Indirect   bool
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
			if earlier.Result.Archive == port.Archive && dependency.Git.AgainstFetched(earlier.Inputs.Fetched) == model.SourceSame {
				return true
			}
		}
	}
	return false
}

// builtAgainstMovedTag reports whether an earlier build had active an
// archive of a target whose GitHub tag has moved since that archive's
// build was planned (TagMoved), or, where the archive is no recorded
// build's, whose tag any earlier build says moved: the dependency builds
// again, from the commit the tag names now, so a build against the old
// one stands for nothing now, as one against another commit of a
// Git-fetched target doesn't (Codex's sixth review, #71).
func builtAgainstMovedTag(c Candidate, dependency Target) bool {
	if dependency.Tag == nil {
		return false
	}
	for _, port := range c.Inputs.Active {
		if !strings.EqualFold(port.Name, string(dependency.ID)) || port.Archive == "" {
			continue
		}
		recorded := false
		for _, earlier := range dependency.Earlier {
			if earlier.Result.Archive != port.Archive {
				continue
			}
			recorded = true
			if dependency.unseated(earlier) {
				return true
			}
		}
		if !recorded && dependency.tagMovedAny() {
			return true
		}
	}
	return false
}
