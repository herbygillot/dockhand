package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/herbygillot/dockhand/internal/plan"
)

// RenderPlan writes the human-facing summary of a plan.
//
// The reason column is padded to sixteen so a run of edits reads down
// the page as a table. It is its own width and not the branch column's:
// the two line up different things, and one number serving both would
// make either one's tuning move the other's output.
func RenderPlan(w io.Writer, p *plan.Plan) {
	target := p.Portdir
	if p.Subport != "" {
		target += " (subport " + p.Subport + ")"
	}
	fmt.Fprintf(w, "plan: %s %s, %d edits\n", p.Intent, target, len(p.Edits))
	for _, e := range p.Edits {
		fmt.Fprintf(w, "  %-16s %s -> %s\n", e.Reason+":", e.Old, e.New)
	}
	// The riders are already in the list above, spelled as edits. This
	// line says which of them nobody asked for — the word "also" because
	// it is the word the pull request body uses for the same fact, and a
	// reader who meets it twice should meet it once.
	if len(p.Riders) > 0 {
		fmt.Fprintf(w, "also: %s\n", strings.Join(p.Riders, ", "))
	}
	fmt.Fprintln(w, "predicted delta:")
	for _, cd := range p.Predicted {
		var parts []string
		for _, ch := range cd.Changes {
			parts = append(parts, renderChange(ch))
		}
		fmt.Fprintf(w, "  %s: %s\n", cd.Subport, strings.Join(parts, "; "))
	}
}

// renderChange keeps the delta line readable: a small change prints in
// full, and a big one — a cargo port's distfiles run to hundreds of
// entries — summarizes to counts. A field run measured the inlined
// form at 87KB on one line, burying the branch and verify lines the
// user actually needed; the full values still live in --plan's JSON.
//
// It stays unexported and keeps its own name: DescribeChange is a
// branch's standing, and this is a field's delta. The two words mean
// different things and sharing one would hide that.
func renderChange(ch plan.Change) string {
	const inlineMax = 6
	if len(ch.Old) <= inlineMax && len(ch.New) <= inlineMax {
		return fmt.Sprintf("%s %s -> %s",
			ch.Field, strings.Join(ch.Old, " "), strings.Join(ch.New, " "))
	}
	before := map[string]bool{}
	for _, v := range ch.Old {
		before[v] = true
	}
	changed := 0
	for _, v := range ch.New {
		if !before[v] {
			changed++
		}
	}
	return fmt.Sprintf("%s %d -> %d entries (%d new or changed)",
		ch.Field, len(ch.Old), len(ch.New), changed)
}
