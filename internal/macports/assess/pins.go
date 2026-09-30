package assess

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// A Python requirement asks something of the port that provides it: a
// noarch build passes whatever version MacPorts has of it, so a
// requirement the provider can't meet is one a build can't catch. sqlit-tui
// 1.6.4 pinned textual-fastdatatable==0.19.0, and MacPorts had 0.17.1 (the
// sshuttle run). A requirement is in question where it changed, or where
// the port that provides it did; the rest stand as the base had them.

// judged is how a requirement stands against its provider in one version
// of the tree.
type judged int

const (
	// unjudged is a requirement whose provider's version wasn't observed.
	unjudged judged = iota
	met
	unmet
	unknown
)

// pinned is a requirement in question: its provider in each version of
// the port, by the package a Python port is named for.
type pinned struct {
	requirement
	now, before string
}

// questions are the requirements in question; none where nothing is known
// of the port, whose dependencies are what provides them.
func (a *assessment) questions() []pinned {
	if a.input.Port.Name == "" {
		return nil
	}
	var found []pinned
	for _, r := range a.requirements {
		q := pinned{requirement: r, now: provider(a.input.Port, r.name), before: provider(a.input.Base, r.name)}
		if !r.changed && q.now == q.before {
			continue
		}
		found = append(found, q)
	}
	return found
}

// provider is the port a port depends on that provides a Python package,
// by the name MacPorts gives such ports; empty where none does.
func provider(port macports.PortInfo, name string) string {
	for _, dependency := range port.Dependencies {
		if provided, ok := macports.PythonPackage(dependency.Port); ok && project.NormalizeName(provided) == name {
			return dependency.Port
		}
	}
	return ""
}

// applying are a requirement's declarations that may apply to a MacPorts
// build, with the Python version its provider uses: one whose marker says
// it applies only elsewhere, as a Windows-only pin, asks nothing of
// MacPorts' port (the helper-ownership review's finding 1), and one that
// can't be told is taken as one that applies.
func applying(declarations []project.Requirement, provider string) []project.Requirement {
	python, _ := macports.PythonVersion(provider)
	var found []project.Requirement
	for _, declaration := range declarations {
		if applies, err := declaration.OnMacOS(python); err == nil && applies == project.No {
			continue
		}
		found = append(found, declaration)
	}
	return found
}

// judge says how declarations stand against a provider's observed version,
// with what it didn't meet or couldn't tell.
func (a *assessment) judge(declarations []project.Requirement, provider string, base bool) (judged, project.Requirement, Observation) {
	observation, ok := a.input.Observed[Provider{Port: provider, Base: base}]
	if !ok {
		return unjudged, project.Requirement{}, observation
	}
	for _, declaration := range declarations {
		if observation.Problem != "" {
			return unknown, declaration, observation
		}
		admits, err := project.Admits(declaration.Specifier, observation.Version)
		switch {
		case err != nil:
			return unknown, declaration, Observation{Version: observation.Version, Problem: err.Error()}
		case !admits:
			return unmet, declaration, observation
		}
	}
	return met, project.Requirement{}, observation
}

// Wanted are the observations an assessment needs that it doesn't have:
// the version MacPorts has of each port that provides a requirement in
// question, in the candidate's tree, and, where the candidate's doesn't
// meet it, in the base's, to tell whether the base met it.
func Wanted(input Input) []Provider {
	a := assessment{input: input, counted: map[string][2]int{}, seen: map[string]bool{}}
	for _, pair := range input.Pairs {
		a.pair(pair)
	}
	var wanted []Provider
	want := func(provider Provider) {
		if _, ok := input.Observed[provider]; !ok && !contains(wanted, provider) {
			wanted = append(wanted, provider)
		}
	}
	for _, q := range a.questions() {
		now := applying(q.requirement.now, q.now)
		if q.now == "" || len(now) == 0 {
			continue
		}
		judgedNow, _, _ := a.judge(now, q.now, false)
		switch {
		case judgedNow == unjudged:
			want(Provider{Port: q.now})
		case judgedNow == unmet && q.before != "" && len(applying(q.requirement.before, q.before)) > 0:
			want(Provider{Port: q.before, Base: true})
		}
	}
	return wanted
}

func contains(providers []Provider, provider Provider) bool {
	for _, p := range providers {
		if p == provider {
			return true
		}
	}
	return false
}

// pins are what the requirements in question ask of the ports that
// provide them.
//   - A provider whose version doesn't meet a requirement holds, unless it
//     didn't at the base either, which is said and holds nothing.
//   - One whose version couldn't be told holds, since a passing build may
//     not settle it (batch 19).
//   - A requirement no dependency's name matches is said, holding nothing,
//     since a port needn't be named for its package (py313-yaml is
//     PyYAML), unless the base's Portfile depended on a port that did and
//     the candidate's doesn't, which holds.
func (a *assessment) pins() []model.UpstreamChange {
	var found []model.UpstreamChange
	for _, q := range a.questions() {
		finding := model.UpstreamChange{Kind: "dependency", Path: q.manifest, Subject: q.name, Class: model.Introduced}
		if q.now == "" {
			now := applying(q.requirement.now, "")
			if len(now) == 0 {
				continue
			}
			wanted := spelled(q.name, now[0])
			switch {
			case q.before != "":
				finding.Rule, finding.Hold = ProviderRemoved, true
				finding.Message = fmt.Sprintf("upstream: %s requires %s, which %s provided at the base, and the Portfile no longer depends on it", q.manifest, wanted, q.before)
			default:
				// In question with no provider on either side, it changed.
				finding.Rule = ProviderUnresolved
				finding.Message = fmt.Sprintf("upstream: %s requires %s, and no port the Portfile depends on is named for it", q.manifest, wanted)
			}
			found = append(found, finding)
			continue
		}
		now := applying(q.requirement.now, q.now)
		if len(now) == 0 {
			continue
		}
		verdict, declaration, observation := a.judge(now, q.now, false)
		switch verdict {
		case unjudged:
			finding.Rule, finding.Hold = RequirementUnknown, true
			finding.Message = fmt.Sprintf("upstream: couldn't tell whether MacPorts' %s meets %s's %s: its version wasn't read", q.now, q.manifest, spelled(q.name, now[0]))
		case unknown:
			finding.Rule, finding.Hold = RequirementUnknown, true
			finding.Message = fmt.Sprintf("upstream: couldn't tell whether MacPorts' %s meets %s's %s: %s", q.now, q.manifest, spelled(q.name, declaration), observation.Problem)
		case unmet:
			finding.Rule, finding.Hold = RequirementUnmet, true
			finding.Message = fmt.Sprintf("upstream: %s requires %s, which MacPorts' %s %s doesn't meet", q.manifest, spelled(q.name, declaration), q.now, observation.Version)
			switch a.baseline(q) {
			case unmet:
				finding.Class, finding.Hold = model.Present, false
				finding.Message += ", as the base's didn't either"
			case unjudged, unknown:
				finding.Class = model.UnknownBaseline
			}
		default:
			continue
		}
		found = append(found, finding)
	}
	return found
}

// baseline is how the base stood on a requirement the candidate's
// provider doesn't meet: met where the base didn't require it, or didn't
// depend on a port that provides it.
func (a *assessment) baseline(q pinned) judged {
	before := applying(q.requirement.before, q.before)
	if q.before == "" || len(before) == 0 {
		return met
	}
	verdict, _, _ := a.judge(before, q.before, true)
	return verdict
}

// spelled is a requirement as a message names it: its name, and its
// specifier where it has one.
func spelled(name string, requirement project.Requirement) string {
	return strings.TrimSpace(name + " " + requirement.Specifier)
}
