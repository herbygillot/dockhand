package intent

import (
	"bytes"
	"log/slog"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Rule names one thing Examine looks for. The name is the word that
// reaches a note: a record's Riders are these, as strings, so a rider a
// reader sees in a change is a rule they can look up.
type Rule string

// RuleModeline is the editor header the MacPorts best-practices page
// prescribes, added to a Portfile that opens without one.
const RuleModeline Rule = "modeline"

// Phrase is how a rule reads in a sentence a human is given: a plan's
// body bullet, a housekeeping commit's subject, the "Also" line of a
// pull request. It is prose and may be reworded; the Rule itself is the
// name a note carries and may not.
func (r Rule) Phrase() string {
	switch r {
	case RuleModeline:
		return "add the editor modeline"
	}
	return string(r)
}

// Rider is a small edit an intent did not ask for, offered by Examine
// and folded into the change when both halves of the double proof hold.
// A rider is not a second intent: it is a correction too small to be
// worth its own branch, carried by whichever change was already
// touching the file.
type Rider struct {
	Rule Rule
	Edit edit.Edit
}

// RiderPolicy is what a run does with the riders Examine offers. It is
// the run's, not the intent's: the rules are written once and every
// headline intent is examined, so what varies is what the caller asked
// for on the command line and nothing else.
//
// It was a per-intent adoption map until S10. That map let a rule be
// adopted one intent at a time, which sounded careful and meant in
// practice that two of the three intents silently carried none — a
// difference no user could see in the help text and no test asserted a
// reason for.
type RiderPolicy int

const (
	// RidersAlong is the default: a rider rides with the headline change
	// when both proofs hold, and is named as withheld when the headline
	// finds nothing to do.
	RidersAlong RiderPolicy = iota
	// RidersOnly makes housekeeping the whole change: the riders are what
	// is planned, and the headline verb only chose the port.
	RidersOnly
	// RidersNone applies none and withholds none. A reviewer owed an
	// undistracted diff — the refresh caution's reader, most of all — gets
	// one by asking for it.
	RidersNone
)

// Cascade is a further change this one requires elsewhere: the revision
// bump a dependent needs because this port's ABI moved, the sibling that
// pins what this one just moved. A cascade is required where a finding
// merely proposes.
//
// Nothing produces one yet. The shape is here so that the return type
// of Examine does not move when something does.
type Cascade struct {
	// Port and Portdir name what must also change.
	Port    string
	Portdir string
	// Intent is the verb the cascade calls for, in the catalogue's own
	// vocabulary: the value a Definition's Name holds.
	Intent string
	// Reason is why, in words a reviewer can check.
	Reason string
}

// Examination is everything a look at the change turned up beyond the
// change itself.
//
// Findings are record's own type rather than a planning one. There is
// one findings vocabulary in this tree and it is the note's, because a
// finding that cannot be recorded is a finding nobody can answer; a
// second type here would exist only to be mapped onto the first. A
// finding made at plan time carries record.Proposed and leaves At to
// whoever appends it, which is the disposition's stated purpose.
type Examination struct {
	Riders   []Rider
	Cascades []Cascade
	Findings []record.Finding
}

// Examine looks at a change that has already been predicted and reports
// what else it noticed: riders to fold in, cascades it requires, and
// findings nobody asked about.
//
// One rule runs today — the modeline — and the rest of the shape is
// declared rather than implemented. before and after are the snapshots
// the prediction was made from, vals the evaluated state at the current
// version, and dependents the ports that depend on this one; all three
// are what the cascade and finding rules will read, and none of them are
// consulted by the modeline rule.
//
// Every rider it offers has already passed the FIRST half of the double
// proof: the edit touches only bytes Tcl never evaluates. The second
// half — that the prediction is the same with the rider as without it —
// costs a shadow evaluation, so it belongs to Finish. Neither half is
// sufficient alone, and the split is not an accident of layering: this
// one is structural and free, that one is semantic and expensive, and a
// rider needs both.
//
// It takes no context because it does no I/O. The day a rule needs to
// look something up, that is the day this grows one.
func Examine(src []byte, cst *syntax.Script, before, after info.Snapshot, vals info.Values, dependents []string) Examination {
	return Examination{Riders: Riders(src, cst)}
}

// Riders is the rule sweep on its own: what this source offers, with
// each candidate held to the first proof. It is separate from Examine
// because a planner that has just decided it has nothing to do needs
// the names — a decline says what went undone with it — and must not
// pay for a prediction it is not making.
func Riders(src []byte, cst *syntax.Script) []Rider {
	offered, _ := Sweep(src, cst)
	return offered
}

// Sweep is the rule pass with both halves of its answer: the riders
// offered, and the rules that had an edit and lost it to the first
// proof.
//
// A rule whose edit fails that proof is dropped rather than refused. It
// is a bug in the rule and not a fact about the port, so the port's
// change is not held hostage to it. But "no rule had anything to offer"
// and "a rule offered something and it was suppressed" are two different
// facts, and the run that asked for the housekeeping specifically is the
// run where the difference matters most — so the names come back rather
// than living only in a debug line nobody has --debug on for.
func Sweep(src []byte, cst *syntax.Script) (offered []Rider, dropped []Rule) {
	for _, rule := range rules {
		e, ok := rule.Edit(src)
		if !ok {
			continue
		}
		if !InCommentOrSpace(src, cst, e) {
			slog.Debug("rider dropped: its edit touches evaluated bytes",
				"rule", string(rule.Rule), "start", e.Start, "end", e.End)
			dropped = append(dropped, rule.Rule)
			continue
		}
		offered = append(offered, Rider{Rule: rule.Rule, Edit: e})
	}
	return offered, dropped
}

// rule is one entry of the sweep: the name a note carries, and the edit
// it would make. A rule reads only the source, because a rule that
// needed the evaluated state would be a finding and not a rider.
type rule struct {
	Rule Rule
	Edit func(src []byte) (edit.Edit, bool)
}

// rules is every housekeeping rule this build knows, in the order they
// run — which is the order a rider set is named in, on a plan, in a note
// and in a pull request body. A list and not a set, because that order
// is part of what a reader is promised.
var rules = []rule{
	{RuleModeline, modelineEdit},
}

// Withheld names the riders a decline is leaving undone, in the order
// the rules ran. It is what a caller sweeping ports reads to tell
// "nothing to do" from "nothing to do, and here is what that cost".
//
// These are FIRST-PROOF candidates and not a promise. A decline made
// here has no prediction to compare against, so the second half of the
// double proof has not been paid for; a rule that would fail it is named
// anyway, and the --riders run the exit-12 code invites would then
// answer ErrRiderMoved in the failure band. Nothing can reach that today
// — the one rule that ships inserts a comment at offset zero and cannot
// move a prediction — and the day a second rule exists, that gap is the
// thing to close rather than to discover.
//
// Under RidersNone nothing was ever going to ride, so nothing is
// withheld and the decline keeps the plain already-current code.
func Withheld(src []byte, cst *syntax.Script, policy RiderPolicy) []string {
	if policy == RidersNone {
		return nil
	}
	return Names(Riders(src, cst))
}

// ruleNames is a rule list as strings, for a sentence that names rules
// no rider was made from.
func ruleNames(rules []Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, string(r))
	}
	return out
}

// Names is the rule names of a rider set: the words a note carries and a
// body cites.
func Names(riders []Rider) []string {
	if len(riders) == 0 {
		return nil
	}
	out := make([]string, 0, len(riders))
	for _, r := range riders {
		out = append(out, string(r.Rule))
	}
	return out
}

// Phrase is a rider set said in one clause — "add the editor modeline",
// or two of those joined — for the summary of a change that is nothing
// but housekeeping.
func Phrase(riders []Rider) string {
	parts := make([]string, 0, len(riders))
	for _, r := range riders {
		parts = append(parts, r.Rule.Phrase())
	}
	return strings.Join(parts, "; ")
}

// InCommentOrSpace is the first half of the double proof: the edit
// touches only bytes the Tcl evaluator never reads.
//
// What "touches" means differs by shape, and the difference is the whole
// content of the rule. An edit that REPLACES bytes must replace bytes no
// command occupies — a comment, or the whitespace between commands. An
// edit that INSERTS replaces nothing, so it is judged by where it lands
// and by what it is made of: inside a command is forbidden, because
// splitting a command in two is touching it, and a gap boundary is
// allowed only when the new bytes stay a gap.
//
// That second condition is the one this rule was missing. A boundary
// insertion is not automatically in the gap: bytes written at a
// command's LAST offset are appended to its last token, so ` --enable-
// evil` after `configure.args --with-foo` reads as an argument the port
// now builds with, and bytes at its FIRST offset run into its first
// token, so `# ` there comments the whole command out. Both were offered
// as riders and both were measured doing it. The precondition behind
// "that is where every housekeeping line goes" is the newline that keeps
// the line its own: an insertion at a command's start must end with one,
// and one at its end must begin with one.
//
// What the rule still cannot answer is what the new bytes SAY once they
// are their own line — a whole command written into a gap has an
// innocent span and is not housekeeping. That is the second proof's
// half, and Finish re-reads the tree the edits produce before paying for
// it. This one exists to catch what neither of those can see: an edit
// that rewrote a literal and happened to leave the evaluation where it
// was.
//
// A command's span covers its braced bodies whole, so a comment inside a
// variant block counts as evaluated. That is deliberately conservative:
// brace bodies are opaque until a lens re-reads them, and a rule that
// wants to edit inside one can argue for it then.
func InCommentOrSpace(src []byte, cst *syntax.Script, e edit.Edit) bool {
	if cst == nil {
		// No parse, no proof. The caller has one — Handle.Source refuses a
		// Portfile it could not parse — so this is a programming error
		// answered the safe way rather than a case that reaches users.
		return false
	}
	if e.Start < 0 || e.End > len(src) || e.Start > e.End {
		return false
	}
	for _, item := range cst.Items {
		c, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if e.Start == e.End {
			switch {
			case c.Span.Start < e.Start && e.Start < c.Span.End:
				return false
			case e.Start == c.Span.Start && !strings.HasSuffix(e.New, "\n"):
				// The command begins where these bytes end, so without a
				// newline between them the command's first token is
				// whatever the insertion trails off with.
				return false
			case e.Start == c.Span.End && !strings.HasPrefix(e.New, "\n"):
				// And symmetrically: these bytes begin where the command's
				// last token ends, so without a newline they are part of it.
				return false
			}
			continue
		}
		if e.Start < c.Span.End && c.Span.Start < e.End {
			return false
		}
	}
	return true
}

// Modeline is the editor header the MacPorts best-practices page
// prescribes.
const Modeline = "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"

// modelineEdit inserts the modeline when the Portfile's leading comment
// block carries none. Detection is deliberately loose — any leading
// comment carrying an emacs -*- block or a vim: stanza counts — so an
// existing modeline in either dialect, however configured, is never
// second-guessed or rewritten.
//
// The whole run of blank and comment lines before the first command is
// read, and not the first line alone. A Portfile that opens with a blank
// line and carries its modeline on line two was judged to have none, and
// got a second one written above the first: two ports in the tree
// (python/py27-dulwich, devel/tortoisehg) are in exactly that shape, and
// the visibly wrong hunk would have gone to a public pull request. Both
// halves of the double proof pass it, correctly — the edit is inert. The
// proofs certify that a rider is inert, never that it is right, and rule
// quality is not something they can be delegated.
func modelineEdit(src []byte) (edit.Edit, bool) {
	for rest := src; len(rest) > 0; {
		line, tail, found := bytes.Cut(rest, []byte("\n"))
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] != '#' {
			// The first command. Everything above it has been read.
			break
		}
		if isModeline(trimmed) {
			return edit.Edit{}, false
		}
		if !found {
			break
		}
		rest = tail
	}
	return edit.Edit{Start: 0, End: 0, Old: "", New: Modeline + "\n", Reason: "modeline"}, true
}

// isModeline reads one already-trimmed comment line for either editor's
// stanza.
func isModeline(line []byte) bool {
	return bytes.HasPrefix(line, []byte("#")) &&
		(bytes.Contains(line, []byte("-*-")) ||
			bytes.Contains(line, []byte("vim:")) ||
			bytes.Contains(line, []byte("vi:")))
}
