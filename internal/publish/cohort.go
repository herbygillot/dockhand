package publish

import (
	"fmt"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/darwin/abi"
	"github.com/herbygillot/dockhand/internal/record"
)

// The pull request's cohort section: the dependents a change revbumped,
// the measurement it did so on, and the ports the proposal examined and
// left out.
//
// ONE SENTENCE APPEARS IN THREE PLACES AND IT APPEARS VERBATIM: the
// criterion the measurement produced. That is the point of the criterion
// being made once, in the judgment, rather than reworded per audience. A
// commit body, a pull request and a terminal line that each paraphrased
// "install name libwidget.2.dylib -> libwidget.3.dylib" would be three
// claims a reviewer has to reconcile, and the whole argument for a
// proposal is that a person can check the one claim behind it with otool
// by hand.
//
// THE FINDING VOCABULARY IS SMALLER THAN THE ONE THE SHIPPED RENDERER
// READ, and the section is smaller with it. render/cohort.go switched on
// five kinds it spelled itself — a cohort proposal, an instruction
// comment, and three separate abi-change / abi-unchanged / abi-
// unavailable rows carrying the measurement. record.FindingKind has
// since closed around the three kinds a durable record carries, of which
// a bare measurement is not one: dependents.Cohort.Finding says so and
// says what replaces it, which is that the criterion a reader wants is
// quoted on the PROPOSAL that rests on it, where somebody weighing the
// proposal is already looking. So there is one measured sentence here
// instead of three, and the caveat travels beside it.

// cohortBody is the cohort section, derived from the change record and
// from the verdict set and from nothing else.
//
// Everything it says is a fact the record already carries, which is the
// whole reason it takes one: the body vouches for what the record
// remembers and not for what the diff can be re-read to contain. The
// criterion is restated verbatim from the measurement that made it, the
// members come with the link proof their own runs recorded, and the
// ports the proposal examined and left out are printed with the reason —
// including a cohort a measurement REFUTED, which is the one sentence a
// reader would otherwise never see.
//
// Empty for a change with no findings at all, so an ordinary bump's body
// is unchanged.
func cohortBody(c record.Change, vs []Verdict) string {
	var b strings.Builder
	for _, f := range c.Findings {
		switch f.Kind {
		case record.KindInstruction:
			fmt.Fprintf(&b, "\nThe comment in %s says:\n\n%s\n", f.Source, fence(f.Quote))
			if f.Disposition == record.Dismissed {
				b.WriteString("\nDismissed by hand: no revision bumps were made on it.\n")
			}
		case record.KindStealth:
			// A reconstruction that did not agree with the tip. It reaches a
			// published body only where a person answered the hold it raised
			// and published anyway, and then it is exactly the thing a reviewer
			// must be told: the files are named because the pass that raised it
			// was unattended and the only account anybody gets is what is
			// written down.
			if len(f.Diverged) > 0 {
				fmt.Fprintf(&b, "\nRe-planning this change from its own base did not reproduce the tip: %s.\n",
					strings.Join(f.Diverged, ", "))
			}
		case record.KindPatchesUnchecked:
			// WHAT WAS NOT CHECKED, said to the reviewer. "not checked" and
			// "checked, and still where they were" are different answers,
			// and a body that printed neither leaves a reviewer to assume
			// the second — which is the whole reason bump makes this
			// finding at all (rule 7).
			//
			// The criterion opens with its own verdict, so it is printed as
			// it stands rather than under a label that would say it twice —
			// the same reading report.Plan gives it in the plan narration.
			// This is the second half of a finding that used to reach the
			// plan and stop there, because its kind could not become a
			// record kind and the mint that would have carried it failed.
			if f.Criterion != "" {
				fmt.Fprintf(&b, "\n%s\n", f.Criterion)
			}
		case record.KindPatchUnrelocated:
			// A PATCH THE BUMP COULD NOT CARRY OVER, and the reviewer is the
			// second person who needs to know. This branch used never to
			// exist — the bump declined outright — so the sentence had
			// nowhere to be said; now it can be published past by a person,
			// and a body that did not carry it would be the
			// complete-looking artifact the old decline was protecting
			// against.
			//
			// Dismissed is spelled, like the instruction comment's, because
			// "somebody looked and said the patch is fine" is a different
			// fact from "nobody has looked".
			fmt.Fprintf(&b, "\n%s\n", f.Criterion)
			if f.Disposition == record.Dismissed {
				b.WriteString("\nDismissed by hand: the patch was judged to carry over as it stands.\n")
			}
		case record.KindABIDependents:
			b.WriteString(dependentsSection(f, vs))
		}
	}
	return b.String()
}

// dependentsSection is one ABI-dependents finding as the body states it:
// the criterion, the caveat that always travels with a criterion, what
// became of the proposal, and every port it examined.
func dependentsSection(f record.Finding, vs []Verdict) string {
	var b strings.Builder
	if f.Criterion != "" {
		// The caveat is IMPORTED from the measurement that made it and never
		// reworded here. It is one sentence about what otool cannot see, true
		// of every reading, and the whole reason it is a constant is that a
		// commit body, a pull request and a terminal line must not be able to
		// word it three ways.
		fmt.Fprintf(&b, "\n%s.\n%s.\n", f.Criterion, abi.Limits)
	}
	switch f.Disposition {
	case record.Proposed:
		// A proposal still open in a published body is a person having
		// published past their own advisory, which is theirs to do and worth
		// saying out loud rather than dressing up as a cohort.
		fmt.Fprintf(&b, "\n%s was proposed and is not in this change.\n", proposedPorts(f))
	case record.Dismissed:
		fmt.Fprintf(&b, "\n%s was proposed and dismissed by hand.\n", proposedPorts(f))
	case record.Accepted:
		b.WriteString("\nRevision bumped in this change:\n")
		for _, m := range cohortMembers(f, vs) {
			fmt.Fprintf(&b, "  — %s\n", memberLine(m))
		}
	}
	for _, line := range listedLines(f.Candidates) {
		if line == listedHeader {
			fmt.Fprintf(&b, "\n%s\n", line)
			continue
		}
		fmt.Fprintf(&b, "  — %s\n", line)
	}
	return b.String()
}

// cohortMember is one port a cohort revbumped, with what the
// environment said about it afterwards.
type cohortMember struct {
	Port    string
	Portdir string
	// Reason is why it is in the list, in the words the proposal used —
	// the depends_* fields the edge came from, and the comment that named
	// it where one did.
	Reason string
	// Links are the link-proof lines from this member's own runs: which
	// of the files it installed bind to the library that moved.
	//
	// Nil and empty are different answers and both are printed as
	// themselves. Nil is nobody looked; empty is the sweep ran and found
	// no binding, which makes the port build-only in fact whatever its
	// depends_* fields said — and that is worth saying rather than quietly
	// dropping, because the revbump was still spent.
	Links []string
	// Unmeasured is why there is no proof, where Links is nil because the
	// member's own run never reached the sweep: the build failed, or it
	// was blocked before it was reached, or this build withheld it. It is
	// printed in the proof's place, so a member the body claims a bump for
	// is never listed with nothing beside it — the reviewer reading
	// "Revision bumped in this change" is owed either the evidence or the
	// reason there is none, on the same line.
	Unmeasured string
}

// cohortMembers pairs each proposed candidate with the link proof its
// own runs recorded, in the proposal's own order — which is the order
// the members had to be built in.
//
// The runs are read per port and not per platform: a member verified on
// two releases has two sets of lines, and what the body claims is that
// the binding was observed, not that it was observed twice. The lines
// are unioned and sorted so the same record renders the same bytes.
func cohortMembers(f record.Finding, vs []Verdict) []cohortMember {
	links := map[string][]string{}
	looked := map[string]bool{}
	states := map[string][]record.RunState{}
	for _, v := range vs {
		states[v.Port] = append(states[v.Port], v.Run.State)
		if v.Run.Links == nil {
			continue
		}
		looked[v.Port] = true
		links[v.Port] = append(links[v.Port], v.Run.Links...)
	}
	out := make([]cohortMember, 0, len(f.Candidates))
	for _, c := range f.Candidates {
		if !c.Proposed {
			continue
		}
		m := cohortMember{Port: c.Port, Portdir: c.Portdir, Reason: c.Reason}
		if looked[c.Port] {
			m.Links = dedupe(links[c.Port])
		} else {
			m.Unmeasured = unmeasured(states[c.Port], c.Solo)
		}
		out = append(out, m)
	}
	return out
}

// memberLine is one revbumped port with its portdir and its reason, and
// the link proof where the environment took one — or, where the member's
// own run is why there is none, the reason in its place.
func memberLine(m cohortMember) string {
	line := m.Port
	if m.Portdir != "" {
		line += " (" + m.Portdir + ")"
	}
	if m.Reason != "" {
		line += ": " + m.Reason
	}
	switch {
	case m.Links == nil:
		// Nobody looked. Where that is the member's own doing the line says
		// which — a failed member listed under "Revision bumped" with
		// nothing beside it reads as evidence that was forgotten, not as a
		// build that never got there. Where it is not (a measurement that
		// could not be made) the silence stands, because the reason is
		// stated once elsewhere and not per member.
		if m.Unmeasured != "" {
			line += "; " + m.Unmeasured
		}
	case len(m.Links) == 0:
		// "That moved" and not "that this change publishes": the proof is
		// taken against the install names the measurement says a dependent
		// can no longer rely on, so a member that links an untouched library
		// of the headline's reads as nothing here, and the sentence has to
		// be the one that is true of it.
		line += "; links nothing that moved"
	default:
		line += "; " + strings.Join(m.Links, ", ")
	}
	return line
}

// unmeasured is the sentence a member carries in the proof's place,
// where its own runs are why no proof was taken. Empty where they are
// not: a member that passed and was still not swept had a measurement
// that could not be made, and that is said once for the whole change.
//
// The three states named are exactly the outcomes best effort publishes
// over without a pass — the population D24 made routine — so each has a
// sentence where the bump is claimed. A failure outranks a block, which
// outranks a withholding, because a member can carry one of each across
// platforms and the line says the strongest fact about it; a non-outcome
// (still running, canceled, the machine's silence) is not published over
// and is the verification block's to name, so it earns nothing here.
//
// The withheld sentence is the one the proposal usually wrote already. A
// candidate the proposal marked Solo carries "bumped here, and not
// built" in its own reason, which is how the withheld member came to
// explain itself inline before the others did, and the line does not say
// it twice. Solo is what says so: the flag is the record's, and a
// renderer sniffing the reason's prose for the sentence would be coupled
// to its wording rather than its meaning.
func unmeasured(states []record.RunState, solo bool) string {
	failed, blocked, withheld := false, false, false
	for _, s := range states {
		switch s {
		case record.Failed:
			failed = true
		case record.Blocked:
			blocked = true
		case record.Withheld:
			withheld = true
		case record.Passed, record.Unsupported, record.Queued, record.Submitting,
			record.Running, record.Canceled, record.Superseded, record.Errored,
			record.Faulted:
		}
	}
	switch {
	case failed:
		return "the build failed, so nothing was measured"
	case blocked:
		return "blocked before it was reached, so nothing was measured"
	case withheld && !solo:
		return "not built here"
	}
	return ""
}

// listedHeader opens the examined-and-not-proposed block.
const listedHeader = "Examined and not bumped:"

// listedLines is the examined-and-left-out block: the header, then one
// line per port with the reason the proposal gave.
//
// The rows the proposal put forward are skipped here — they are the
// members, and they have their own block — so what is left is exactly
// the decisions a reviewer has to check by hand. A decision no reader
// can see is a decision nobody can disagree with, which is why these are
// printed rather than counted.
func listedLines(all []record.Candidate) []string {
	var out []string
	for _, c := range all {
		if c.Proposed {
			continue
		}
		line := c.Port
		if c.Portdir != "" {
			line += " (" + c.Portdir + ")"
		}
		if c.Reason != "" {
			line += ": " + c.Reason
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil
	}
	return append([]string{listedHeader}, out...)
}

// proposedPorts says what a cohort proposal puts forward, naming the
// ports rather than counting them where there are few enough to read.
func proposedPorts(f record.Finding) string {
	var ports []string
	for _, c := range f.Candidates {
		if c.Proposed {
			ports = append(ports, c.Port)
		}
	}
	if len(ports) == 0 {
		ports = f.Ports
	}
	what := "dependents need a revision bump"
	if len(ports) == 1 {
		what = "dependent needs a revision bump"
	}
	if len(ports) > 6 {
		// Past a handful the names stop being a list and start being a
		// paragraph. The record carries all of them, and `status --json`
		// publishes it.
		return fmt.Sprintf("%d %s", len(ports), what)
	}
	return fmt.Sprintf("%d %s (%s)", len(ports), what, strings.Join(ports, ", "))
}

// fence puts a quoted comment in a fenced code block, so a verbatim
// Portfile comment inside a pull request body reads as a quotation
// rather than as the body's own prose. The bytes are otherwise
// untouched: a quote that was reflowed is not verbatim.
//
// IT USED TO INDENT BY TWO SPACES, AND A PORTFILE COMMENT STARTS WITH
// "#". Markdown needs four spaces for a code block and reads two as
// ordinary prose, so every line of a quoted instruction rendered as an
// H1 HEADING — the whole maintainer's comment, on a live pull request,
// in title-sized type. Measured on #34567.
//
// Four spaces would fix the size and still be wrong: the quote would be
// re-wrapped by the renderer and a line beginning "#" inside a Portfile
// is not a heading in any reading. A fence says "these are bytes from a
// file" and says it to the renderer as well as to the reader.
//
// The fence is a plain one with no language: what is inside is a comment
// block from a Portfile, and claiming a lexer for it would be claiming
// something about bytes this function promises not to touch.
func fence(quote string) string {
	return "```\n" + strings.TrimRight(quote, "\n") + "\n```"
}

// dedupe is a line set said once each, sorted, so two platforms
// reporting the same binding read as one binding.
func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
