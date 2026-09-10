package bump

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// elsewhere asks the INTERPRETER where a version came from, for a port
// whose carrier no reading of the text could find.
//
// The two failures behind that decline are different and read
// identically today. In one, the version is composed by a transform
// dockhand will not invert — a [string map], a split — and the carrier
// is right there in the file. In the other the carrier is not in the
// file at all: a PortGroup set it, or it was cut out of the subport's
// own name. "The value is computed rather than written; edit what
// computes it" is unhelpful advice for the first and false advice for
// the second.
//
// The interpreter tells them apart without being asked to interpret
// anything. Port options are plain Tcl globals, so a finished
// evaluation holds every name that took part, whatever set it. A global
// whose value is a piece of the version and is NOT the literal of any
// span the locator considered is a carrier dockhand cannot write —
// stated by name, with its value, so the user knows what to go and
// edit.
//
// WHAT THE INTERPRETER HOLDS IS MOSTLY NOT THE PORT'S. Base fills an
// evaluation with its own names, and a version shaped like a date is
// inside a great many of them: measured over 1131 computed-version
// ports, a bare substring test names egid = "20" for every version
// beginning 20, os.major = "25" for every 25, lint_portsystem = "1.0"
// for a version carrying 1.0 anywhere. Not one of those is a carrier
// and every one of them is advice to go and edit the wrong thing.
//
// So the names every evaluation holds are subtracted. A Portfile that
// says nothing is evaluated in the same session, and what it leaves
// behind is base's furniture; what remains is what THIS port
// introduced. The method is the one the rest of this file uses — learn
// by difference, not by pattern — and it needs no list of base's
// globals to be kept in step with base.
//
// It decides nothing. Every answer is a sentence in a refusal that has
// already been made; a port with no evaluator, an evaluator that errors,
// and a port whose carrier really is local all return the empty string
// and leave the standard remedy standing.
func elsewhere(ctx context.Context, h port.Handle, src []byte, cst *syntax.Script, vals info.Values) string {
	if vals.Version == "" {
		return ""
	}
	globals, err := h.Globals(ctx)
	if err != nil || len(globals) == 0 {
		return ""
	}
	furniture, err := baselineGlobals(ctx, h)
	if err != nil {
		// Without the subtraction the answer would be mostly base's own
		// names, which is worse than no answer: the standard remedy is
		// vague and this would be wrong.
		return ""
	}
	// What the locator already put on the table. A global holding
	// exactly one of these adds nothing: that span was offered and
	// failed, and naming it again as a discovery would be noise.
	offered := map[string]bool{}
	for _, c := range portstyle.Candidates(src, cst, vals, info.FieldVersion) {
		if c.Literal {
			offered[c.Span.Text(src)] = true
		}
	}
	var names []string
	for name, val := range globals {
		if _, base := furniture[name]; base {
			continue
		}
		if !participates(name, val, vals.Version) || offered[val] {
			continue
		}
		names = append(names, fmt.Sprintf("%s = %q", name, val))
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	// Deliberately short of naming WHERE it is set. The interpreter
	// reports what it holds, not which file wrote it, and a sentence
	// that guessed "a PortGroup" would be wrong for the port whose
	// carrier is cut out of its own subport name.
	return fmt.Sprintf("the version is composed from %s, which is not a version literal in this Portfile; edit whatever sets it",
		strings.Join(names, " and "))
}

// baselineGlobals is what an evaluation holds when the Portfile says
// nothing: base's own furniture, read from the same installation and the
// same session rather than from a list this file would have to maintain.
//
// It is shadowed into the port's own directory so the evaluation is the
// port's in every respect but its text — same tree, same session, same
// platform frame — and taken at the top level, because a subport of a
// Portfile that no longer declares one does not exist.
func baselineGlobals(ctx context.Context, h port.Handle) (map[string]string, error) {
	shadow, cleanup, err := h.Shadow([]byte("PortSystem 1.0\nname baseline\nversion 0\n"))
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return shadow.Subport("").Globals(ctx)
}

// participates is the test for a global that is PART of the version:
// its value occurs in the version as a whole component rather than as a
// slice through the middle of one, and it is not one of the names that
// merely hold the answer.
//
// THE COMPONENT RULE IS WHAT MAKES THE SENTENCE TRUSTWORTHY. A version
// is delimited pieces, and a carrier contributes whole pieces. Base's
// python_version is "311", which falls inside the date 20231128, and a
// bare substring test reports it as composing libtapi's version; an
// Xcode workaround's "19" falls inside luajit's 1774896198 the same
// way. Requiring that the occurrence not be flanked by digits refuses
// both, and refuses no true carrier in the surveyed tree that this
// function is the last word on.
//
// It costs one real finding to buy that. perl5.moduleversion is "2.15"
// where the version is 2.150.0 — a component boundary the transform
// moves — and the rule rejects it. That is the right trade for a
// sentence whose only job is to send a user somewhere: silence leaves
// the standard remedy standing, and a confident wrong name does not.
// The perl5 shape has a registered transform of its own in any case.
func participates(name, val, version string) bool {
	switch name {
	case "version", "livecheck.version", "epoch", "revision":
		// Mirrors and neighbours, not sources: `version` IS the answer,
		// livecheck.version defaults to it, and the other two sit beside
		// it in every Portfile without composing it.
		return false
	}
	if len(val) < 2 || val == version {
		return false
	}
	for at := 0; ; {
		i := strings.Index(version[at:], val)
		if i < 0 {
			return false
		}
		i += at
		if !digitAt(version, i-1) && !digitAt(version, i+len(val)) {
			return true
		}
		at = i + 1
	}
}

// digitAt reports whether the version has a digit at an index, treating
// off-the-end as a clean boundary.
func digitAt(version string, i int) bool {
	return i >= 0 && i < len(version) && version[i] >= '0' && version[i] <= '9'
}
