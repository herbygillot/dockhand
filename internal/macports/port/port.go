// Package port binds a resolved target to the oracle that gives it
// meaning: a Handle on one port, in one variant frame, through one
// evaluator. It is the pairing every caller that reads a port needs —
// planners, the census, upstream resolution — collected once so the
// (portdir, subport, variants) triple stops being spelled out at every
// call site.
//
// It mirrors the mport handle mportopen returns, with one deliberate
// difference worth stating: MacPorts' handle owns a Tcl interpreter and
// must be closed. A Handle owns nothing. Its evaluator is borrowed,
// typically from a pool, and outlives it — so a Handle is a value,
// copied and derived freely, with no Close and no lifetime of its own.
package port

import (
	"context"
	"os"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Handle addresses one evaluation context: the Portfile at Target's
// portdir, the subport Target names — the top-level port when empty —
// under Variants.
type Handle struct {
	Target   tree.Target
	Ev       *eval.Evaluator
	Variants info.VariantSet
}

// New returns a handle on a resolved target, under the default variant
// frame.
func New(target tree.Target, ev *eval.Evaluator) Handle {
	return Handle{Target: target, Ev: ev}
}

// At derives a handle on the same context at a different portdir: the
// shadow copy of a portdir, evaluated to learn exactly what an edit
// would do.
func (h Handle) At(portdir string) Handle {
	h.Target.Portdir = portdir
	return h
}

// Subport derives a handle on a sibling context in the same Portfile.
// The name is deliberately not validated here — evaluation validates
// it, and MacPorts reports a better error than a prior check could,
// naming both the port and the subport it lacks. Names from
// SubportNames or from target resolution are valid by construction.
func (h Handle) Subport(name string) Handle {
	h.Target.Subport = name
	return h
}

// WithVariants derives a handle on the same context under a different
// variant frame.
func (h Handle) WithVariants(v info.VariantSet) Handle {
	h.Variants = v
	return h
}

// Values evaluates this context and returns its state: one evaluation,
// whichever context the handle names.
func (h Handle) Values(ctx context.Context) (info.Values, error) {
	return h.Ev.Values(ctx, h.Target.Portdir, h.Target.Subport, h.Variants)
}

// Snapshot evaluates every context the Portfile defines — the
// top-level port and each subport — under this handle's variant frame.
// Totality is the point (D13): a partial snapshot would weaken every
// check built on it.
func (h Handle) Snapshot(ctx context.Context) (info.Snapshot, error) {
	return h.Ev.Snapshot(ctx, h.Target.Portdir, h.Variants)
}

// SubportNames lists the subports the Portfile defines, with a single
// evaluation and without building the full snapshot.
func (h Handle) SubportNames(ctx context.Context) ([]string, error) {
	return h.Ev.Subports(ctx, h.Target.Portdir)
}

// Options reads the named port options for this context, omitting
// options the port does not have.
func (h Handle) Options(ctx context.Context, names ...string) (map[string]string, error) {
	return h.Ev.Options(ctx, h.Target.Portdir, h.Target.Subport, h.Variants, names...)
}

// FetchInfo reports this context's fetch surface. noMirrors skips the
// MacPorts fallback mirrors — the right mode when the distfiles sought
// are for a version the mirrors cannot have yet.
func (h Handle) FetchInfo(ctx context.Context, noMirrors bool) (eval.FetchInfo, error) {
	return h.Ev.FetchInfo(ctx, h.Target.Portdir, h.Target.Subport, h.Variants, noMirrors)
}

// ParseError reports a Portfile that does not parse as Tcl. It is typed
// because "could not be read" and "did not parse" are different facts
// about a port — the census counts them apart — and only the reader
// knows which it is looking at.
type ParseError struct {
	Path   string
	Detail string
}

// Error implements the error interface.
func (e *ParseError) Error() string { return "port: " + e.Path + ": " + e.Detail }

// Source reads and parses the Portfile: the text half of the design's
// asymmetry, where Values is the evaluated half. Spans in the returned
// tree index into the returned bytes, so the two travel together. A
// Portfile that does not parse returns a *ParseError; a Portfile that
// cannot be read returns the filesystem's error.
func (h Handle) Source() ([]byte, *syntax.Script, error) {
	path, err := h.Target.Portfile()
	if err != nil {
		return nil, nil, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	cst, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, nil, &ParseError{Path: path, Detail: errs[0].Describe(src)}
	}
	return src, cst, nil
}
