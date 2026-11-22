package model

// ChangeRequest is what was asked of one branch's work, one value kept
// with the branch (docs/designs/preparation-and-request.md, "The
// request"). #194 keeps its targets, each with the actions concluded for
// it; the flags as asked are #195's.
type ChangeRequest struct {
	// ID is new whenever what was asked changes.
	ID string
	// Origin is the command that asked: update, bump, ship or serve.
	Origin  string
	Targets []RequestTarget `json:",omitempty"`
}

// RequestTarget is one port of a branch's work.
type RequestTarget struct {
	Port string
	// Action is update, or stub for an obsolete stub that follows.
	Action string
	// Named is the version the person named; empty for the newest.
	Named string `json:",omitempty"`
	// Release is the release selected for it, kept from when it is
	// selected, so the same command again prepares it and not a newer.
	Release *Release `json:",omitempty"`
	// Current is the version it was found at, where nothing newer was
	// selected; that concludes the version alone.
	Current string `json:",omitempty"`
	// Outcomes are each action asked of the port, once it is concluded.
	Outcomes []ActionOutcome `json:",omitempty"`
}

// ActionOutcome is what became of one action of a request.
type ActionOutcome struct {
	// Action is version, source, moves, obsolete, dependents or closes.
	Action string
	Result ActionResult
	Why    string `json:",omitempty"`
}

// ActionResult is how an action was concluded.
type ActionResult string

const (
	// ActionDone is an action carried out.
	ActionDone ActionResult = "done"
	// ActionSatisfied is one already so.
	ActionSatisfied ActionResult = "satisfied"
	// ActionRefused is one concluded and not carried out, which blocks
	// until its ask changes.
	ActionRefused ActionResult = "refused"
)

// Outcome is the outcome recorded for action, where one is.
func (t RequestTarget) Outcome(action string) (ActionOutcome, bool) {
	for _, outcome := range t.Outcomes {
		if outcome.Action == action {
			return outcome, true
		}
	}
	return ActionOutcome{}, false
}

// Finished reports whether a target's version is concluded: by its
// outcome, by the version it was found current at, or by a written edit
// of its port at the release kept for it. An edit at another release, an
// earlier ask's, leaves it unfinished (Codex's read of #194's 2c, P2-2).
func (t RequestTarget) Finished(edits []Edit) bool {
	if _, done := t.Outcome("version"); done || t.Current != "" {
		return true
	}
	for _, edit := range edits {
		if edit.Port != t.Port || edit.Kind != EditUpdate {
			continue
		}
		if t.Release == nil || edit.Release != nil && edit.Release.Version == t.Release.Version {
			return true
		}
	}
	return false
}
