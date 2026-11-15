package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
)

// probePlan retains all observed values. Equality excludes exactly the
// options the follower accepts as benign sinks; their changes stay visible.
type probePlan struct {
	Distfiles                       []macports.Distfile
	Checksums, RevisionDeclarations [][]string
	Revision                        int
	Options                         []probeOption
}

type probeOption struct {
	Name                  string
	Present, ErrorPresent bool
	Value, Error          string
}

const probeVersion = 2
const (
	probeSame           = "same"
	probeBuildOnlyMoved = "same, build-only moved"
	probeDiffers        = "differs"
	probeFailed         = "failed"
	probeNotPerturbed   = "not perturbed"
)

type probeResult struct {
	Version                 int
	Status                  string
	BuildOnlyMoved          []string `json:",omitempty"`
	SubstitutionsKnown      bool
	Field, ErrorCode, Error string `json:",omitempty"`
	Rules                   []observe.Rule
	Seconds                 float64
	Standins                int
	FollowerBenign          bool
	Before                  probePlan
	After                   *probePlan                `json:",omitempty"`
	Observation             *macports.PortObservation `json:",omitempty"`
}

type probeSummary struct {
	Version                  int
	BuildOnlyMoved           []string `json:",omitempty"`
	SubstitutionsKnown       bool
	Status, Field, ErrorCode string
	Rules                    []observe.Rule
	Seconds                  float64
	Standins                 int
	FollowerBenign           bool
}

func (r *probeResult) summary() *probeSummary {
	if r == nil {
		return nil
	}
	return &probeSummary{Version: r.Version, BuildOnlyMoved: r.BuildOnlyMoved, SubstitutionsKnown: r.SubstitutionsKnown, Status: r.Status, Field: r.Field, ErrorCode: r.ErrorCode, Rules: r.Rules, Seconds: r.Seconds, Standins: r.Standins, FollowerBenign: r.FollowerBenign}
}

func makeProbePlan(info macports.PortInfo, p macports.PortObservation) probePlan {
	plan := probePlan{Revision: info.Revision, Distfiles: []macports.Distfile{}, Checksums: [][]string{}, RevisionDeclarations: [][]string{}}
	for _, f := range p.Distfiles {
		plan.Distfiles = append(plan.Distfiles, macports.Distfile{Name: f.Name, URLs: append([]string{}, f.URLs...)})
	}
	for _, d := range p.Declarations {
		switch d.Command {
		case "checksums":
			plan.Checksums = append(plan.Checksums, append([]string{}, d.Values...))
		case "revision":
			plan.RevisionDeclarations = append(plan.RevisionDeclarations, append([]string{}, d.Values...))
		}
	}
	for _, name := range macports.ReadOptions {
		v, ok := info.Options[name]
		e, failed := info.OptionErrors[name]
		plan.Options = append(plan.Options, probeOption{Name: name, Present: ok, Value: v, ErrorPresent: failed, Error: e})
	}
	return plan
}

// firstProbeDifference defines the comparison once, in a stable order.
// Frames, host events, execution records and the ledger never enter a plan.
func firstProbeDifference(a, b probePlan) string {
	if len(a.Distfiles) != len(b.Distfiles) {
		return "distfiles.length"
	}
	for i, x := range a.Distfiles {
		if x.Name != b.Distfiles[i].Name {
			return fmt.Sprintf("distfiles[%d].name", i)
		}
		if !reflect.DeepEqual(x.URLs, b.Distfiles[i].URLs) {
			return fmt.Sprintf("distfiles[%d].locations", i)
		}
	}
	if !reflect.DeepEqual(a.Checksums, b.Checksums) {
		return "checksums.declarations"
	}
	if !reflect.DeepEqual(a.RevisionDeclarations, b.RevisionDeclarations) {
		return "revision.declarations"
	}
	if a.Revision != b.Revision {
		return "revision"
	}
	actedA, actedB := actedProbeOptions(a.Options), actedProbeOptions(b.Options)
	if len(actedA) != len(actedB) {
		return "read-options.length"
	}
	for i, x := range actedA {
		if x != actedB[i] {
			return "read-options." + x.Name
		}
	}
	return ""
}

func probeRules(c *toleranceCapture) ([]observe.Rule, bool) {
	seen := map[observe.Rule]bool{}
	var rules []observe.Rule
	benign := false
	for _, a := range c.Pair.On.Accesses {
		if a.Rule != observe.RuleBuildDefault && a.Rule != observe.RuleBaseLookup {
			continue
		}
		if !seen[a.Rule] {
			rules = append(rules, a.Rule)
			seen[a.Rule] = true
		}
		for _, f := range a.Followers {
			benign = benign || f.Benign
		}
	}
	return rules, benign
}

// Infrastructure failure cannot establish a plan difference. A semantic
// evaluation failure after a complete baseline is a difference, retaining
// its code as well as the field, as the spec's safe-direction rule requires.
func probeFailure(r *probeResult, err error) {
	r.ErrorCode = portedit.Problem("evaluation", err).Code
	r.Error = err.Error()
	r.Status = "differs"
	r.Field = "evaluation"
	if errors.Is(err, rpc.ErrBrokenSession) || errors.Is(err, rpc.ErrHandshake) || errors.Is(err, rpc.ErrNoAnswer) || errors.Is(err, macports.ErrStartup) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		r.Status = "failed"
		r.Field = ""
	}
}

func actedProbeOptions(options []probeOption) []probeOption {
	var out []probeOption
	for _, o := range options {
		if observe.ActedOption(o.Name) {
			out = append(out, o)
		}
	}
	return out
}

// Retain names of every excluded option whose presence, value or error
// changes. The full before/after values remain in the captured plans.
func buildOnlyProbeChanges(a, b probePlan) []string {
	right := map[string]probeOption{}
	for _, o := range b.Options {
		right[o.Name] = o
	}
	seen := map[string]bool{}
	var moved []string
	for _, o := range a.Options {
		seen[o.Name] = true
		other, ok := right[o.Name]
		if !observe.ActedOption(o.Name) && (!ok || o != other) {
			moved = append(moved, o.Name)
		}
	}
	for _, o := range b.Options {
		if !seen[o.Name] && !observe.ActedOption(o.Name) {
			moved = append(moved, o.Name)
		}
	}
	return moved
}

// Called only after a completed second observation, whose substitution
// count is known. Errors keep their own semantic/infrastructure classes.
func classifyProbePlans(r *probeResult) {
	r.SubstitutionsKnown = true
	r.Field = firstProbeDifference(r.Before, *r.After)
	r.BuildOnlyMoved = buildOnlyProbeChanges(r.Before, *r.After)
	switch {
	case r.Standins == 0:
		r.Status = probeNotPerturbed
	case r.Field != "":
		r.Status = probeDiffers
	case len(r.BuildOnlyMoved) > 0:
		r.Status = probeBuildOnlyMoved
	default:
		r.Status = probeSame
	}
}
