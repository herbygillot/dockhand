package branchstate

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Disposition is where a prepared branch stands before its check.
type Disposition string

const (
	// ReadyToCheck is a branch whose check may be queued.
	ReadyToCheck Disposition = "ready"
	// HeldBeforeCheck is one an unattended ship held at its committed
	// edit, a patch not applying (model.Edit.Held).
	HeldBeforeCheck Disposition = "held"
	// HandEditsToCarry is one whose worktree holds edits not committed,
	// the person's to carry on.
	HandEditsToCarry Disposition = "hand-edits"
)

// Prepared is a branch's preparation verdict, and why where it isn't
// ready.
type Prepared struct {
	Disposition Disposition
	Why         string
}

// Preparation is the one verdict on whether a branch's check may be
// queued: ship's pre-check decision and serve's recovery both ask it, where
// recovery queued the check of a branch ship had held for a patch that
// doesn't apply (Codex's review of 275125ff, finding 2). Edits not
// committed are the person's; the newest edit of each port held is held,
// as no edit after it took it on.
func Preparation(b Branch) Prepared {
	if len(b.Edited) > 0 {
		return Prepared{Disposition: HandEditsToCarry, Why: "its worktree holds edits not committed"}
	}
	newest := map[string]model.Edit{}
	var ports []string
	for _, edit := range b.Edits {
		if _, seen := newest[edit.Port]; !seen {
			ports = append(ports, edit.Port)
		}
		newest[edit.Port] = edit
	}
	var held []string
	for _, port := range ports {
		held = append(held, newest[port].Held...)
	}
	if len(held) > 0 {
		return Prepared{Disposition: HeldBeforeCheck, Why: strings.Join(held, "; ")}
	}
	return Prepared{Disposition: ReadyToCheck}
}

// HeldWords say a branch held before its check, and what lets it build.
func HeldWords(why string) string {
	return fmt.Sprintf("held before its check, at its committed edit: %s; holds.allow = [\"patch-rejected\"] lets it build", why)
}
