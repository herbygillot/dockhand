package view

import (
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
	return map[attentionKind]string{AttentionFailed: "✗", AttentionNeedsYou: "!", AttentionReady: "·"}[k]
}

func AttentionFor(s engine.BranchStatus) []Attention {
	var rows []Attention
	for _, a := range branchstate.Attend(s.Facts()) {
		rows = append(rows, Attention{Kind: attentionKind(a.Kind), Branch: a.Branch, What: a.Words(engine.Vocabulary{}), Next: RenderNext(a.Next, a.Reason)})
	}
	return rows
}
