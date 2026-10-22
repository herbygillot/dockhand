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

// ObservedKind is how an evaluation differs from its prediction.
type ObservedKind string

const (
	// Unpredicted is a change the prediction didn't make: a field of a
	// port the edit changes, or the port set, or the context.
	Unpredicted ObservedKind = "unpredicted"
	// PredictedMissing is a change the prediction made that the
	// evaluation didn't show as predicted.
	PredictedMissing ObservedKind = "predicted-missing"
	// ProtectedMoved is a port the prediction left as it was that moved.
	ProtectedMoved ObservedKind = "protected-moved"
)

// Observed is one way an evaluation differs from its prediction: of what
// kind, which port, and which field ("revision", a compared field's
// name, or empty for the port set or the context).
type Observed struct {
	Kind  ObservedKind
	Port  string
	Field string
}

// Observe is the report of an evaluation against the prediction: its
// context, the port set, each port's revision, and every field Compare
// compares, each side normalized by its own root; any difference is
// unexpected, and found as one of its kinds (Report.Observed), against
// before as the prediction left it or changed it.
func (p Prediction) Observe(before, after macports.Snapshot) Report {
	report := Report{Before: before, After: after, ExpectedChanges: p.Expected, UnexpectedChanges: []string{}}
	found := func(kind ObservedKind, port, field, words string) {
		report.Observed = append(report.Observed, Observed{Kind: kind, Port: port, Field: field})
		report.UnexpectedChanges = append(report.UnexpectedChanges, words)
	}
	if before.Platform != after.Platform || !reflect.DeepEqual(before.Target, after.Target) {
		found(Unpredicted, "", "", "evaluation context changed")
	}
	for _, name := range slices.Sorted(maps.Keys(after.Ports)) {
		if _, ok := p.Ports[name]; !ok {
			found(Unpredicted, name, "", name+": port set changed")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Ports)) {
		expected := p.Ports[name]
		next, ok := after.Ports[name]
		if !ok {
			found(PredictedMissing, name, "", name+": port set changed")
			continue
		}
		was, had := before.Ports[name]
		old := ComparablePort(was, before.Root)
		unchanged := had && old.Revision == expected.Revision && len(differences(old, expected)) == 0
		kind := func(field string, changed bool) ObservedKind {
			switch {
			case unchanged:
				return ProtectedMoved
			case changed:
				return PredictedMissing
			}
			return Unpredicted
		}
		if next.Revision != expected.Revision {
			found(kind("revision", had && old.Revision != expected.Revision), name, "revision", fmt.Sprintf("%s.revision: expected %d, got %d", name, expected.Revision, next.Revision))
		}
		predicted := map[string]bool{}
		for _, change := range differences(old, expected) {
			predicted[change.Field] = true
		}
		for _, change := range differences(expected, ComparablePort(next, after.Root)) {
			found(kind(change.Field, predicted[change.Field]), name, change.Field, name+"."+change.Field+" changed")
		}
	}
	slices.Sort(report.UnexpectedChanges)
	return report
}
