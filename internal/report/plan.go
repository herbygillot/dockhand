package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
)

// KindPatchesUnchecked is the finding a bump carries when it fetched no
// distfile and so had nothing to check the port's patches against.
//
// It is DERIVED from the record's enum rather than spelled again. There
// were three copies of this word — here, in intent/bump, and (missing)
// in record — and the missing one failed every mint that produced the
// finding. A third literal is a third place for the same word to drift,
// so this one is the enum's, converted: RenderPlan reads a plan-time
// finding, whose Kind is a plain string by design.
const KindPatchesUnchecked = string(record.KindPatchesUnchecked)

// KindPatchUnrelocated is the finding a bump carries for a patch that
// does not come over to the new source. Derived for KindPatchesUnchecked's
// reason, and printed for a sharper one: this is the sentence that turns
// a plan a person is reading into one they have work to do on, and it
// used to be a refusal they met instead of a plan.
const KindPatchUnrelocated = string(record.KindPatchUnrelocated)

// RenderPlan writes the human-facing summary of a plan.
//
// The reason column is padded to sixteen so a run of edits reads down
// the page as a table. It is its own width and not the branch column's:
// the two line up different things, and one number serving both would
// make either one's tuning move the other's output.
func Plan(w io.Writer, p *plan.Plan) {
	target := p.Portdir
	if p.Subport != "" {
		target += " (subport " + p.Subport + ")"
	}
	fmt.Fprintf(w, "plan: %s %s, %d edits\n", p.Intent, target, len(p.Edits))
	for _, e := range p.Edits {
		fmt.Fprintf(w, "  %-16s %s -> %s\n", e.Reason+":", terse(e.Old), terse(e.New))
	}
	// The whole files the plan rewrites, in the same column as the
	// edits so the table reads on: the path is the reason's slot,
	// because a file's reason is which file it is, and what happened to
	// it stands where the values would. Its bytes are not printed — a
	// patch is a page, and the JSON has it.
	for _, f := range p.Files {
		fmt.Fprintf(w, "  %-16s %s\n", f.Path+":", f.Reason)
	}
	// The riders are already in the list above, spelled as edits. This
	// line says which of them nobody asked for — the word "also" because
	// it is the word the pull request body uses for the same fact, and a
	// reader who meets it twice should meet it once.
	if len(p.Riders) > 0 {
		fmt.Fprintf(w, "also: %s\n", strings.Join(p.Riders, ", "))
	}
	// What the plan could not do, after what it did. A bump that fetched
	// no distfile had no source to check the port's patches against, and
	// the plan carries the sentence as a finding; it is a line here for
	// the reason the ABI check's "unavailable" is a line in a body —
	// "not checked" and "checked, and still where they were" are the two
	// answers a reader would otherwise confuse, and the second is what an
	// absent line reads as. The criterion opens with its own verdict, so
	// it is printed as it stands rather than under a label that would say
	// it twice.
	for _, f := range p.Findings {
		// Both patch findings, and they are two different sentences. One
		// says nothing was fetched so nothing was checked; the other says
		// a patch was checked and does not come over, which is the one
		// that needs a person before this branch goes anywhere. Each
		// criterion opens with its own verdict, so both print as they
		// stand.
		switch f.Kind {
		case KindPatchesUnchecked, KindPatchUnrelocated:
			if f.Criterion != "" {
				fmt.Fprintln(w, f.Criterion)
			}
		}
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

// terse is one edit's value, kept to a line.
//
// AN EDIT REPLACES A SPAN, AND A SPAN CAN BE A PAGE. The narration was
// written for the edits a bump usually makes — a version, a revision, a
// checksum — where the whole value is the news. It prints Old and New
// verbatim, and a field run of a Rust port measured what that costs: the
// cargo.crates block is one edit whose Old and New are each some four
// hundred lines, so `bump` scrolled eight hundred lines of vendored
// crate names past the branch name and the verify line the person was
// waiting for.
//
// The rule is the one renderChange already applies to the predicted
// delta a few lines down: a small value prints in full because the value
// IS the news, and a big one prints its shape because the shape is all a
// reader can use on one line. The bytes are not lost — `--plan`'s JSON
// carries every edit whole, which is the road for a reader who wants
// them.
//
// Line count first, because an edit spanning a block is a block: "412
// lines" is what a person recognizes cargo.crates by, where a byte count
// alone could be one very long line.
func terse(s string) string {
	const inlineMax = 72
	if !strings.ContainsRune(s, '\n') && len(s) <= inlineMax {
		return s
	}
	if n := strings.Count(s, "\n") + 1; n > 1 {
		return fmt.Sprintf("%d lines, %d bytes", n, len(s))
	}
	// By RUNES, and clamped: a line under the rune budget can still be
	// over the byte budget, and slicing a rune slice past its end panics
	// in a reporter.
	r := []rune(s)
	if len(r) <= inlineMax {
		return s
	}
	return string(r[:inlineMax-1]) + "\u2026"
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
