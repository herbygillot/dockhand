// Package planning turns a target and an invocation's parameters into a
// plan: resolve the selector, open an evaluator, open a fetcher if this
// intent fetches, fill the parameters, choose the planner from the
// catalogue, and run it.
//
// It exists because an adversarial pass asked where those six steps
// live and the answer was nowhere. They are one contiguous function in
// the CLI today (internal/cmd/intent.go:120-184), which is the second
// orchestrator in miniature; and putting them in app would give the
// application layer the whole MacPorts stack — an evaluator, a fetcher,
// a port handle, a tree — to hold on behalf of one step.
//
// So this is the producer and `change` is the consumer: planning makes
// a *plan.Plan, change turns it into Prepared content and mints it.
// Nothing here decides how far a change goes or whether it verifies.
package planning

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// ErrNoIntent is Plan's refusal of a verb the catalogue does not carry.
// It is a sentinel and not a sentence because the caller that meets it
// is the composition root wiring a verb that does not exist, which is a
// build-time mistake showing up at run time, and nothing downstream may
// recover the fact by reading words (rule 6).
var ErrNoIntent = errors.New("planning: no intent in the catalogue answers to this verb")

// ErrNoEvaluator is Plan's refusal of a Planner with no Eval. A plan is
// made by asking a port what it currently evaluates to, so a planner
// with nothing to ask cannot produce a plan and must not produce an
// empty one: the zero value here would otherwise mean both "nothing to
// plan" and "nobody wired an evaluator" (rule 7).
var ErrNoEvaluator = errors.New("planning: no evaluator was supplied; nothing can be asked of the port")

// Planner is deliberately not called Deps, and it is not one bag shared
// with anything else: these are the services PLANNING needs and nothing
// else in the program wants together.
//
// THE TWO SEAMS THE SKETCH DECLARES ARE ALREADY IN THE TREE, which is
// this port's one adaptation and is stated rather than smuggled in. The
// sketch declares `Evaluator interface { Open(ctx, portdir) (any, error) }`
// and `Fetcher interface { Fetch(ctx, url) (string, error) }` —
// consumer-owned, one method wide, "because a plan must be testable
// without a MacPorts installation and without the network". Both of
// those already exist as exactly that: port.Oracle is transcribed from
// *eval.Evaluator's method set precisely so a scripted oracle can stand
// in, and distfile.Fetcher is the one-method seam portfetch implements
// over MacPorts' own curl. Redeclaring either here would buy nothing and
// cost an adapter at the one call site that hands them on — intent's
// Planner takes a port.Handle and a distfile.Fetcher verbatim — so the
// seams are named rather than duplicated. What the sketch is right about
// is that they are the boundary, and the depguard rule for this package
// is what holds it: nothing here may reach eval, portfetch or the net.
type Planner struct {
	// Eval is the oracle every plan is made against. Nil is refused
	// rather than tolerated, because a plan made without asking the port
	// anything is not a smaller plan, it is a different act.
	Eval port.Oracle
	// Fetch is the distfile fetcher, and it is nil on a machine or an
	// invocation that will not go to the network. A nil fetcher is
	// handed on as a nil interface for the intents that do not fetch;
	// Plan refuses to run an intent whose Definition says Fetches with
	// nothing to fetch with, because a bump that silently planned no
	// checksums would be a plan that lied about what it wrote.
	Fetch distfile.Fetcher
	// Temp is where a shadow evaluation materializes its copies. The
	// zero Root issues from the system temporary directory and owns
	// nothing, which is what a test wants.
	Temp tempdir.Root
	// Catalog is the write-intent catalogue, as data. Plan chooses from
	// it by name and never branches on which intent is running — the
	// property internal/cmd's intentAction has today and the reason the
	// registration exists at all.
	Catalog []intent.Definition
}

// Plan runs one intent over one resolved target and returns its plan.
//
// THE TARGET ARRIVES RESOLVED, and that is this port's second stated
// adaptation. The package doc's "resolve the selector" names the six
// steps as they read in internal/cmd/intent.go, where Execute has
// already expanded the selector and hands single() one tree.Target; the
// plural expansion belongs to the caller for a reason app.Survey makes
// structural — a sweep delivers targets one at a time as it plans them,
// so the thing that resolves four hundred ports is the pool, and a
// producer that resolved its own selector could only ever produce one
// plan per invocation. The Planner holds no tree and no selector
// grammar, so it could not resolve one if it wanted to.
//
// It performs the other five steps in the order internal/cmd does, and
// for the same reasons: the handle is built on the run's temp root so a
// shadow evaluation's copies are attributable; the fetcher is passed
// only to an intent whose catalogue entry says it fetches, so a revision
// bump does not open a Tcl session per revbump; --riders replaces the
// chosen planner with intent.Housekeeping, which is the ONE place this
// road does not run what the catalogue named and is still not a branch
// on WHICH intent, because every verb's housekeeping is the same change.
//
// It returns the planner's own refusal unwrapped. A *plan.Decline is the
// exit-10 family and carries its own band; wrapping it in a sentence
// here would put a second author's words in front of the planner's.
func (p Planner) Plan(ctx context.Context, verb string, target tree.Target, params intent.Params) (*plan.Plan, error) {
	def, ok := p.definition(verb)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoIntent, verb)
	}
	if p.Eval == nil {
		return nil, ErrNoEvaluator
	}
	h := port.New(target, p.Eval).WithTempDir(p.Temp)

	// The fetcher is handed on only for planners that read the network,
	// and nil stays a nil interface rather than a typed nil in disguise:
	// intent.Planner tests it for nil, and a non-nil interface holding a
	// nil pointer would pass that test and panic in the planner.
	var fetch distfile.Fetcher
	if def.Fetches && params.Riders != intent.RidersOnly {
		if p.Fetch == nil {
			return nil, fmt.Errorf("planning: %s reads the network and no fetcher was supplied", def.Name)
		}
		fetch = p.Fetch
	}

	planner, err := def.New(params)
	if err != nil {
		return nil, err
	}
	if params.Riders == intent.RidersOnly {
		// --riders makes housekeeping the whole change: the verb chose the
		// port and nothing else of it is used.
		planner = intent.Housekeeping{}
	}
	return planner.Plan(ctx, h, fetch)
}

// definition finds the catalogue entry a verb names, by its Name or by
// any of its Aliases, case-folded on the aliases' own terms: cobra
// matches a command name exactly, so this is the same lookup the adapter
// performs and not a looser one.
//
// It is a method rather than a package function because the catalogue is
// the Planner's, and a package-level registry is the global this design
// removed.
func (p Planner) definition(verb string) (intent.Definition, bool) {
	for _, def := range p.Catalog {
		if def.Name == verb {
			return def, true
		}
		for _, alias := range def.Aliases {
			if alias == verb {
				return def, true
			}
		}
	}
	return intent.Definition{}, false
}

// Verbs names every intent this catalogue answers to, in catalogue
// order, with each entry's aliases after its name.
//
// It exists so that a refusal can say what WAS available without the
// caller holding the catalogue itself, which is the shape rule 7 asks of
// a lookup that can fail: "no such verb" and "no catalogue at all" are
// two facts, and a caller that could not tell them apart would report a
// wiring gap as a typo.
func (p Planner) Verbs() []string {
	var out []string
	for _, def := range p.Catalog {
		out = append(out, def.Name)
		out = append(out, def.Aliases...)
	}
	return out
}

// String is the catalogue in one line, for a refusal's detail and for a
// test's failure message. It is not a report: report renders, and this
// is the one sentence a sentinel's %w wrapper is allowed.
func (p Planner) String() string {
	return "planning over [" + strings.Join(p.Verbs(), " ") + "]"
}
