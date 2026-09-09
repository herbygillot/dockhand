package bump

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// carrier is a span proven to drive the version, and the literal to
// write into it to reach a target.
type carrier struct {
	Span  text.Span
	Style portstyle.Type
	Write string // what to put in the span
	// Template is the proof, for the debug line: the evaluated version
	// with the literal's place marked.
	Template string
	// ViaSet is a carrier located in a `set` word. Such an edit is
	// justified by ONE context's evaluation while the variable it writes
	// may be read by siblings, so the caller owes it the isolation guard.
	ViaSet bool
}

// discover finds which span drives the version and what to write into it,
// BY EXPERIMENT rather than by reading the text.
//
// THE METHOD. Write a same-shape probe value P over a candidate's literal
// L, evaluate, and read the version back as V_P. The longest common
// prefix and suffix of V and V_P bracket where the literal landed. If the
// bracketed middles are exactly L and P, the version is PROVEN to be
// prefix + <that literal> + suffix — the composition function observed by
// substitution rather than guessed from the source.
//
// The literal to write for target T is then T with those affixes removed,
// and the carrier can be accepted EXACTLY: unlike the movement rule the
// uncorroborated road falls back on, this knows what the evaluated
// version will be before the plan is made.
//
// WHY TEXT ALONE CANNOT DO IT. `version [terraformBaseVersion].${patchNumber}`
// writes no literal a search can corroborate, while an obsolete-stub
// branch elsewhere in the same Portfile hard-codes the very version the
// subport computes — kept in step by hand, so it matches every time.
// Reading the source, the two are indistinguishable; probed, only one
// moves the version.
//
// SELECTION IS BY THE TARGET AND NOT BY THE FIRST PROOF. helm's "4.2.4"
// proves as "" + <4.2> + ".4", which is true and useless — no edit to
// "4.2" reaches 4.2.5. The candidate to take is the one whose affixes
// bracket the TARGET too. Measured over 443 ports: selecting on the first
// proof instead picks a wrong-but-provable span for 2 of every 15.
//
// TWO CANDIDATES THAT BOTH EXPRESS THE TARGET ARE A REFUSAL. It is 1.4%
// of the surveyed population and always a Portfile carrying the same
// value twice; writing one and leaving the other is a Portfile that
// disagrees with itself, and choosing between them is not this function's
// to do.
//
// A carrier that TRANSFORMS its literal — [string map], [string range],
// a split — fails the middle test and is declined here, which is the
// honest answer: writing a target into a truncated sha would be wrong.
// The caller keeps its existing inexact road for those.
func discover(ctx context.Context, h port.Handle, src []byte, cst *syntax.Script, vals info.Values, target string) (carrier, bool) {
	var found []carrier
	for _, c := range portstyle.Candidates(src, cst, vals, info.FieldVersion) {
		if !c.Literal {
			continue // no literal to substitute a probe for
		}
		lit := c.Span.Text(src)
		probe := probeValue(lit)
		if probe == "" || probe == lit {
			continue
		}
		got, ok := evaluateWith(ctx, h, src, c.Span, probe)
		if !ok || got == vals.Version {
			continue // this span does not drive the version
		}
		pre, suf := affixes(vals.Version, got)
		if vals.Version[len(pre):len(vals.Version)-len(suf)] != lit ||
			got[len(pre):len(got)-len(suf)] != probe {
			continue // the literal is transformed on its way to the version
		}
		if !strings.HasPrefix(target, pre) || !strings.HasSuffix(target, suf) ||
			len(target) < len(pre)+len(suf) {
			continue // proven, but it cannot express this target
		}
		found = append(found, carrier{
			Span: c.Span, Style: c.Style,
			Write:    target[len(pre) : len(target)-len(suf)],
			Template: fmt.Sprintf("%q + <%s> + %q", pre, lit, suf),
			ViaSet:   c.Style == portstyle.SetVariable,
		})
	}
	if len(found) != 1 {
		return carrier{}, false
	}
	return found[0], true
}

// evaluateWith is one shadow evaluation of the source with a value
// written over a span. A failure to evaluate is not a finding — a probe
// value the Portfile refuses says nothing about the carrier — so it
// reports only whether an answer arrived.
func evaluateWith(ctx context.Context, h port.Handle, src []byte, span text.Span, value string) (string, bool) {
	probed, err := edit.Apply(src, []edit.Edit{{
		Kind: edit.Version, Start: span.Start, End: span.End,
		Old: span.Text(src), New: value, Reason: "carrier probe",
	}})
	if err != nil {
		return "", false
	}
	shadow, cleanup, err := h.Shadow(probed)
	if err != nil {
		return "", false
	}
	defer cleanup()
	sv, err := shadow.Values(ctx)
	if err != nil || sv.Version == "" {
		return "", false
	}
	return sv.Version, true
}

// probeValue is a value of the LITERAL'S OWN SHAPE, so a Portfile that
// does arithmetic or a comparison on it still evaluates: digits stay
// digits and letters stay letters. A sentinel like "ZZZZ" would be
// cleaner to spot and would break every port that computes with its
// version, which is the population this exists to serve.
func probeValue(lit string) string {
	if lit == "" || len(lit) > 200 {
		return ""
	}
	var b strings.Builder
	for _, r := range lit {
		switch {
		case r >= '0' && r <= '9':
			if r == '7' {
				b.WriteRune('4')
			} else {
				b.WriteRune('7')
			}
		case r >= 'a' && r <= 'z':
			if r == 'q' {
				b.WriteRune('w')
			} else {
				b.WriteRune('q')
			}
		case r >= 'A' && r <= 'Z':
			if r == 'Q' {
				b.WriteRune('W')
			} else {
				b.WriteRune('Q')
			}
		default:
			b.WriteRune(r) // separators keep the shape recognizable
		}
	}
	return b.String()
}

// affixes is the longest common prefix and suffix of two evaluated
// versions, which bracket the place the probed literal landed.
//
// The prefix is taken first and the suffix is measured over what is
// left, so the two cannot overlap on a version that is all one repeated
// character.
func affixes(a, b string) (string, string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	j := 0
	for j < len(a)-i && j < len(b)-i && a[len(a)-1-j] == b[len(b)-1-j] {
		j++
	}
	return a[:i], a[len(a)-j:]
}
