package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// followPoll is how often a watched build is asked whether it has
// finished. Deliberately slower than the gate's wait: the gate is the
// only thing standing between a user and their answer, while a follow
// is already printing the guest's own output as evidence that
// something is happening.
const followPoll = 4 * time.Second

// Follow streams a running build's log as the guest writes it, then
// settles the run through the same machinery status uses — the
// --trace contract: don't exit, watch. Ctrl-C detaches; the build
// continues without us, which is the submit-and-poll design keeping
// its promise even while we happen to be watching.
func (e *Engine) Follow(ctx context.Context, repo *git.Repo, sha, portName, plat string, prov verify.Verifier, job verify.Job) error {
	fmt.Fprintf(e.Err, "following %s on %s — Ctrl-C detaches, the build continues\n", portName, plat)
	const detached = "detached; `dockhand status` follows it from here"
	_, finished, err := e.stream(ctx, prov, job, detached)
	if err != nil {
		if ctx.Err() == nil {
			return err
		}
		// The poll failed because we are being interrupted, not because
		// the machine has anything to report. Only this follow reads it
		// that way: it is watching a run it recorded, so the run outlives
		// the interrupt and status will finish the sentence.
		fmt.Fprintln(e.Err, detached)
		return nil
	}
	if !finished {
		return nil
	}
	n, err := e.Ledger(repo).LoadOrStart(ctx, sha, portName)
	if err != nil {
		return err
	}
	if err := e.settle(ctx, repo, &n); err != nil {
		return err
	}
	r := n.Runs[plat]
	switch r.State {
	case record.Passed:
		fmt.Fprintf(e.Err, "passed on %s; worker released\n", plat)
		return nil
	case record.Unsupported:
		fmt.Fprintf(e.Err, "%s declines %s: %s\n", portName, plat, r.Detail)
		return nil
	case record.Failed:
		return &VerifyFailedError{Port: portName, Handle: r.Handle}
	case record.Running, record.Blocked, record.Canceled, record.Superseded,
		record.Deferred, record.Errored:
	}
	// Everything else, an unknown state included: the follow watched the
	// job to its end and the settle still reached no verdict about the
	// port, which makes it the machine's answer rather than the port's.
	return fmt.Errorf("%w: %s", verify.ErrNoEnvironment, r.Detail)
}

// FollowStarted streams the run a submit has just started — after the
// claim on it is released, because a build's forty minutes must hold
// no lock a peer's status pump waits on. The job is read back from the
// note the submit recorded rather than passed along, since a submit
// the pre-flight settled without a build (known_fail, recorded
// unsupported) leaves nothing running to follow and nothing to say.
func (e *Engine) FollowStarted(ctx context.Context, repo *git.Repo, tip, portName, plat string, prov verify.Verifier) error {
	n, err := e.Ledger(repo).Read(ctx, tip)
	if err != nil {
		return err
	}
	run, ok := n.Runs[plat]
	if !ok || run.State != record.Running {
		return nil
	}
	return e.Follow(ctx, repo, tip, portName, plat, prov, run.Job)
}

// Trace is the follow that keeps no record: `log --trace` watches an
// environment and writes nothing, because the environment it was given
// may not even belong to this repository — a pre-mint failure's kept
// worker has no branch and no note to settle. Verdict-keeping stays
// with status, which the closing sentence hands off to.
//
// A poll that fails is this watcher's answer and comes back as one,
// where the recording follow reads the same failure under an interrupt
// as a detach. The two differ because the run does: a followed build
// was recorded and status can still finish the sentence, while an
// environment handed to `log --trace` has nobody else to report it.
func (e *Engine) Trace(ctx context.Context, prov verify.Verifier, job verify.Job) error {
	st, finished, err := e.stream(ctx, prov, job, "detached; the build continues")
	if err != nil || !finished {
		return err
	}
	fmt.Fprintf(e.Err, "build finished: %s; `dockhand status` records it\n", st.State)
	return nil
}

// stream prints a job's log to stdout as the guest grows it, until the
// job reaches a terminal state, and reports which of the two ends
// happened: the build finished, or the user detached. A detach is not
// a failure — the build outlives us by design — so it comes back as
// finished false and no error, with detached said on the way out.
//
// A poll that fails comes back as the error it is. What an interrupted
// poll means is the caller's to decide and not the loop's: the two
// watchers disagree about it, and folding their answer in here is how
// one of them silently acquired the other's.
//
// The sentence is the caller's because the two follows promise
// different things about what happens next, and it is said here
// because both of the loop's own ways out are here.
func (e *Engine) stream(ctx context.Context, prov verify.Verifier, job verify.Job, detached string) (verify.Status, bool, error) {
	printed := 0
	for {
		st, err := prov.Poll(ctx, job)
		if err != nil {
			return verify.Status{}, false, err
		}
		if log, lerr := prov.Log(ctx, job); lerr == nil && len(log) > printed {
			fmt.Fprint(e.Out, log[printed:])
			printed = len(log)
		}
		if st.State.Terminal() {
			return st, true, nil
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(e.Err, detached)
			return verify.Status{}, false, nil
		case <-time.After(followPoll):
		}
	}
}
