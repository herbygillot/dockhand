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
	"strings"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/edit"
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
// A value the checksums command carries only by substitution may still
// live in the file as a literal: six Portfiles tree-wide keep their
// hashes in top-level set variables (set rmd160(pcre2) <hash>) that
// the checksums statement dereferences. Those replacements fall back
// to in-scope set commands — see locateInSets for the aliasing rules —
// and viaSet reports that any did, because such an edit is justified
// by one subport's evaluation while standing outside any checksums
// command, so the caller owes a proof that no sibling context moved.
//
// context is the subport whose values the replacements were derived
// from; scope is injected rather than assumed so this package needs no
// knowledge of what a Portfile's evaluation scopes are: callers pass
// portstyle.ScopeOf for the context they are editing.
func Edits(src []byte, cst *syntax.Script, scope func(syntax.Command) bool, context string, reps []checksums.Replacement) (edits []edit.Edit, unlocated []checksums.Replacement, viaSet bool) {
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
				edits = append(edits, edit.Edit{
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
	setEdits := locateInSets(src, cst, scope, context, reps, located)
	edits = append(edits, setEdits...)
	viaSet = len(setEdits) > 0
	for i, rep := range reps {
		if !located[i] {
			unlocated = append(unlocated, rep)
		}
	}
	return edits, unlocated, viaSet
}

// setCarrier is one in-scope `set` command whose value is a literal —
// a place a checksum may live outside any checksums command.
type setCarrier struct {
	variable string
	value    string
	start    int
	end      int
	claimed  bool
}

// locateInSets finds still-unlocated replacements among set commands,
// marking what it locates in located. Aliasing is the hazard here —
// two subports can record an identical value (sizes collide for
// real), and editing a sibling's line would corrupt the sibling AND
// leave the asked-for value stale — so selection is strict: a carrier
// whose array key names the context exactly (rmd160(pcre2) for
// context pcre2) wins, and without such a key the match must be the
// only candidate. Anything ambiguous stays unlocated, which the
// caller reports honestly.
func locateInSets(src []byte, cst *syntax.Script, scope func(syntax.Command) bool, context string, reps []checksums.Replacement, located []bool) []edit.Edit {
	var carriers []*setCarrier
	for cmd := range cst.Commands(src, scope) {
		name, ok := cmd.Name(src)
		if !ok || name != "set" || len(cmd.Words) != 3 {
			continue
		}
		variable, ok := cmd.Words[1].Literal(src)
		if !ok {
			continue
		}
		value, ok := cmd.Words[2].Literal(src)
		if !ok {
			continue
		}
		carriers = append(carriers, &setCarrier{variable: variable, value: value,
			start: cmd.Words[2].Span.Start, end: cmd.Words[2].Span.End})
	}
	keyed := "(" + context + ")"
	var edits []edit.Edit
	for i, rep := range reps {
		if located[i] {
			continue
		}
		var exact, loose []*setCarrier
		for _, c := range carriers {
			if c.claimed || c.value != rep.Old {
				continue
			}
			if strings.HasSuffix(c.variable, keyed) {
				exact = append(exact, c)
			} else {
				loose = append(loose, c)
			}
		}
		var chosen *setCarrier
		switch {
		case len(exact) == 1:
			chosen = exact[0]
		case len(exact) == 0 && len(loose) == 1:
			chosen = loose[0]
		default:
			continue // absent or ambiguous: unlocated, honestly
		}
		chosen.claimed = true
		located[i] = true
		if rep.New == rep.Old {
			continue
		}
		edits = append(edits, edit.Edit{Start: chosen.start, End: chosen.end,
			Old: rep.Old, New: rep.New, Reason: rep.Reason})
	}
	return edits
}
