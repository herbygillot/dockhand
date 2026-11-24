package branchstate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Disposition is where a prepared branch stands before its check.
type Disposition string

const (
	// ReadyToCheck is a branch whose check may be queued.
	ReadyToCheck Disposition = "ready"
	// HeldBeforeCheck is one a caller nobody attends stops before its
	// check: a patch rejection stands that holds.allow doesn't let go
	// (StandingRejections).
	HeldBeforeCheck Disposition = "held"
	// HandEditsToCarry is one whose worktree holds edits not committed,
	// the person's to carry on.
	HandEditsToCarry Disposition = "hand-edits"
)

// HeldWords say a branch held before its check, whether its changes are
// committed, and what lets it build.
func HeldWords(reasons []string, committed bool) string {
	state := "committed"
	if !committed {
		state = "not committed"
	}
	return fmt.Sprintf("held before its check, with its changes %s: %s; holds.allow = [\"patch-rejected\"] lets it build", state, strings.Join(reasons, "; "))
}

// UnfinishedPreparation is a branch whose preparation stopped part-way: an edit not
// written, a target not prepared, an action not concluded or refused, or
// dockhand's own edits not committed (#194).
const UnfinishedPreparation Disposition = "unfinished"

// UnwrittenEdit is an edit recorded before its files were written, with
// the files settling found neither as recorded before nor after: a file
// it can't be settled over, which is the person's.
type UnwrittenEdit struct {
	Edit    model.Edit
	Changed []string
}

// Readying is what preparation's verdict reads (docs/designs/
// preparation-and-request.md at 6829407e, "Readiness"): the edits written and not,
// the uncommitted paths the edits explain, as tidy tells them, and the
// others, what was asked, whether a person is there, and the kinds each
// port's holds.allow names now.
type Readying struct {
	Edits     []model.Edit
	Unwritten []UnwrittenEdit
	Explained []string
	Others    []string
	Request   *model.ChangeRequest
	Attended  bool
	Allowed   func(port string) []string
}

// Readied is the verdict with every reason for it, and whether the edits
// are committed.
type Readied struct {
	Disposition Disposition
	Reasons     []string
	Committed   bool
	// Pending is an unfinished answer for more than dockhand's own edits
	// not committed: an edit not written, a target not prepared, or an
	// action not concluded or refused, which status says (the design's
	// table); edits not committed alone are a person's to check as they
	// are, and ship's to commit.
	Pending bool
}

// PreparationOf answers the first that applies, in order: hand-edits,
// unfinished, held, ready. Unfinished comes before held, since serve
// commits held work and holds it at its check. Each port is judged by its
// own edits, so the order of updates doesn't matter.
func PreparationOf(in Readying) Readied {
	committed := len(in.Explained) == 0
	var reasons []string
	reasons = append(reasons, in.Others...)
	for _, unwritten := range in.Unwritten {
		for _, file := range unwritten.Changed {
			reasons = append(reasons, fmt.Sprintf("%s changed since %s's update was prepared", file, unwritten.Edit.Port))
		}
	}
	if len(reasons) > 0 {
		return Readied{Disposition: HandEditsToCarry, Reasons: reasons, Committed: committed}
	}
	if reasons = unfinishedReasons(in); len(reasons) > 0 {
		pending := len(in.Unwritten) > 0 || len(reasons) > 1 || len(in.Explained) == 0
		return Readied{Disposition: UnfinishedPreparation, Reasons: reasons, Committed: committed, Pending: pending}
	}
	if !in.Attended {
		for _, port := range portsOf(in.Edits) {
			if in.Allowed != nil && slices.Contains(in.Allowed(port), patchRejected) {
				continue
			}
			for _, patch := range StandingRejections(port, in.Edits) {
				reasons = append(reasons, rejectionWords(port, patch))
			}
		}
	}
	if len(reasons) > 0 {
		return Readied{Disposition: HeldBeforeCheck, Reasons: reasons, Committed: committed}
	}
	return Readied{Disposition: ReadyToCheck, Committed: committed}
}

// patchRejected is the hold kind holds.allow names to let a port whose
// patch doesn't apply build unattended (assess.PatchRejected).
const patchRejected = model.FindingPatchRejected

// unfinishedReasons are what keeps the branch's preparation from being
// finished.
func unfinishedReasons(in Readying) []string {
	var reasons []string
	for _, unwritten := range in.Unwritten {
		reasons = append(reasons, fmt.Sprintf("an update of %s wasn't finished", unwritten.Edit.Port))
	}
	if in.Request != nil {
		for _, target := range in.Request.Targets {
			for _, outcome := range target.Outcomes {
				if outcome.Result == model.ActionRefused {
					reasons = append(reasons, fmt.Sprintf("%s of %s was refused: %s", outcome.Action, target.Port, outcome.Why))
				}
			}
			if !target.Finished(in.Edits) {
				reasons = append(reasons, fmt.Sprintf("%s has no update yet", target.Port))
			}
		}
	}
	if len(in.Explained) > 0 {
		reasons = append(reasons, "dockhand's edits of "+strings.Join(in.Explained, ", ")+" aren't committed")
	}
	return reasons
}

// portsOf are the ports edits change, in the order first edited.
func portsOf(edits []model.Edit) []string {
	var ports []string
	for _, edit := range edits {
		if !slices.Contains(ports, edit.Port) {
			ports = append(ports, edit.Port)
		}
	}
	return ports
}

// Rejection is a patch rejection that stands: the patch, and the scope
// its list was read in; a rejection from before #194 has neither.
type Rejection struct {
	Patch, Scope string
}

// StandingRejections are a port's patch rejections that stand. One stands
// from the update edit that recorded it until a later update edit of the
// port, with a list read in the same scope, records that patch applied or
// doesn't declare it. A nil list, an unchecked patch, or a list read in
// another scope changes nothing; a rejection from before, with no patch
// and no scope, stands until the person ends it (#194, P5 to P8).
func StandingRejections(port string, edits []model.Edit) []Rejection {
	var standing []Rejection
	for _, edit := range edits {
		if edit.Port != port || edit.Kind != model.EditUpdate || edit.Result == nil {
			continue
		}
		if list := edit.Result.Patches; list != nil {
			standing = slices.DeleteFunc(standing, func(r Rejection) bool {
				if !model.ScopesComparable(r.Scope, list.Scope) {
					return false
				}
				i := slices.IndexFunc(list.Declared, func(p model.PatchResult) bool { return p.Name == r.Patch })
				return i < 0 || list.Declared[i].State == model.PatchApplied
			})
			for _, patch := range list.Declared {
				if patch.State == model.PatchRejected && !slices.Contains(standing, Rejection{Patch: patch.Name, Scope: list.Scope}) {
					standing = append(standing, Rejection{Patch: patch.Name, Scope: list.Scope})
				}
			}
		}
		for _, finding := range edit.Result.Findings {
			if finding.Kind == model.FindingPatchRejected && finding.Subject == "" && !slices.Contains(standing, Rejection{}) {
				standing = append(standing, Rejection{})
			}
		}
	}
	if len(standing) == 0 {
		return nil
	}
	return standing
}

// rejectionWords say a standing rejection.
func rejectionWords(port string, r Rejection) string {
	if r.Patch == "" {
		return port + ": a patch didn't apply when the branch was prepared"
	}
	return fmt.Sprintf("%s: patch %s doesn't apply", port, r.Patch)
}
