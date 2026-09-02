package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
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

// Cancel is the one cancel: everything a branch still holds, given
// back. The superseded commits go first — workers the branch has
// already moved past — and then the tip's own, which is what a user
// typing `dockhand cancel` means. Field evidence made the case: a bad
// first attempt kicked off an hours-long universal build, and the only
// lever was tart surgery behind dockhand's back.
//
// Two things the tip can hold, and both are freed: a running job, and
// a failed run's kept debug environment — "done debugging, the slot
// back please" previously had no verb short of discarding the branch.
// The failure verdict stays; only the environment goes.
func (e *Engine) Cancel(ctx context.Context, repo *git.Repo, target string) error {
	branch, err := e.Resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	// The stale sweep releases running jobs the branch moved past; the
	// tip's own running job is the one cancel exists for.
	if err := e.SupersedeStale(ctx, repo, branch, tip); err != nil {
		return err
	}
	freed, err := e.cancelRuns(ctx, repo, tip, "canceled by the user", true)
	if errors.Is(err, git.ErrNoNote) {
		fmt.Fprintf(e.Err, "%s has no verification to cancel\n", branch)
		return nil
	}
	// The account comes before the error, the way discard's does: the
	// workers named here went back whether or not the note could be
	// rewritten to say so, and a user told only that the command failed
	// is not told which of their slots are free.
	for _, f := range freed {
		if f.Kept {
			fmt.Fprintf(e.Out, "released kept environment of %s on %s (the failed verdict stands)\n", branch, f.Platform)
			continue
		}
		fmt.Fprintf(e.Out, "canceled verification of %s on %s (worker %s released)\n", branch, f.Platform, f.Worker)
	}
	if err != nil {
		return err
	}
	if len(freed) == 0 {
		fmt.Fprintf(e.Err, "%s has no running verification or kept environment\n", branch)
	}
	return nil
}

// freed is one thing a cancel gave back: the platform whose run held
// it, the worker that went back, and whether what was released was a
// running build or a failed run's kept debug environment.
//
// Returned rather than printed because the two callers say different
// things about the same act — the verb names the branch and platform
// per line, a promotion counts them into one sentence — and neither
// wants the other's words.
type freed struct {
	Platform string
	Worker   string
	Kept     bool
}

// cancelRuns releases what one commit's note still holds and rewrites
// it to say so: every running run is canceled with the reason, and
// with kept set a failed run's debug environment is released too while
// its verdict stands. A commit with no note holds nothing, and says so
// with git.ErrNoNote — the callers differ on whether that is worth a
// sentence.
//
// The provider is resolved only once something is actually going to be
// released. A tart-less machine promotes branches with settled notes
// all day, and CI proved that an eager lookup broke exactly that.
func (e *Engine) cancelRuns(ctx context.Context, repo *git.Repo, sha, reason string, kept bool) ([]freed, error) {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	l := e.Ledger(repo)
	n, err := l.Read(ctx, sha)
	if err != nil {
		return nil, err
	}
	held := n.AnyState(record.Running) || (kept && holdsEnvironment(n))
	if !held {
		return nil, nil
	}
	prov, err := e.Verifier(ctx)
	if err != nil {
		return nil, err
	}
	var out []freed
	// In the record's own platform order, so that a commit verified on
	// several releases reports them the same way twice.
	for _, plat := range n.Platforms() {
		run := n.Runs[plat]
		switch {
		case run.State == record.Running:
			e.freeWorker(ctx, prov, run.Job, "releasing "+run.Job.ID)
			run.State, run.Detail = record.Canceled, reason
			out = append(out, freed{Platform: plat, Worker: run.Job.ID})
		case kept && run.State == record.Failed && run.Handle != "":
			e.freeWorker(ctx, prov, run.Job, "releasing kept environment "+run.Handle)
			run.Handle = ""
			run.Detail = strings.TrimSuffix(run.Detail, "\n") + " — kept environment released"
			out = append(out, freed{Platform: plat, Worker: run.Job.ID, Kept: true})
		default:
			continue
		}
		n.Runs[plat] = run
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, l.Write(ctx, n)
}

// freeWorker hands one worker back, reporting a provider that refuses
// as a warning and no more: the note is being rewritten to say the run
// is over either way, and a worker the provider will not free is a
// fact about the machine rather than a reason to leave the record
// claiming a build that no longer runs. what names the act, because
// canceling a build and releasing a kept environment are the same call
// and not the same news.
func (e *Engine) freeWorker(ctx context.Context, prov verify.Verifier, job verify.Job, what string) {
	if err := prov.Release(ctx, job); err != nil {
		fmt.Fprintf(e.Err, "warning: %s: %v\n", what, err)
	}
}

// SupersedeStale releases everything a branch's superseded commits
// still hold: running jobs are canceled, and a failed run's kept debug
// environment is released — once the branch moves past the failure,
// the environment documents code that no longer exists, and a field
// run watched one pin an admission slot forever. Staleness is judged
// by ancestry OR the branch's reflog, because the commonest way past
// a failure is an amend, which ancestry cannot see.
//
// It is its own pass rather than cancelRuns over each stale sha: what
// happened to those runs is supersession, not cancellation, and the
// note says which — the tip was replaced, nobody asked for a stop.
func (e *Engine) SupersedeStale(ctx context.Context, repo *git.Repo, branch, tip string) error {
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	l := e.Ledger(repo)
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
		prov, err := e.Verifier(ctx)
		if err != nil {
			return err
		}
		changed := false
		for plat, run := range n.Runs {
			switch {
			case run.State == record.Running:
				e.freeWorker(ctx, prov, run.Job, "canceling "+run.Job.ID)
				run.State, run.Detail = record.Superseded, "canceled: the branch moved to "+git.Abbrev(tip)
			case run.State == record.Failed && run.Handle != "":
				e.freeWorker(ctx, prov, run.Job, "releasing kept environment "+run.Handle)
				run.State, run.Handle = record.Superseded, ""
				run.Detail = "failed here, then the branch moved to " + git.Abbrev(tip) + " — kept environment released"
			default:
				continue
			}
			n.Runs[plat], changed = run, true
			fmt.Fprintf(e.Err, "released stale verification of %s on %s (branch moved past it)\n", git.Abbrev(sha), plat)
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
