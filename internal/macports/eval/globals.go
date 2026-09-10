package eval

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Globals reports every scalar global the worker interpreter holds after
// evaluating one context, name to value.
//
// It is a different kind of read from Options. Options asks for names
// the caller already knows; this asks the interpreter what it ended up
// with. Port options are plain Tcl globals in port1.0, so an evaluated
// Portfile leaves behind a complete record of every name that took part
// — including names no search of the Portfile's text could find, because
// a PortGroup set them or they were composed from the subport's own
// name.
//
// That is the whole reason it exists. Told that no candidate span in a
// Portfile drives its version, a caller cannot tell "the value is
// computed in a way we cannot follow" from "the carrier is somewhere
// else entirely". This answers the second case by name.
//
// Long values and arrays are not reported: arrays are namespaced
// machinery rather than options, and the values this serves are version
// fragments, not prose.
func (e *Evaluator) Globals(ctx context.Context, portdir, subport string, variants info.VariantSet) (map[string]string, error) {
	reply, err := e.sess.Call(ctx, "globals", portdir, subport, variationsArg(variants))
	if err != nil {
		return nil, fmt.Errorf("eval: globals of %s: %w", portdir, err)
	}
	fields, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return nil, fmt.Errorf("eval: globals of %s: malformed reply %q: %w", portdir, reply, errs[0])
	}
	out := make(map[string]string, len(fields))
	for name, raw := range fields {
		out[name] = syntax.ListValue(raw)
	}
	return out, nil
}
