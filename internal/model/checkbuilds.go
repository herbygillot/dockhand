package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// CheckBuilds names the builds available in one environment and the
// builds each dependent is already known to consume (#236).
type CheckBuilds struct {
	Targets      []PlanTarget
	Dependencies map[TargetID][]TargetID
}

// Resolve says which build of port satisfies dependent. An explicit
// dependency selects it first, otherwise the default build does, or the
// sole configured build. Two configured builds with no selection leave
// the plan unresolved: ordering after both cannot select an image (#236).
func (b CheckBuilds) Resolve(dependent TargetID, port string) (TargetID, error) {
	var candidates, selected []TargetID
	var ordinary, configured TargetID
	for _, target := range b.Targets {
		name := target.Target.Name
		if target.Target.Subport != "" {
			name = target.Target.Subport
		}
		if target.ID == dependent || !strings.EqualFold(name, port) {
			continue
		}
		candidates = append(candidates, target.ID)
		if slices.Contains(b.Dependencies[dependent], target.ID) {
			selected = append(selected, target.ID)
		}
		if len(target.Target.Variants) == 0 {
			ordinary = target.ID
		} else if configured == "" {
			configured = target.ID
		}
	}
	if len(selected) == 1 {
		return selected[0], nil
	}
	if len(selected) == 0 {
		if ordinary != "" {
			return ordinary, nil
		}
		if len(candidates) == 0 {
			return "", nil
		}
		if len(candidates) == 1 {
			return candidates[0], nil
		}
	}
	return "", fmt.Errorf("%s depends on %s, but which build it consumes is unresolved: %v; select one configuration with --variants '%s', or for a baseline use --baseline --only '%s,%s'", dependent, port, candidates, configured, dependent, configured)
}

// Consumed names the build of each in-check port active as a dependent
// built. A planned prerequisite missing from the active ports cannot be
// established as consumed, and leaves the inputs unknown (#236).
func (b CheckBuilds) Consumed(dependent TargetID, active []ActivePort) (map[string]TargetID, error) {
	consumed := map[string]TargetID{}
	found := map[TargetID]bool{}
	for _, port := range active {
		id, err := b.Resolve(dependent, port.Name)
		if err != nil {
			return nil, err
		}
		if id != "" {
			consumed[strings.ToLower(port.Name)] = id
			found[id] = true
		}
	}
	for _, id := range b.Dependencies[dependent] {
		if !found[id] {
			return nil, fmt.Errorf("%s's consumption of %s was not recorded", dependent, id)
		}
	}
	return consumed, nil
}

// Planned names the builds a dependent is planned to consume before it
// runs. Missing target metadata or ambiguous edges cannot establish a
// selection, even for a request joining itself (#236).
func (b CheckBuilds) Planned(dependent TargetID) (map[string]TargetID, error) {
	var active []ActivePort
	for _, id := range b.Dependencies[dependent] {
		at := slices.IndexFunc(b.Targets, func(t PlanTarget) bool { return t.ID == id })
		if at < 0 {
			return nil, fmt.Errorf("%s's prerequisite %s is not in the check", dependent, id)
		}
		target := b.Targets[at].Target
		name := target.Name
		if target.Subport != "" {
			name = target.Subport
		}
		active = append(active, ActivePort{Name: name})
	}
	return b.Consumed(dependent, active)
}

// SameInputs reports whether the recorded in-check builds are the ones
// this check would consume. Old inputs with no such record stand only
// where no in-check port was consumed (#236).
func (b CheckBuilds) SameInputs(dependent TargetID, inputs TargetInputs) bool {
	if inputs.NotReusable != "" {
		return false
	}
	expected, err := b.Consumed(dependent, inputs.Active)
	return err == nil && maps.Equal(inputs.Targets, expected)
}

// CheckBuilds are the builds the plan makes available in environment.
func (p Plan) CheckBuilds(environment Environment) CheckBuilds {
	build, _ := p.In(environment)
	selection := CheckBuilds{Dependencies: build.Dependencies}
	for _, target := range p.Targets {
		if build.Builds(target.ID) {
			selection.Targets = append(selection.Targets, target)
		}
	}
	return selection
}
