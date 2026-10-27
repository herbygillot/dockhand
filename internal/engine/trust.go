package engine

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
)

// The trust rule, its attended half (the direction synthesis's "Before
// D", the person's word of 2026-10-02; Then's item 12, reshaped at the
// person's word of 2026-10-27): MacPorts' evaluator runs a Portfile's Tcl
// as the person, on the host that holds the GitHub login, so where
// dockhand evaluates on this Mac a Portfile another wrote, it says once
// whose, as a reviewer who runs port install extends that trust knowingly.
// Nothing unattended evaluates an unmerged Portfile another wrote: serve
// submits only the branches it prepared, ship and bump start their own,
// and a local check of an adopted branch, which would run it as root, is
// refused (localAllowed).

// OthersPortfiles is whose a branch's Portfiles are where they aren't the
// person's, as an adopted pull request's are its author's: "@alice's", or
// "#4711's author's" where the author wasn't kept; false for the person's
// own.
func OthersPortfiles(branch model.Branch) (string, bool) {
	pr := branch.PullRequest
	if pr == nil || !pr.Adopted {
		return "", false
	}
	if pr.Author == "" {
		return fmt.Sprintf("#%d's author's", pr.Number), true
	}
	return "@" + pr.Author + "'s", true
}

// sayWhose says once a command, where the work under ctx evaluates a
// branch's Portfiles another wrote on this Mac, whose they are; nothing
// for the person's own.
func (e *Engine) sayWhose(ctx context.Context, branch model.Branch) {
	if whose, others := OthersPortfiles(branch); others {
		sayWhoseOf(ctx, whose, branch.ShortName())
	}
}

// sayWhoseOf is sayWhose for Portfiles whose they are, of what, as a
// branch or "#35174".
func sayWhoseOf(ctx context.Context, whose, of string) {
	progress.ReportOnce(ctx, "Evaluating %s Portfiles on this Mac, for %s", whose, of)
}
