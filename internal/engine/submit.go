package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// VerifyDeferredError reports a verification that could not start —
// no bases, full slots, a mid-submit failure — after its branch was
// successfully minted. The branch stands (the git commit/push shape:
// nobody deletes the commit because the push failed), but the
// invocation's contract was mint AND submit, so the exit is nonzero.
// --no-verify narrows the contract to mint alone.
type VerifyDeferredError struct {
	Branch string
	Reason string
	// Cause is the underlying refusal when one exists — a typed
	// CapacityError is how status's deferred pump knows a full machine
	// from a missing capability.
	Cause error
}

func (e *VerifyDeferredError) Error() string {
	return fmt.Sprintf("verification not started: %s\nthe branch stands — `dockhand status` starts it when it can, or run `dockhand verify %s` yourself", e.Reason, e.Branch)
}

func (e *VerifyDeferredError) Unwrap() error { return e.Cause }

// DockhandExit reads the cause, because "the run did not start" is not one
// outcome: a full machine will free on its own, an unprovisioned
// release will not until someone provisions it, a capability refusal
// never will, and a submit that broke after the mint left half the
// work standing. All four used to answer the machine's band, which
// told a user waiting on a queue to go and fix something.
//
// The band is never the synchronous one here even when the cause could
// carry it: a deferral is by definition nobody standing there, and the
// run was recorded for status to start.
func (e *VerifyDeferredError) DockhandExit() int {
	var full *verify.CapacityError
	switch {
	case errors.As(e.Cause, &full):
		return exitcode.VerifyQueued
	case errors.Is(e.Cause, verify.ErrNoEnvironment):
		// Queued rather than refused: the run is on the note, and the
		// event that frees it is a provisioning the user may already be
		// running. The synchronous mirror — an ask with nobody to queue
		// for — is NoVerifyEnv, and cmd's table owns it.
		return exitcode.VerifyAwaitingSlot
	case errors.Is(e.Cause, verify.ErrUnsupported):
		// Nothing frees this one. The provider has said it cannot run
		// what was asked for, which is a verdict about the request.
		return exitcode.VerifyUnsupported
	}
	// The summary a multi-release verify returns carries no cause: each
	// release it counts was recorded deferred and status retries them,
	// which is what queued means. Everything else here is a submit that
	// broke after the branch was minted.
	if e.Cause == nil {
		return exitcode.VerifyQueued
	}
	return exitcode.MintedSubmitErrored
}

// Code names the deferral for a machine. The cause's own name is
// preferred where it has one: the band says a run did not start, and
// this says which of the four reasons it was.
func (e *VerifyDeferredError) Code() string {
	// Capacity first, and not through the cause's own name: a refusal
	// stamped synchronous somewhere upstream would name itself
	// "verifier-busy" while this exits queued, and a reason that
	// contradicts its band is worse than a coarse one.
	var full *verify.CapacityError
	if errors.As(e.Cause, &full) {
		return "verify-queued"
	}
	var namer exitcode.Reasoner
	if errors.As(e.Cause, &namer) {
		return namer.Code()
	}
	switch {
	case errors.Is(e.Cause, verify.ErrNoEnvironment):
		return "verify-awaiting-slot"
	case errors.Is(e.Cause, verify.ErrUnsupported):
		return "verification-unsupported"
	case e.Cause == nil:
		return "verify-queued"
	}
	return "minted-submit-errored"
}

// submit stages the minted commit's portdir out of the object database
// — the working tree is irrelevant to what the branch carries —
// submits it to the VM provider, and records the running job as the
// commit's note. Submission not starting is not a minting failure —
// the branch stands — but it is a contract failure: VerifyDeferredError
// carries that split.
func (e *Engine) submit(ctx context.Context, m *Minted, portName string, release platform.Release, trace, test bool) error {
	prov, err := e.Verifier(ctx)
	if err != nil {
		if errors.Is(err, verify.ErrNoProvider) {
			// No provider, no contract: the machine cannot verify at all,
			// so this is a --no-verify bump that says so — and the branch
			// may be promoted as it is, unverified. A machine that HAS
			// the provider and no base images is the other refusal
			// below: there the remedy is provisioning, so the contract
			// failed rather than narrowed.
			fmt.Fprintln(e.Err, "no verification possible: no local verify provider (tart)")
			fmt.Fprintf(e.Err, "the branch is unverified; you may promote it as is, or install tart and run `dockhand verify %s`\n", m.Branch)
			return nil
		}
		if errors.Is(err, verify.ErrNoEnvironment) {
			return e.queue(ctx, m, portName, release, err)
		}
		return err
	}
	// The platform resolves before anything is recorded: a run is keyed
	// by release name, and "the default" is not a key. The provider is
	// asked what it offers only when the caller named nothing, because
	// Capabilities is an interface method with no purity promise on it —
	// a provider that answered by talking to a hypervisor would be doing
	// so on every submit for an answer already in hand.
	if release.IsZero() {
		if release, err = verdict.ResolveRelease(release, prov.Capabilities().Platforms); err != nil {
			return e.queue(ctx, m, portName, release, err)
		}
	}
	root, err := e.Temp()
	if err != nil {
		return err
	}
	stage, _, err := root.MakeDir("stage-" + portName)
	if err != nil {
		return err
	}
	if err := m.Repo.Materialize(ctx, m.Sha, m.RelPort, stage); err != nil {
		return err
	}
	staged := filepath.Join(stage, filepath.FromSlash(m.RelPort))
	// mpbb's list-time exclusion, borrowed: evaluation answers
	// known_fail in a second, before any VM boots — and it answers for
	// the branch's content, which is what was materialized. The same
	// session answers use_xcode, so a port that needs a full Xcode is
	// probed for one before the build starts, not forty minutes in.
	pre := map[string]verdict.Preflight{}
	if pf, kerr := e.preflightOn(ctx, staged, release); kerr != nil {
		// An evaluation that could not run is not evidence that the port
		// declines anything, so the release goes unlisted and is
		// scheduled as an ordinary build.
		fmt.Fprintf(e.Err, "warning: pre-flight evaluation: %v\n", kerr)
	} else {
		pre[release.Name] = pf
	}
	sched := verdict.SchedulePlatforms(portName, []platform.Release{release}, pre)[0]
	if sched.Declined != nil {
		return e.recordRun(ctx, m.Repo, m.Sha, portName, release.Name, *sched.Declined, sched.Message)
	}
	job, err := prov.Submit(ctx, verify.Request{
		Ports:      []string{portName},
		Portdirs:   []string{staged},
		Platform:   release,
		Owner:      m.Repo.Root,
		Test:       test,
		NeedsXcode: sched.NeedsXcode,
	})
	if err != nil {
		// A full provider (two-slot cap), a capability refusal, or a
		// mid-submit failure: the branch is minted and the tip is
		// simply unverified. The deferred run is recorded here rather
		// than left to a later verify — a field run saw an intent-path
		// refusal show as bare "unverified" with the reason only in
		// scrollback.
		return e.queue(ctx, m, portName, release, err)
	}
	if err := e.recordRun(ctx, m.Repo, m.Sha, portName, release.Name, record.Run{
		State: record.Running, Job: job, Tested: test, Linted: true,
	}, fmt.Sprintf("verify: submitted %s on %s (job %s); `dockhand status` follows it", portName, release.Name, job.ID)); err != nil {
		// Submit-and-record is a transaction: a job whose note cannot
		// be persisted is a running VM no settlement can ever find, so
		// the compensation is release, on a context that survives the
		// caller's cancellation. Strict note validation made this path
		// reachable — a malformed existing note now refuses instead of
		// being overwritten — which is exactly when a worker must not
		// be left running behind an error return.
		if rerr := prov.Release(context.WithoutCancel(ctx), job); rerr != nil {
			return fmt.Errorf("recording the run failed (%w) and releasing %s failed too: %w — `tart delete %s` frees the slot", err, job.ID, rerr, job.ID)
		}
		return fmt.Errorf("recording the run failed; the worker was released: %w", err)
	}
	if trace {
		return e.Follow(ctx, m.Repo, m.Sha, portName, release.Name, prov, job)
	}
	return nil
}

// queue is the one way a submit gives up: the run nothing could start
// is written onto the note, and the deferral goes back to the caller.
//
// The note is what makes a deferral recoverable — `dockhand status`
// retries what it finds recorded — so it is written before the error
// travels, and a failure to write it is a warning rather than the
// answer: the branch stands either way, and losing the reason to a
// second failure would leave the tip reading as bare "unverified".
//
// A run is keyed by release, so one that never resolved a release
// cannot be recorded. That is only reachable on a machine whose
// provider offers no platform at all, where there is nothing for
// status to retry against until a base exists.
func (e *Engine) queue(ctx context.Context, m *Minted, portName string, release platform.Release, cause error) error {
	if !release.IsZero() {
		if rerr := e.recordRun(ctx, m.Repo, m.Sha, portName, release.Name, record.Run{
			State: record.Deferred, Detail: cause.Error(),
		}, ""); rerr != nil {
			fmt.Fprintf(e.Err, "warning: recording the deferred run: %v\n", rerr)
		}
	}
	return &VerifyDeferredError{Branch: m.Branch, Reason: cause.Error(), Cause: cause}
}

// recordRun writes one platform's run into the commit's note — the
// read-modify-write every per-platform update goes through — and tells
// the user what was recorded.
//
// The note half is the ledger's, lock and re-read included; the
// sentence about it is this package's, because what a verb says
// belongs to the verb.
func (e *Engine) recordRun(ctx context.Context, repo *git.Repo, sha, portName, releaseName string, r record.Run, msg string) error {
	if err := e.Ledger(repo).RecordRun(ctx, sha, portName, releaseName, r); err != nil {
		return err
	}
	if msg != "" {
		fmt.Fprintln(e.Err, msg)
	}
	return nil
}

// SubmitRelease claims one release's run for the branch and submits it,
// under the repository's submit lock from the re-read through the
// record — the claim pumpRun makes, made the same way, so a verify and
// a status over one deferred run cannot both start it. The note is
// read under the lock, because what it says outside is what a peer
// may already have changed: a run already running is left alone, and
// a deferral is re-recorded with its reason before the lock goes, so
// the record can never land on top of a peer's start. started reports
// a submit that went through, for --trace to follow once the claim is
// released.
func (e *Engine) SubmitRelease(ctx context.Context, repo *git.Repo, branch, tip, rel, portName string, r platform.Release, test bool) (started bool, err error) {
	unlock, err := repo.LockSubmit(ctx, SubmitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention — a peer's pump booting a guest — is
		// not a hung process, so the lock's own advice would mislead.
		return false, fmt.Errorf("%w: a verification is being submitted in this repository; `dockhand status` shows what it started, then `dockhand verify %s` again", lockfile.ErrHeld, branch)
	}
	if err != nil {
		return false, err
	}
	defer unlock()
	if n, nerr := e.Ledger(repo).Read(ctx, tip); nerr == nil {
		if run, ok := n.Runs[r.Name]; ok && run.State == record.Running {
			fmt.Fprintf(e.Err, "already verifying on %s (%s); `dockhand status` follows it\n",
				r.Name, time.Since(run.Job.Started).Round(time.Second))
			return false, nil
		}
	}
	err = e.submit(ctx, &Minted{
		Repo: repo, Branch: branch, Sha: tip, RelPort: rel,
	}, portName, r, false, test)
	var vde *VerifyDeferredError
	if errors.As(err, &vde) {
		if rerr := e.recordRun(ctx, repo, tip, portName, r.Name, record.Run{
			State: record.Deferred, Detail: vde.Reason,
		}, fmt.Sprintf("deferred %s: %s", r.Name, vde.Reason)); rerr != nil {
			return false, rerr
		}
		return false, err
	}
	return err == nil, err
}
