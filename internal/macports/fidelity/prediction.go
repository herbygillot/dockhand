package fidelity

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Prediction is what an edit says it changes, before its evaluation is
// read: every port's expected state, and why each change is expected. The
// evaluation after the edit must be the prediction, and nothing more
// (strict fidelity, v0.5.0 part 3): each check of an edit makes one, where
// each built its own expectation inline and compared it its own way.
type Prediction struct {
	// Ports are each port's expected state, comparable (ComparablePort):
	// as the evaluation before the edit had it, normalized by its root,
	// with the edit's changes made.
	Ports map[string]macports.PortInfo
	// Expected are the changes predicted, in words, as the report says
	// them.
	Expected []string
}

// Predict is the prediction of an edit that changes nothing: every port
// as before has it.
func Predict(before macports.Snapshot) Prediction {
	p := Prediction{Ports: make(map[string]macports.PortInfo, len(before.Ports))}
	for name, port := range before.Ports {
		p.Ports[name] = ComparablePort(port, before.Root)
	}
	return p
}

// Port is a port's predicted state, its maps its own to change.
func (p Prediction) Port(name string) macports.PortInfo {
	port := p.Ports[name]
	port.Options = maps.Clone(port.Options)
	if port.Options == nil {
		port.Options = map[string]string{}
	}
	return port
}

// Expect predicts a port's state, saying why.
func (p *Prediction) Expect(name string, port macports.PortInfo, why ...string) {
	p.Ports[name] = port
	p.Expected = append(p.Expected, why...)
}

// Observe is the report of an evaluation against the prediction: its
// context, the port set, each port's revision, and every field Compare
// compares, each side normalized by its own root; any difference is
// unexpected.
func (p Prediction) Observe(before, after macports.Snapshot) Report {
	report := Report{Before: before, After: after, ExpectedChanges: p.Expected, UnexpectedChanges: []string{}}
	if before.Platform != after.Platform || !reflect.DeepEqual(before.Target, after.Target) {
		report.UnexpectedChanges = append(report.UnexpectedChanges, "evaluation context changed")
	}
	for name := range after.Ports {
		if _, ok := p.Ports[name]; !ok {
			report.UnexpectedChanges = append(report.UnexpectedChanges, name+": port set changed")
		}
	}
	for name, expected := range p.Ports {
		next, ok := after.Ports[name]
		if !ok {
			report.UnexpectedChanges = append(report.UnexpectedChanges, name+": port set changed")
			continue
		}
		if next.Revision != expected.Revision {
			report.UnexpectedChanges = append(report.UnexpectedChanges, fmt.Sprintf("%s.revision: expected %d, got %d", name, expected.Revision, next.Revision))
		}
		report.UnexpectedChanges = append(report.UnexpectedChanges, Compare(name, expected, ComparablePort(next, after.Root))...)
	}
	slices.Sort(report.UnexpectedChanges)
	return report
}
