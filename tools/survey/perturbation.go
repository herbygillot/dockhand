package main

import (
	"fmt"
	"reflect"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

// probePlan is deliberately the literal comparison specified for this
// experiment. In particular, all ReadOptions include build-only options.
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

type probeResult struct {
	Status                  string // same, differs, or failed
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
	return &probeSummary{Status: r.Status, Field: r.Field, ErrorCode: r.ErrorCode, Rules: r.Rules, Seconds: r.Seconds, Standins: r.Standins, FollowerBenign: r.FollowerBenign}
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
	if len(a.Options) != len(b.Options) {
		return "read-options.length"
	}
	for i, x := range a.Options {
		if x != b.Options[i] {
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
