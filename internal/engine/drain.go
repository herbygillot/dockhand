package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// PumpDeferred starts what was deferred, now that this status pass
// has settled finished runs and freed their slots. Every deferred run
// gets one attempt, whatever its recorded reason — conditions change
// (a base provisioned, a slot freed), and the attempt re-records the
// truth either way. The one early exit is a typed capacity refusal:
// the machine is full, so further attempts this pass are noise. This
// is the reconciler acting, not a daemon — a field batch run sat
// eight deferred branches against an idle machine because the old
// message promised a pump that did not exist.
func (e *Engine) PumpDeferred(ctx context.Context, repo *git.Repo, branches []string) {
	// The gate is whether this machine can verify at all, asked of the
	// machine: a PATH lookup, no provider composed. Composing one lists
	// the machine's base images, which a pass with nothing to start
	// must not pay for, and a provider a test stood in is not evidence
	// about the machine. Absent tart is also a different fact from
	// present-but-unprovisioned, which the loop below reports per run.
	if !e.Tools.Have(tool.Tart) {
		return
	}
	for _, br := range branches {
		tip, err := repo.RevParse(ctx, br)
		if err != nil {
			continue // cleaned mid-pass, or never a branch
		}
		n, err := e.Ledger(repo).Read(ctx, tip)
		if err != nil {
			continue
		}
		for _, plat := range n.Platforms() {
			if n.Runs[plat].State != record.Deferred {
				continue
			}
			rel, derr := ChangedPortdirs(ctx, repo, br, tip)
			if derr != nil {
				fmt.Fprintf(e.Err, "%s: deferred %s not retried: %v\n", br, plat, derr)
				continue
			}
			release, ok := e.platformNamed(ctx, plat)
			if !ok {
				fmt.Fprintf(e.Err, "%s: deferred %s not retried: no such platform is provisioned\n", br, plat)
				continue
			}
			if e.pumpRun(ctx, repo, br, tip, rel, plat, release) {
				return
			}
		}
	}
}

// pumpRun retries one deferred run and reports whether the pass should
// stop here. The retry is a claim as much as a submit: two status
// passes sharing a checkout — two agents, which is how the tool is now
// used — both read the run as deferred, both submitted, and the second
// recordRun overwrote the first's job, leaving a worker no note
// accounted for. Schema 2 has no field to claim a run with (a peer
// binary's write round-trips the record and drops what it does not
// know), so the claim is a lock held from the re-read through the
// record: the holder re-reads the note, and a run no longer deferred
// was started or settled by the other claimant — skipped, silently,
// because that claimant announced it.
func (e *Engine) pumpRun(ctx context.Context, repo *git.Repo, br, tip, rel, plat string, release platform.Release) (stop bool) {
	// Lock order, checked against every holder at HEAD 82f2f2c. The
	// submit lock has two takers, this pump and SubmitRelease, and
	// neither takes it under any other lock. Inside it, submit takes
	// tart's admission lock (Provider.Submit, released once the guest
	// is visibly running, before stage and launch) and then the notes
	// flock (recordRun, released before the compensating Release runs)
	// — in sequence, never nested in each other. No holder of either
	// inner lock reaches back for this one, and neither inner lock is
	// taken under the other: the admission holders (Provider.Submit,
	// RunOnBase, provision's boot) never touch notes, and the notes
	// holders (settle, recordRun, SupersedeStale, CancelRunning,
	// Discard, cancel) call only Provider.Release, which is `tart stop`
	// and `tart delete` and takes no admission. submit → admission and
	// submit → notes are the only edges; there is no cycle. Why it is a
	// lock of its own and not the notes lock is on git.(*Repo).LockSubmit.
	unlock, err := repo.LockSubmit(ctx, SubmitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention: a peer mid-submit holds it past the
		// wait, and would hold it for every run after this one too.
		// The pass stops, and the peer's own status names what it
		// started. Not the lock's own text, which sends the user
		// hunting a hung process that is booting a guest on purpose.
		fmt.Fprintf(e.Err, "%s: deferred %s not retried: another dockhand is starting deferred runs in this repository; its status names what it started\n", br, plat)
		return true
	}
	if err != nil {
		fmt.Fprintf(e.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
		return true
	}
	defer unlock()
	n, err := e.Ledger(repo).Read(ctx, tip)
	if err != nil {
		// No note is a branch discarded under this pass: nothing left
		// to start. Anything else — a peer's newer schema, a corrupt
		// note, git failing — is a run this pass could not judge, and
		// the second read exists to notice a peer's writes, so it says.
		if !errors.Is(err, git.ErrNoNote) {
			fmt.Fprintf(e.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
		}
		return false
	}
	run := n.Runs[plat]
	if run.State != record.Deferred {
		return false
	}
	// The note names what this branch verifies — for a minted
	// branch, the SUBPORT the plan bumped. The portdir's base
	// name is the parent port, and submitting that would build
	// the untouched main port and call the branch verified
	// (field-caught on pcre2, whose portdir is devel/pcre).
	portName := n.Port
	if portName == "" {
		portName = filepath.Base(rel)
	}
	err = e.submit(ctx, &Minted{
		Repo: repo, Branch: br, Sha: tip, RelPort: rel,
	}, portName, release, false, run.Tested)
	var vde *VerifyDeferredError
	if errors.As(err, &vde) {
		if rerr := e.recordRun(ctx, repo, tip, portName, plat, record.Run{
			State: record.Deferred, Detail: vde.Reason, Tested: run.Tested,
		}, ""); rerr != nil {
			fmt.Fprintf(e.Err, "warning: re-recording deferred run: %v\n", rerr)
		}
		var cap_ *verify.CapacityError
		if errors.As(err, &cap_) {
			fmt.Fprintf(e.Err, "still waiting for a slot: %s on %s (and anything deferred after it)\n", br, plat)
			return true
		}
		fmt.Fprintf(e.Err, "still deferred: %s on %s — %s\n", br, plat, vde.Reason)
		return false
	}
	if err != nil {
		fmt.Fprintf(e.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
	}
	return false
}

// SubmitLockWait bounds a claimant's wait for a peer's submit — the
// pump's, and SubmitRelease's. A submit that never boots — a capacity
// refusal, a test double — is over in a couple of seconds, and waiting
// it out lets the re-read find the run started and skip cleanly; a
// submit that boots a guest holds the lock for minutes, which no
// claimant should sit through: the peer is starting the very run this
// one would have. A variable so the contention tests need not wait it
// out; those tests live in internal/cmd, which drives both claimants
// through their verbs, and they are serial by design (none calls
// t.Parallel), which is what makes assigning it from a test safe.
var SubmitLockWait = 5 * time.Second

// platformNamed resolves a run's recorded platform key against the
// provider's provisioned platforms.
func (e *Engine) platformNamed(ctx context.Context, name string) (platform.Release, bool) {
	prov, err := e.Verifier(ctx)
	if err != nil {
		return platform.Release{}, false
	}
	for _, r := range prov.Capabilities().Platforms {
		if r.Name == name {
			return r, true
		}
	}
	return platform.Release{}, false
}
