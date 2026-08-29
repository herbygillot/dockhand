// Package classify surveys ports for tractability: for each port, can
// dockhand locate the span carrying its version, and through which style?
// The output is the empirical census the design documents have wanted
// since before any code existed — measured style coverage and a typed
// decline distribution, replacing estimates.
//
// This package is the engine only: it classifies portdirs it is handed and
// aggregates results. Walking a tree and rendering a report belong to its
// callers.
package classify

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
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
	Portdir string
	Name    string
	Outcome Outcome
	Style   portstyle.Type
	Span    text.Span
	Detail  string
}

// Port classifies a single portdir using the given evaluator.
func Port(ctx context.Context, ev *eval.Evaluator, portdir string) Result {
	r := Result{Portdir: portdir}

	vals, err := ev.Top(ctx, portdir, "")
	if err != nil {
		r.Outcome = EvalFailed
		r.Detail = err.Error()
		return r
	}
	r.Name = vals.Name

	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	if err != nil {
		r.Outcome = EvalFailed
		r.Detail = err.Error()
		return r
	}
	tree, errs := syntax.Parse(src)
	if len(errs) != 0 {
		r.Outcome = ParseFailed
		r.Detail = errs[0].Describe(src)
		return r
	}

	loc, err := portstyle.Locate(src, tree, vals, info.FieldVersion)
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

// declineOutcome maps a style decline onto the census vocabulary.
func declineOutcome(d *portstyle.Decline) Outcome {
	switch d.Type {
	case portstyle.UnknownStyle:
		return UnknownStyle
	case portstyle.NotLiteral:
		return NotLiteral
	case portstyle.FieldUnsupported:
		return EvalFailed // cannot happen for version; surfaced, not hidden
	}
	return EvalFailed
}
