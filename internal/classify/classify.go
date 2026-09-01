// Package classify surveys ports for tractability: for each port, can
// dockhand locate the span carrying its version, and through which style?
// The output is the empirical census the design documents have wanted
// since before any code existed — measured style coverage and a typed
// decline distribution, replacing estimates.
//
// This package is the engine only: it classifies the ports it is handed
// and aggregates results. Walking a tree and rendering a report belong
// to its callers.
package classify

import (
	"context"
	"errors"
	"regexp"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/text"
)

// Outcome is the classification of one port, covering the whole pipeline:
// evaluation, parsing, and style location each have a failure that is
// data, not an error.
type Outcome int

const (
	// Located means a style was found and corroborated; the port's version
	// is editable by span replacement.
	Located Outcome = iota
	// UnknownStyle means no known style appears in the port; the style
	// table has something to learn from this port.
	UnknownStyle
	// NotLiteral means styles exist but none is literal: the version is
	// computed, and locating it needs more than the style table.
	NotLiteral
	// Probeable is NotLiteral with at least one literal candidate: the
	// population bump's counterfactual probe can attempt. The census
	// tier is free — no probe runs here — so the name claims the
	// attempt, never the outcome.
	Probeable
	// ParseFailed means the Portfile has Tcl syntax errors.
	ParseFailed
	// EvalFailed means the port did not evaluate; nothing downstream ran.
	EvalFailed
)

func (o Outcome) String() string {
	switch o {
	case Located:
		return "located"
	case UnknownStyle:
		return "unknown style"
	case NotLiteral:
		return "not literal"
	case Probeable:
		return "not literal (probeable)"
	case ParseFailed:
		return "parse failed"
	case EvalFailed:
		return "eval failed"
	}
	return "unknown outcome"
}

// Result is one port's classification. Style and Span are meaningful only
// for Located; Detail carries the failure's diagnostic otherwise.
type Result struct {
	Target  tree.Target
	Name    string
	Outcome Outcome
	Style   portstyle.Type
	Span    text.Span
	Detail  string
	// DeclaresGoMin marks a port declaring go.toolchain_min — the Go
	// floor bump now maintains, so the census can say how many ports
	// carry one (and, with the go_toolchain.setup style count, how
	// many ARE the toolchain).
	DeclaresGoMin bool
}

// Port classifies the evaluation context a handle names: the top-level
// port, or the subport when the handle carries one.
func Port(ctx context.Context, h port.Handle) Result {
	r := Result{Target: h.Target}

	vals, err := h.Values(ctx)
	if err != nil {
		r.Outcome = EvalFailed
		r.Detail = err.Error()
		return r
	}
	r.Name = vals.Name

	src, cst, err := h.Source()
	if err != nil {
		// Unparseable and unreadable are different census facts.
		var pe *port.ParseError
		if errors.As(err, &pe) {
			r.Outcome = ParseFailed
			r.Detail = pe.Detail
		} else {
			r.Outcome = EvalFailed
			r.Detail = err.Error()
		}
		return r
	}

	r.DeclaresGoMin = goMinRE.Match(src)

	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		var d *portstyle.Decline
		if errors.As(err, &d) {
			r.Outcome = declineOutcome(d)
			r.Detail = d.Error()
			return r
		}
		r.Outcome = EvalFailed
		r.Detail = err.Error()
		return r
	}
	r.Outcome = Located
	r.Style = loc.Style
	r.Span = loc.Span
	return r
}

// goMinRE recognizes a go.toolchain_min declaration — a literal
// specifier by the PortGroup's own design, so a text scan is the
// census-honest reading.
var goMinRE = regexp.MustCompile(`(?m)^\s*go\.toolchain_min\s`)

// declineOutcome maps a style decline onto the census vocabulary.
func declineOutcome(d *portstyle.Decline) Outcome {
	switch d.Type {
	case portstyle.UnknownStyle:
		return UnknownStyle
	case portstyle.NotLiteral:
		for _, c := range d.Candidates {
			if c.Literal {
				return Probeable
			}
		}
		return NotLiteral
	case portstyle.FieldUnsupported:
		return EvalFailed // cannot happen for version; surfaced, not hidden
	}
	return EvalFailed
}
