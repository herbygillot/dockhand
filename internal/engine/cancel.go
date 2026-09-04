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

// DockhandExit places verification failure in the verdict band: not the
// tool, not the machine, not the invocation — the run answered, and
// the port does not build. It shares the code with promote's refusal
// over one, which is that same answer being enforced.
func (e *VerifyFailedError) DockhandExit() int { return exitcode.VerifyFailed }

// Code names the outcome for a machine.
func (e *VerifyFailedError) Code() string { return "verification-failed" }

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

// freed is one thing a cancel gave back: the platform whose guest held
// it, the worker that went back, and whether what was released was a
// running build or a failed run's kept debug environment.
//
// One entry per release and not per run, because one guest is one
// thing: a cohort of nine canceled on one platform freed one worker,
// and nine lines saying so would be nine lies about the slot count.
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
// with keepToo set a failed run's debug environment is released too
// while its verdict stands. A commit with no note holds nothing, and
// says so with git.ErrNoNote — the callers differ on whether that is
// worth a sentence.
//
// The provider is resolved only once something is actually going to be
// released. A tart-less machine promotes branches with settled notes
// all day, and CI proved that an eager lookup broke exactly that.
//
// It releases before it writes, which is the order settle is forbidden.
// The difference is the lock: this whole function runs inside the notes
// flock, so no peer can read "finished" and release the same guest in
// between, and the ledger's own release right — which takes that same
// flock — cannot be asked for from in here without waiting itself out.
func (e *Engine) cancelRuns(ctx context.Context, repo *git.Repo, sha, reason string, keepToo bool) ([]freed, error) {
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
	held := n.AnyState(record.Running) || (keepToo && holdsEnvironment(n))
	if !held {
		return nil, nil
	}
	prov, err := e.Verifier(ctx)
	if err != nil {
		return nil, err
	}
	var out []freed
	// In release order, so that a commit verified on several platforms
	// reports them the same way twice.
	for _, rel := range releasesIn(n) {
		job, ok := n.Jobs[rel]
		if !ok {
			// A queued run holds nothing: it was never submitted, and
			// there is no environment behind it to give back.
			continue
		}
		switch {
		case anyRunning(n, rel):
			e.freeWorker(ctx, prov, job.Job, "releasing "+job.Job.ID)
			for _, ref := range runsOn(n, rel) {
				if ref.Run.State != record.Running {
					continue
				}
				run := ref.Run
				run.State, run.Detail = record.Canceled, reason
				n.Runs[ref.Key()] = run
			}
			job.Released = true
			n.Jobs[rel] = job
			out = append(out, freed{Platform: rel, Worker: job.Job.ID})
		case keepToo && keepsEnvironment(job):
			e.freeWorker(ctx, prov, job.Job, "releasing kept environment "+job.Handle)
			for _, ref := range runsOn(n, rel) {
				if ref.Run.State != record.Failed {
					continue
				}
				run := ref.Run
				run.Detail = strings.TrimSuffix(run.Detail, "\n") + " — kept environment released"
				n.Runs[ref.Key()] = run
			}
			// The flag goes down and the name stays. What was handed back
			// is still worth naming to whoever has to go and delete it if
			// the provider refused.
			job.Released = true
			n.Jobs[rel] = job
			out = append(out, freed{Platform: rel, Worker: job.Job.ID, Kept: true})
		default:
			continue
		}
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
		for _, rel := range releasesIn(n) {
			job, ok := n.Jobs[rel]
			if !ok {
				continue
			}
			running := anyRunning(n, rel)
			switch {
			case running:
				e.freeWorker(ctx, prov, job.Job, "canceling "+job.Job.ID)
			case keepsEnvironment(job):
				e.freeWorker(ctx, prov, job.Job, "releasing kept environment "+job.Handle)
			default:
				continue
			}
			for _, ref := range runsOn(n, rel) {
				run := ref.Run
				switch run.State {
				case record.Running:
					run.State, run.Detail = record.Superseded, "canceled: the branch moved to "+git.Abbrev(tip)
				case record.Failed:
					run.State = record.Superseded
					run.Detail = "failed here, then the branch moved to " + git.Abbrev(tip) + " — kept environment released"
				case record.Queued, record.Submitting, record.Passed, record.Unsupported,
					record.Blocked, record.Canceled, record.Superseded, record.Errored,
					record.Withheld:
					// Nothing this sweep supersedes. A run that never
					// started holds nothing, and one that already reached a
					// verdict about a commit the branch has moved past is
					// still what was learned there.
					continue
				}
				n.Runs[ref.Key()] = run
			}
			job.Released = true
			n.Jobs[rel], changed = job, true
			fmt.Fprintf(e.Err, "released stale verification of %s on %s (branch moved past it)\n", git.Abbrev(sha), rel)
		}
		if changed {
			if err := l.Write(ctx, n); err != nil {
				return err
			}
		}
	}
	return nil
}
