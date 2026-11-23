package model

import (
	"maps"
	"slices"
)

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
	// Shared is --shared-release.
	Shared Flag[bool] `json:",omitzero"`
	// Source is how the edit is shaped: --source, --keep-http,
	// --keep-old-checksums, --keep-modeline and --follow-moves.
	Source SourceAsk `json:",omitzero"`
	// Actions are what is asked beside the version: --with-obsolete,
	// --revbump-dependents with --except, and --closes.
	Actions ActionsAsk `json:",omitzero"`
	// Outcomes are the request's own actions' outcomes, the dependents'
	// and the tickets', once each is concluded; a target's are on it.
	Outcomes []ActionOutcome `json:",omitempty"`
	// To is ship's stage, where --to was given: commit, check or submit.
	To Flag[string] `json:",omitzero"`
	// Check is the check's place, --on or --build as one choice, and its
	// --tests.
	Check CheckAsk `json:",omitzero"`
	// Publish is the submission's: --draft, --type, --skip-notification,
	// and the tested statements as asked, until they are bound.
	Publish PublishAsk `json:",omitzero"`
}

// Flag is a flag with whether it was given: a flag left out is told from
// one given false or empty (#195). (The design calls it Asked, which a
// publication's record already names.)
type Flag[T any] struct {
	Given bool `json:",omitempty"`
	Value T    `json:",omitzero"`
}

// Set is a flag given with value.
func Set[T any](value T) Flag[T] { return Flag[T]{Given: true, Value: value} }

// Or is the flag's value where it was given, else fallback.
func (a Flag[T]) Or(fallback T) T {
	if a.Given {
		return a.Value
	}
	return fallback
}

// SourceAsk is how the edit is shaped.
type SourceAsk struct {
	Source           Flag[string] `json:",omitzero"`
	KeepHTTP         Flag[bool]   `json:",omitzero"`
	KeepOldChecksums Flag[bool]   `json:",omitzero"`
	KeepModeline     Flag[bool]   `json:",omitzero"`
	FollowMoves      Flag[bool]   `json:",omitzero"`
}

// ActionsAsk is what is asked beside the version.
type ActionsAsk struct {
	WithObsolete      Flag[bool]     `json:",omitzero"`
	RevbumpDependents Flag[bool]     `json:",omitzero"`
	Except            Flag[[]string] `json:",omitzero"`
	Closes            Flag[[]string] `json:",omitzero"`
}

// CheckAsk is the check's place and tests. The place is one choice:
// --on and --build each replace the other.
type CheckAsk struct {
	On    Flag[[]string] `json:",omitzero"`
	Build Flag[string]   `json:",omitzero"`
	Tests Flag[string]   `json:",omitzero"`
}

// PublishAsk is the submission's flags, and the tested statements as
// asked, which are bound to their files where they are bound today and
// taken out of the request then.
type PublishAsk struct {
	Draft            Flag[bool]     `json:",omitzero"`
	Type             Flag[[]string] `json:",omitzero"`
	SkipNotification Flag[bool]     `json:",omitzero"`
	NoCheck          Flag[bool]     `json:",omitzero"`
	TestedBinaries   Flag[bool]     `json:",omitzero"`
	TestedVariants   Flag[bool]     `json:",omitzero"`
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

// Unconclude makes an action owed again: its outcome goes, and for the
// version the version found current goes too, since Current concludes it
// as an outcome does (Finished). The one rule for an outcome whose ask or
// judgment changed (#194's H5, #195).
func (t *RequestTarget) Unconclude(action string) {
	t.Outcomes = slices.DeleteFunc(slices.Clone(t.Outcomes), func(o ActionOutcome) bool { return o.Action == action })
	if len(t.Outcomes) == 0 {
		t.Outcomes = nil
	}
	if action == "version" {
		t.Current = ""
	}
}

// ActionOutcome is what became of one action of a request.
type ActionOutcome struct {
	// Action is version, source, moves, obsolete, dependents or closes.
	Action string
	Result ActionResult
	Why    string `json:",omitempty"`
	// JudgedBy are the inputs a setting supplied when the action was
	// judged, each with its value then, as "Source.KeepHTTP": "true": a
	// fact of a past judgment, not configuration carried forward. The
	// outcome is judged again where one differs from what the setting
	// gives now; an input a flag gave is the ask's, and isn't here
	// (#195, Codex's read of step 3, P2-2).
	JudgedBy map[string]string `json:",omitempty"`
	// AppliedWith are the shaping asks a written edit was made with, as
	// "Source.KeepHTTP": "false", on a version refused because one of
	// them changed after the edit: given as it was again, the refusal
	// goes and the version is done again (#195, AW).
	AppliedWith map[string]string `json:",omitempty"`
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

// Clone is a deep copy of the request: its targets with their releases
// and outcomes, every ask's list, and each outcome's JudgedBy and
// AppliedWith, so a change to the copy never reaches the request it was
// made from.
func (r ChangeRequest) Clone() ChangeRequest {
	clone := r
	clone.Targets = slices.Clone(r.Targets)
	for i, target := range clone.Targets {
		if target.Release != nil {
			release := *target.Release
			clone.Targets[i].Release = &release
		}
		clone.Targets[i].Outcomes = cloneOutcomes(target.Outcomes)
	}
	clone.Outcomes = cloneOutcomes(r.Outcomes)
	clone.Actions.Except.Value = slices.Clone(r.Actions.Except.Value)
	clone.Actions.Closes.Value = slices.Clone(r.Actions.Closes.Value)
	clone.Check.On.Value = slices.Clone(r.Check.On.Value)
	clone.Publish.Type.Value = slices.Clone(r.Publish.Type.Value)
	return clone
}

// cloneOutcomes is a deep copy of outcomes, their maps included.
func cloneOutcomes(outcomes []ActionOutcome) []ActionOutcome {
	cloned := slices.Clone(outcomes)
	for i := range cloned {
		cloned[i].JudgedBy = maps.Clone(cloned[i].JudgedBy)
		cloned[i].AppliedWith = maps.Clone(cloned[i].AppliedWith)
	}
	return cloned
}
