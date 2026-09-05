package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// rosterAsRecorded shapes a cohort's roster for a resubmission from what
// the note already recorded about each member, rather than from the tree
// the members were derived off.
//
// The deferral roads — the drain's pump and a hand `dockhand verify
// <branch>` — resolve a cohort's members by walking every changed
// portdir, which is right for what to build and wrong for two members
// the cohort's acceptance already decided about. A withheld member's
// Portfile was bumped by the cohort commit, so it is a changed portdir
// and comes back in the walk, but D24 keeps it out of the guest — this
// drops it, and hands it back to be recorded withheld under the release
// this attempt resolves, so its line is on the body whichever road
// started the build. A forced member (the D24 override) must go back
// last, after every member that might need the sibling it deactivates,
// and carry that sibling — this moves it to the end in name order and
// returns the map the submission deactivates by.
//
// Both facts are read from the record twice over, and the run comes
// first. A run is written when a submission resolves a release — a
// withheld run for the member kept out, a run carrying Forced for the
// one seated last — and where one exists it is what the last attempt
// actually did. The accepted proposal's candidates on the note say the
// same thing from the other side (Solo, Over, Forced), and they are
// what remains when no submission ever happened: a cohort accepted with
// --no-verify, or one queued before any release resolved, has no runs
// to read, and a road that read only runs would seat the withheld member
// beside its sibling and drop the person's override on the floor.
//
// On a note with no withheld and no forced member — every deferral there
// was before this feature, and every single-subject one — it returns the
// members unchanged and nil for the rest, so the ordinary retry is
// untouched.
func rosterAsRecorded(n record.Record, release string, members []Member) ([]Member, map[string]string, []WithheldMember) {
	var kept, tail []Member
	var held []WithheldMember
	forced := map[string]string{}
	cands := map[string]record.Candidate{}
	for _, f := range n.Findings {
		if f.Kind != render.KindCohort {
			continue
		}
		for _, c := range f.Candidates {
			cands[strings.ToLower(c.Port)] = c
		}
	}
	for _, m := range members {
		lp := strings.ToLower(m.Port)
		run, ran := n.Runs[record.RunKey(m.Port, release)]
		c, proposed := cands[lp]
		switch {
		case ran && run.State == record.Withheld:
			// Bumped, never built: the guest must not hold it beside its
			// sibling, whatever the walk turned up. Recorded again under
			// this attempt's release, which is the same key it already
			// has.
			held = append(held, WithheldMember{Port: m.Port, Why: run.Detail})
		case ran && run.Forced != "":
			forced[lp] = run.Forced
			tail = append(tail, m)
		case !ran && proposed && c.Proposed && c.Solo && c.Forced && c.Over != "":
			forced[lp] = c.Over
			tail = append(tail, m)
		case !ran && proposed && c.Proposed && c.Solo:
			held = append(held, WithheldMember{Port: m.Port, Why: withheldWhy(c)})
		default:
			kept = append(kept, m)
		}
	}
	sort.Slice(tail, func(i, j int) bool { return tail[i].Port < tail[j].Port })
	if len(forced) == 0 {
		forced = nil
	}
	return append(kept, tail...), forced, held
}

// PumpDeferred starts what was queued, now that this pass has settled
// finished runs and freed their slots (D27: the drain is `cycle`'s;
// `status` reports the queue and names the verb). Every queued run
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
	//
	// Without tart the walk still reads the notes — a note read is not
	// a provider call — so that a queue nothing could start is said to
	// be one, once, at the end. Silence here left `cycle`'s own report
	// naming `dockhand cycle` beside a queued run that this very cycle
	// had not started, with nothing beside it saying why.
	tartHere := e.Tools.Have(tool.Tart)
	waiting := 0
	defer func() {
		if waiting > 0 {
			fmt.Fprintf(e.Err, "nothing started: tart is not on PATH; queued runs on %d branch(es) wait for it\n", waiting)
		}
	}()
	for _, br := range branches {
		tip, err := repo.RevParse(ctx, br)
		if err != nil {
			continue // cleaned mid-pass, or never a branch
		}
		n, err := e.Ledger(repo).Read(ctx, tip)
		if err != nil {
			continue
		}
		if n.Destination == record.ToBranch {
			// Nobody asked for a verdict about this change. A branch is
			// where its contract stops, and a pump that started a build
			// anyway would be inventing the ask — spending a slot, and an
			// hour of the machine, on an answer the user declined.
			continue
		}
		if n.SupersededBy != "" {
			// A newer sibling is the change now, and the ruling is that
			// nothing but `cycle --superseded` touches this branch. The
			// supersede marks the record and cancels no runs — it cannot,
			// because it happens at another branch's mint — so a replaced
			// branch keeps whatever runs it had queued, and a pump without
			// this line spends a VM slot and an hour of the machine building
			// a change that has already been replaced.
			//
			// Silent, like the destination skip above and unlike the hold: a
			// hold is a person's act and the pass reports obeying one, while
			// a supersede is a fact the branch's own status line already
			// states, once, rather than every ten minutes.
			continue
		}
		if !tartHere {
			// Counted and not attempted: no platform is asked for and
			// no provider composed. Before the hold, because a machine
			// that cannot verify has nothing to obey a hold about.
			if n.AnyState(record.Queued) {
				waiting++
			}
			continue
		}
		if err := GateHold(n, br, "the verification"); err != nil {
			// Beside the destination check and on the same argument: a
			// held change must not spend a slot and an hour of the
			// machine. Said out loud, unlike the destination skip above,
			// because a hold is a person's act and this is the pass
			// reporting that it obeyed one — the same voice the retry
			// failures below are reported in.
			fmt.Fprintln(e.Err, err)
			continue
		}
		// Over the runs and not the platforms: a queued run was never
		// submitted, so no job names its release and Platforms would
		// answer with the empty set for precisely the records this pass
		// exists to find.
		//
		// One attempt per release per branch, and not one per run. A
		// cohort's members share a guest, so the first queued run on a
		// release is a claim on all of them and the submit it makes covers
		// the rest; deriving the change again for every member would run
		// the diff and the evaluation N times over to ask for one
		// environment. A change with one subject has one run per release
		// and never reaches the guard.
		tried := map[string]bool{}
		for _, ref := range runRefs(n) {
			if ref.Run.State != record.Queued || tried[ref.Release] {
				continue
			}
			tried[ref.Release] = true
			plat := ref.Release
			members, derr := e.deferredMembers(ctx, repo, br, tip, n, ref)
			if derr != nil {
				fmt.Fprintf(e.Err, "%s: deferred %s not retried: %v\n", br, plat, derr)
				continue
			}
			release, ok := e.platformNamed(ctx, plat)
			if !ok {
				fmt.Fprintf(e.Err, "%s: deferred %s not retried: no such platform is provisioned\n", br, plat)
				continue
			}
			if e.pumpRun(ctx, repo, br, tip, members, ref, release) {
				return
			}
		}
	}
}

// deferredMembers is what a retry of one queued run actually submits.
//
// At one subject it is today's answer and reaches nothing new: the
// run's own port and the portdir its subject recorded, with git
// standing in for a portdir a hand-made branch's subject never had.
// The record is asked first precisely so that the ordinary case never
// runs a diff — and so that the plural cross-check, which can refuse,
// is never in front of a retry that works today.
//
// A change whose record names several portdirs is a cohort, and its
// queued runs were deferred together in one environment and go back
// together: the whole member set is derived, and the run this pass was
// walking is a claim on the cohort rather than on a member. The build
// order is the derivation's and is not rearranged to suit whichever
// run the walk reached first — a dependent built before its dependency
// is not the same build.
func (e *Engine) deferredMembers(ctx context.Context, repo *git.Repo, branch, tip string, n record.Record, ref runRef) ([]Member, error) {
	if ref.Portdir != "" && len(n.Portdirs()) < 2 {
		return []Member{{Port: ref.Port, Portdir: ref.Portdir}}, nil
	}
	rels, err := e.ChangedPortdirs(ctx, repo, branch, tip)
	if err != nil {
		return nil, err
	}
	if len(rels) == 1 {
		// The note names what this branch verifies — for a minted
		// branch, the SUBPORT the plan bumped. The portdir's base name is
		// the parent port, and submitting that would build the untouched
		// main port and call the branch verified (field-caught on pcre2,
		// whose portdir is devel/pcre).
		port := ref.Port
		if port == "" {
			port = filepath.Base(rels[0])
		}
		return []Member{{Port: port, Portdir: rels[0]}}, nil
	}
	// The branch stands in for the target: a pump has nobody at the
	// keyboard to have named one, so every member is resolved from the
	// record or from the directory itself.
	return e.SubjectsOf(ctx, repo, branch, branch, tip, rels)
}

// pumpRun retries one queued run and reports whether the pass should
// stop here. The retry is a claim as much as a submit: two cycle
// passes sharing a checkout — two agents, which is how the tool is now
// used — both read the run as queued, both submitted, and the second
// write overwrote the first's job, leaving a worker no note accounted
// for. Schema 3 has the field to claim with — the job's own claim, and
// the submitting state a claimed run reads — but the protocol that
// reads them is a concurrency change of its own, so what still enforces
// the claim is this lock, held from the re-read through the record: the
// holder re-reads the note, and a run no longer queued was started or
// settled by the other claimant — skipped, silently, because that
// claimant announced it.
func (e *Engine) pumpRun(ctx context.Context, repo *git.Repo, br, tip string, members []Member, was runRef, release platform.Release) (stop bool) {
	plat := was.Release
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
	// Discard, cancel) call only Provider.Release, which is the
	// provider's own stop-and-delete and takes no admission. submit → admission and
	// submit → notes are the only edges; there is no cycle. Why it is a
	// lock of its own and not the notes lock is on git.(*Repo).LockSubmit.
	unlock, err := repo.LockSubmit(ctx, SubmitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention: a peer mid-submit holds it past the
		// wait, and would hold it for every run after this one too.
		// The pass stops, and `dockhand status` shows what the peer
		// started once it has. Not the lock's own text, which sends the
		// user hunting a hung process that is booting a guest on purpose.
		fmt.Fprintf(e.Err, "%s: deferred %s not retried: another dockhand is starting deferred runs in this repository; `dockhand status` shows what it started\n", br, plat)
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
	// The hold, again, over the note this pass just re-read.
	//
	// Not belt and braces. The lock is held across the re-read precisely
	// so that a peer's write between the walk and the submit is honoured,
	// and a hold placed in that window is exactly such a write — a person
	// running `dockhand hold` while a cycle pass is walking the namespace
	// is the ordinary way it happens. The walk's check saves the work; this
	// one is the one that is authoritative.
	if herr := GateHold(n, br, "the verification"); herr != nil {
		fmt.Fprintln(e.Err, herr)
		return false
	}
	// And the supersede, again, for the same reason the hold is asked
	// twice: the lock is held across the re-read precisely so a peer's
	// write between the walk and the submit is honoured, and a sibling
	// minted in that window is exactly such a write.
	if n.SupersededBy != "" {
		return false
	}
	// The claim is on the run this pass was walking, whatever else
	// rides with it: a member already started or settled by a peer is
	// that peer's, and the re-read is what says so.
	portName := was.Port
	if portName == "" {
		portName = members[0].Port
	}
	run, ok := n.Runs[record.RunKey(portName, plat)]
	if !ok || run.State != record.Queued {
		return false
	}
	// What the previous attempt was for is carried into this one: the
	// archive to ignore, and the ask to keep a passing environment
	// (D27), both of which the queued run recorded for exactly this
	// retry. The test suite is not: it is asked of an environment, and a
	// queued run has no environment to have been asked of — so an
	// unattended retry submits the install this pass can honestly stand
	// behind, and the note says so rather than claiming a test nobody
	// ran.
	//
	// The headline is the cohort's and not this run's. Every member goes
	// back into one environment, and the members the walk has not
	// reached yet will find their runs no longer queued and step over
	// them.
	//
	// The roster is shaped from the record and not from the tree. The
	// members came from deferredMembers, which resolves every changed
	// portdir — including a withheld member's, whose Portfile the cohort
	// commit bumped, and which the guest must never build beside the
	// sibling it conflicts with. What the note said the first time is
	// what the retry must honour: a member whose run is withheld stays
	// out, a member whose run carries a forced sibling goes back last
	// with that sibling to deactivate, and everything else keeps its
	// place. On a note with neither — every deferral there was before
	// this — it is the members unchanged.
	members, forced, held := rosterAsRecorded(n, plat, members)
	s := submission{Port: members[0].Port, Release: release, FromSource: run.FromSource, KeepEnv: run.KeepEnv,
		Members: members, Forced: forced, Withheld: held}
	err = e.submit(ctx, &Minted{Repo: repo, Branch: br, Sha: tip, RelPort: members[0].Portdir}, s)
	var vde *VerifyDeferredError
	if errors.As(err, &vde) {
		// Forced rides with FromSource and KeepEnv: the run re-recorded
		// here is the one the walk claimed, and if that is a forced
		// member's, a re-queue that dropped its sibling would seat it in
		// portdir order with nothing deactivated on the attempt after.
		if rerr := e.recordRun(ctx, repo, tip, portName, plat, record.Run{
			State: record.Queued, Detail: vde.Reason, FromSource: run.FromSource, KeepEnv: run.KeepEnv,
			Forced: run.Forced,
		}, ""); rerr != nil {
			fmt.Fprintf(e.Err, "warning: re-recording queued run: %v\n", rerr)
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
