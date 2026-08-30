// Package rewrite locates checksum literals in a Portfile and produces
// the edits that replace them. It is the text half of changing a
// checksums block, where the parent package is the value half: what a
// checksum's new value should be lives there, where that literal sits
// in the source lives here.
//
// Location follows the same corroboration principle as version
// location: an edit target must be a literal whose text equals the
// evaluated value it claims to carry. A value the block does not carry
// literally is returned as unlocated rather than refused — whether
// that is a refusal is the caller's judgment, and different intents
// judge it differently.
package rewrite

import (
	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// commands set a port's checksums. The pair is empirically complete:
// across the whole ports tree only checksums and checksums-append
// appear, never the -delete, -replace, or -strsed forms options
// otherwise admit.
var commands = map[string]bool{
	"checksums":        true,
	"checksums-append": true,
}

// Edits replaces each replacement's old text where it appears as a
// literal word of a checksums command within scope, in document order,
// first unclaimed match winning. Replacements whose old text appears
// nowhere are returned unlocated, in the order given.
//
// scope is injected rather than assumed so this package needs no
// knowledge of what a Portfile's evaluation scopes are: callers pass
// portstyle.ScopeOf for the context they are editing.
func Edits(src []byte, cst *syntax.Script, scope func(syntax.Command) bool, reps []checksums.Replacement) (edits []plan.Edit, unlocated []checksums.Replacement) {
	located := make([]bool, len(reps))
	for cmd := range cst.Commands(src, scope) {
		name, ok := cmd.Name(src)
		if !ok || !commands[name] {
			continue
		}
		for _, w := range cmd.Words[1:] {
			lit, ok := w.Literal(src)
			if !ok {
				continue
			}
			for i, rep := range reps {
				if located[i] || rep.Old != lit {
					continue
				}
				// Located, and that is what the caller asked about — but
				// a replacement whose new text is its old text is not an
				// edit. Emitting one would put a change in a plan that
				// changes nothing, which matters once a caller can ask
				// for a re-derivation that may find everything already
				// correct.
				located[i] = true
				if rep.New == rep.Old {
					break
				}
				edits = append(edits, plan.Edit{
					Start:  w.Span.Start,
					End:    w.Span.End,
					Old:    lit,
					New:    rep.New,
					Reason: rep.Reason,
				})
				break
			}
		}
	}
	for i, rep := range reps {
		if !located[i] {
			unlocated = append(unlocated, rep)
		}
	}
	return edits, unlocated
}
