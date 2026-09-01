package lifecycle

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// VerifyFailedError reports a verification that ran to completion and
// found the port does not build. It is its own type because it is its
// own kind of outcome — not the tool failing, not the machine, not the
// invocation — and the exit table gives it its own code.
type VerifyFailedError struct {
	Port   string
	Handle string
}

func (e *VerifyFailedError) Error() string {
	msg := fmt.Sprintf("verification failed: %s does not build", e.Port)
	if e.Handle != "" {
		msg += fmt.Sprintf(" (environment kept: %s)", e.Handle)
	}
	return msg
}

// TartPresent reports whether the local verify provider exists at all.
// Its absence is a different fact from "present but unprovisioned": a
// machine without tart cannot verify, so verification quietly leaves
// the contract (bump warns and proceeds; promote warns and allows),
// where a machine with tart and no bases is asked to provision.
func TartPresent() bool {
	_, err := exec.LookPath("tart")
	return err == nil
}

// VMProvider resolves the machine's verify provider — the tart
// provider assembled from the base images actually present. Both ways
// of having no environment (tart absent, tart present with no bases)
// are ErrNoEnvironment with the remedy named, which is what routes a
// bump to "the branch stands" rather than a raw exec error. A variable
// so the lifecycle tests can stand in an in-memory verifier: the seam
// is what makes settle, discard, and follow testable without a VM.
var VMProvider func(ctx context.Context) (verify.Verifier, error) = realVMProvider

func realVMProvider(ctx context.Context) (verify.Verifier, error) {
	if _, err := exec.LookPath("tart"); err != nil {
		return nil, fmt.Errorf(
			"%w: tart is not installed (`port install tart`); --no-verify skips verification",
			verify.ErrNoEnvironment)
	}
	releases, err := (provision.Tart{}).Provisioned(ctx)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf(
			"%w: no base images; run `dockhand provision tart --macos <release>` first",
			verify.ErrNoEnvironment)
	}
	// Newest first: the provider's default is its first base, and the
	// default a quick bump wants is the current OS — the mundane-build
	// check — not the oldest. Platform-floor archaeology asks for old
	// releases by name.
	bases := make([]tart.Base, 0, len(releases))
	for i := len(releases) - 1; i >= 0; i-- {
		bases = append(bases, tart.Base{VM: tart.BaseName(releases[i]), Release: releases[i]})
	}
	return tart.Provider{Bases: bases}, nil
}

// CancelStale releases every running job recorded on a commit the
// branch once pointed at but no longer does — reachable ancestors and
// amended-away shas alike — and marks their notes superseded by the
// tip about to be submitted.
func CancelStale(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch, tip string) error {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return err
	}
	for _, sha := range noted {
		if sha == tip {
			continue
		}
		n, err := ReadNote(ctx, repo, sha)
		if err != nil || !n.AnyState("running") || !repo.IsAncestor(ctx, sha, branch) {
			continue
		}
		prov, err := VMProvider(ctx)
		if err != nil {
			return err
		}
		changed := false
		for plat, run := range n.Runs {
			if run.State != "running" {
				continue
			}
			if err := prov.Release(ctx, run.Job); err != nil {
				fmt.Fprintf(rs.Err, "warning: canceling %s: %v\n", run.Job.ID, err)
			}
			run.State, run.Detail = "superseded", "canceled: the branch moved to "+tip[:12]
			n.Runs[plat], changed = run, true
			fmt.Fprintf(rs.Err, "canceled stale verification of %s on %s (branch moved past it)\n", sha[:12], plat)
		}
		if changed {
			if err := WriteNote(ctx, repo, n); err != nil {
				return err
			}
		}
	}
	return nil
}
