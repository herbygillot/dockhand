package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/herbygillot/dockhand/internal/exitcode"
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

// ExitCode places verification failure in its own band: not the tool,
// not the machine, not the invocation — the port does not build.
func (e *VerifyFailedError) ExitCode() int { return exitcode.Verify }

// TartPresent reports whether the local verify provider exists at all.
// Its absence is a different fact from "present but unprovisioned": a
// machine without tart cannot verify, so verification quietly leaves
// the contract (bump warns and proceeds; promote warns and allows),
// where a machine with tart and no bases is asked to provision.
func TartPresent() bool {
	_, err := exec.LookPath("tart")
	return err == nil
}

// RealVMProvider resolves the machine's verify provider — the tart
// provider assembled from the base images actually present. Both ways
// of having no environment (tart absent, tart present with no bases)
// are ErrNoEnvironment with the remedy named, which is what routes a
// bump to "the branch stands" rather than a raw exec error. It is
// wired into runstate.Context by the composition root; everything in
// this package reaches it through rs.VerifyProvider, which is what
// lets tests stand in an in-memory verifier without mutating globals.
func RealVMProvider(ctx context.Context) (verify.Verifier, error) {
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
		prov, err := rs.VerifyProvider(ctx)
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

// CancelRunning releases every running run on one commit and marks it
// canceled with the reason, returning how many were. The shared shape
// under the cancel verb and promote's cancel-and-proceed: choosing to
// promote mid-verification IS the user's answer about the running
// build, so the tool cancels it cleanly rather than making them wait
// or type a flag — friction removed locally, the note still honest.
func CancelRunning(ctx context.Context, rs *runstate.Context, repo *git.Repo, sha, reason string) (int, error) {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return 0, err
	}
	defer unlock()
	n, err := ReadNote(ctx, repo, sha)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return 0, nil
		}
		return 0, err
	}
	if !n.AnyState("running") {
		// Nothing to cancel needs no provider: a tart-less machine
		// promotes branches with settled notes all day, and CI proved
		// the eager lookup broke exactly that.
		return 0, nil
	}
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return 0, err
	}
	canceled := 0
	for plat, run := range n.Runs {
		if run.State != "running" {
			continue
		}
		if rerr := prov.Release(ctx, run.Job); rerr != nil {
			fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", run.Job.ID, rerr)
		}
		run.State, run.Detail = "canceled", reason
		n.Runs[plat] = run
		canceled++
	}
	if canceled == 0 {
		return 0, nil
	}
	return canceled, WriteNote(ctx, repo, n)
}
