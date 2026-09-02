package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
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

// TartPresent reports whether the local verify provider exists at all,
// as the run's finder sees the machine. Its absence is a different
// fact from "present but unprovisioned": a machine without tart cannot
// verify, so verification quietly leaves the contract (bump warns and
// proceeds; promote warns and allows), where a machine with tart and
// no bases is asked to provision.
func TartPresent(tools *tool.Finder) bool {
	return tools.Have(tool.Tart)
}

// RealVMProvider builds the resolver of the machine's verify provider
// — the tart provider assembled from the base images actually present,
// found through the run's finder. Both ways of having no environment
// (tart absent, tart present with no bases) are ErrNoEnvironment with
// the remedy named, which is what routes a bump to "the branch stands"
// rather than a raw exec error. The composition root wires the
// resolver into runstate.Context; everything in this package reaches
// it through rs.VerifyProvider, which is what lets tests stand in an
// in-memory verifier without mutating globals.
func RealVMProvider(tools *tool.Finder) func(ctx context.Context) (verify.Verifier, error) {
	return func(ctx context.Context) (verify.Verifier, error) {
		if _, err := tools.Find(tool.Tart); err != nil {
			return nil, fmt.Errorf(
				"%w: tart is not installed (`port install tart`); --no-verify skips verification",
				verify.ErrNoEnvironment)
		}
		releases, err := (provision.Tart{Tools: tools}).Provisioned(ctx)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			return nil, fmt.Errorf(
				"%w: no base images; run `dockhand provision tart --macos <release>` first",
				verify.ErrNoEnvironment)
		}
		// Newest first: the provider's default is its first base, and
		// the default a quick bump wants is the current OS — the
		// mundane-build check — not the oldest. Platform-floor
		// archaeology asks for old releases by name.
		bases := make([]tart.Base, 0, len(releases))
		for i := len(releases) - 1; i >= 0; i-- {
			bases = append(bases, tart.Base{VM: tart.BaseName(releases[i]), Release: releases[i]})
		}
		return tart.Provider{Bases: bases, Tools: tools}, nil
	}
}

// CancelStale releases everything a branch's superseded commits still
// hold: running jobs are canceled, and a failed run's kept debug
// environment is released — once the branch moves past the failure,
// the environment documents code that no longer exists, and a field
// run watched one pin an admission slot forever. Staleness is judged
// by ancestry OR the branch's reflog, because the commonest way past
// a failure is an amend, which ancestry cannot see.
func CancelStale(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch, tip string) error {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	l := ledger.Open(repo)
	noted, err := l.All(ctx)
	if err != nil {
		return err
	}
	former := repo.FormerTips(ctx, branch)
	for _, sha := range noted {
		if sha == tip {
			continue
		}
		n, err := l.Read(ctx, sha)
		if err != nil || (!n.AnyState(record.Running) && !holdsEnvironment(n)) {
			continue
		}
		if !repo.IsAncestor(ctx, sha, branch) && !former[sha] {
			continue
		}
		prov, err := rs.VerifyProvider(ctx)
		if err != nil {
			return err
		}
		changed := false
		for plat, run := range n.Runs {
			switch {
			case run.State == record.Running:
				if err := prov.Release(ctx, run.Job); err != nil {
					fmt.Fprintf(rs.Err, "warning: canceling %s: %v\n", run.Job.ID, err)
				}
				run.State, run.Detail = record.Superseded, "canceled: the branch moved to "+git.Abbrev(tip)
			case run.State == record.Failed && run.Handle != "":
				if err := prov.Release(ctx, run.Job); err != nil {
					fmt.Fprintf(rs.Err, "warning: releasing kept environment %s: %v\n", run.Handle, err)
				}
				run.State, run.Handle = record.Superseded, ""
				run.Detail = "failed here, then the branch moved to " + git.Abbrev(tip) + " — kept environment released"
			default:
				continue
			}
			n.Runs[plat], changed = run, true
			fmt.Fprintf(rs.Err, "released stale verification of %s on %s (branch moved past it)\n", git.Abbrev(sha), plat)
		}
		if changed {
			if err := l.Write(ctx, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// holdsEnvironment reports whether any run still holds a kept debug
// environment — the failure side's counterpart to a running run.
func holdsEnvironment(n record.Record) bool {
	for _, r := range n.Runs {
		if r.State == record.Failed && r.Handle != "" {
			return true
		}
	}
	return false
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
	l := ledger.Open(repo)
	n, err := l.Read(ctx, sha)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return 0, nil
		}
		return 0, err
	}
	if !n.AnyState(record.Running) {
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
		if run.State != record.Running {
			continue
		}
		if rerr := prov.Release(ctx, run.Job); rerr != nil {
			fmt.Fprintf(rs.Err, "warning: releasing %s: %v\n", run.Job.ID, rerr)
		}
		run.State, run.Detail = record.Canceled, reason
		n.Runs[plat] = run
		canceled++
	}
	if canceled == 0 {
		return 0, nil
	}
	return canceled, l.Write(ctx, n)
}
