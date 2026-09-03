package intent

import (
	"bytes"

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

// Rider is a small edit an intent did not ask for, offered by Examine
// and folded into the change when the intent accepts that rule. A rider
// is not a second intent: it is a correction too small to be worth its
// own branch, carried by whichever change was already touching the file.
type Rider struct {
	Rule Rule
	Edit edit.Edit
}

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
// are what the cascade and finding rules will read, and none of the
// four are consulted by the modeline rule.
//
// It takes no context because it does no I/O. The day a rule needs to
// look something up, that is the day this grows one.
func Examine(src []byte, cst *syntax.Script, before, after info.Snapshot, vals info.Values, dependents []string) Examination {
	var ex Examination
	if e, ok := modelineEdit(src); ok {
		ex.Riders = append(ex.Riders, Rider{Rule: RuleModeline, Edit: e})
	}
	return ex
}

// Modeline is the editor header the MacPorts best-practices page
// prescribes.
const Modeline = "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"

// modelineEdit inserts the modeline when the Portfile's very first line
// is not one. Detection is deliberately loose — any leading comment
// carrying an emacs -*- block or a vim: stanza counts — so an existing
// modeline in either dialect, however configured, is never
// second-guessed or rewritten.
func modelineEdit(src []byte) (edit.Edit, bool) {
	first, _, _ := bytes.Cut(src, []byte("\n"))
	if bytes.HasPrefix(first, []byte("#")) &&
		(bytes.Contains(first, []byte("-*-")) || bytes.Contains(first, []byte("vim:")) || bytes.Contains(first, []byte("vi:"))) {
		return edit.Edit{}, false
	}
	return edit.Edit{Start: 0, End: 0, Old: "", New: Modeline + "\n", Reason: "modeline"}, true
}
