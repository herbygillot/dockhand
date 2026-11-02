package view

import (
	"fmt"
	"io"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/engine"
)

// Attention is one row of what needs you, with the command that moves it.
type Attention struct {
	Kind               attentionKind
	Branch, What, Next string
}

// attentionKind is what an attention row asks of the person, typed, where
// its JSON kind was read back from the glyph its text printed (the command
// package review's concept 6).
type attentionKind string

const (
	AttentionFailed   attentionKind = "failed"
	AttentionNeedsYou attentionKind = "needs_you"
	AttentionReady    attentionKind = "ready"
)

// Mark is the kind's glyph in the text.
func (k attentionKind) Mark() string {
	return map[attentionKind]string{AttentionFailed: Broken, AttentionNeedsYou: Look, AttentionReady: Done}[k]
}

func AttentionFor(s engine.BranchStatus) []Attention {
	var rows []Attention
	for _, a := range branchstate.Attend(s.Facts()) {
		rows = append(rows, Attention{Kind: attentionKind(a.Kind), Branch: a.Branch, What: a.Words(engine.Vocabulary{}), Next: RenderNext(a.Next, a.Reason)})
	}
	return rows
}

// WriteInvestigated says a branch that changes nothing is an
// investigation, as its status row says it, with archive next, for a
// check's last lines: status's would have said submit, which refuses it
// (field testing's curl +sectrust notes, note 1). Any other branch, it
// says nothing of.
func WriteInvestigated(out io.Writer, s engine.BranchStatus) {
	for _, a := range branchstate.Attend(s.Facts()) {
		if a.Reason == branchstate.Investigated {
			fmt.Fprintf(out, "%s\nNext: %s\n", a.Words(engine.Vocabulary{}), RenderNext(a.Next, a.Reason))
		}
	}
}
