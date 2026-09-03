package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// inspect observes one branch: the tip, its settled note (nil when
// unnoted), and the drift finding that stands in for a note when there
// is none.
//
// This is the reconciler's whole reading of a branch, and it is
// deliberately the only one: both renderings draw on the same three
// values, so there is no way for the human report and the machine one
// to disagree about what a branch is doing. The wording is render's,
// and the clock a running run's elapsed time is measured against is
// read by the pass rather than in here, so a golden can pin the
// sentence.
func (e *Engine) inspect(ctx context.Context, repo *git.Repo, branch string) (string, *record.Record, string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", nil, "", err
	}
	n, err := e.Ledger(repo).Read(ctx, tip)
	if errors.Is(err, git.ErrNoNote) {
		drift, derr := e.describeUnverifiedTip(ctx, repo, branch, tip)
		if derr != nil {
			return tip, nil, "", derr
		}
		return tip, nil, drift, nil
	}
	if err != nil {
		return tip, nil, "", err
	}
	if n.AnyState(record.Running) {
		if err := e.settle(ctx, repo, &n); err != nil {
			return tip, nil, "", err
		}
	}
	return tip, &n, "", nil
}

// settle polls every running run and writes what it learns back to the
// note. Poll never mutates and the release is the caller's: status
// hands a guest back once every run in it has passed — a kept green
// environment is a wasted slot — and keeps it when one failed, where it
// is the debug handle. A failure whose log shows the port refusing the
// platform records as unsupported instead, and its guest goes back: a
// correct refusal leaves nothing to debug.
//
// The polling happens outside the notes lock and only the writing
// inside it. That is the split a reconciler needs: a poll is a round
// trip per run and a log fetch is another, and holding the flock across
// them stalls every peer sharing the checkout for as long as the
// slowest provider takes. What it costs is that the note can move while
// this pass is asking, and the compare below is what pays for it — a
// judgment is written only if the run it was reached from is still the
// run on the note, state and job both. Two agents share a checkout now,
// and a cancel that lands mid-poll must not come back as passed.
//
// The verdicts are written BEFORE any guest is handed back, which is
// the opposite of the order schema 2 used and the only order the split
// allows. One environment is shared by every subject in the change, so
// the right to give it back is taken once, under the flock, over a
// record that already says the runs are over — the ledger's ReleaseJob
// is that right. Released first and recorded after, two dockhands each
// reading "finished" would return the same guest twice, which nothing
// can undo; recorded first, a crash in between leaks an environment,
// which the orphan audit finds and a person deletes.
func (e *Engine) settle(ctx context.Context, repo *git.Repo, n *record.Record) error {
	prov, err := e.Verifier(ctx)
	if err != nil {
		return nil // running, cannot poll; the note stands as is
	}
	caps := prov.Capabilities()
	// judged is what this pass concluded, keyed as the runs are, and
	// seen is what each conclusion was reached from — the two halves of
	// the compare.
	judged, seen := map[string]record.Run{}, map[string]observed{}
	// What the guests should do, decided per release because a guest is
	// per release: keep it when any subject in it wants it kept, and say
	// so when a release that was expected to go back does not.
	keep, report := map[string]bool{}, map[string]bool{}
	handles := map[string]heldEnv{}
	for _, ref := range runRefs(*n) {
		if ref.Run.State != record.Running {
			continue
		}
		job, ok := n.Jobs[ref.Release]
		if !ok {
			// A running run whose platform names no job. The note has been
			// mangled — a submission writes both in one write — and there
			// is nothing here to poll.
			continue
		}
		in := verdict.RunInput{Run: ref.Run, Port: ref.Port}
		st, perr := prov.Poll(ctx, job.Job)
		var dep string
		switch {
		case errors.Is(perr, verify.ErrUnknownJob):
			// The job is gone, and so is the worker: nothing to read and
			// nothing to release.
			in.Vanished = true
		case perr != nil:
			// A provider that cannot answer settles nothing at all: the
			// runs judged before this one are left unwritten too, because
			// a half-settled note is a worse account than an unsettled
			// one.
			return perr
		default:
			in.Status = st
			// The log is fetched before the release, because releasing a
			// worker puts its log out of reach — and only when the
			// judgment will actually read one.
			if verdict.NeedsLog(st.State, ref.Run.Linted) {
				if log, lerr := prov.Log(ctx, job.Job); lerr == nil {
					in.Log, in.LogRead = log, true
				}
			}
			// Whether a blamed dependency has a maintainer is a fact
			// about the tree, which a judgment cannot go and read. The
			// guarded reader answers whether it is even worth looking,
			// so a port that merely declined the platform sends nobody
			// globbing.
			if st.State == verify.Failed && in.LogRead {
				if d, blamed := verdict.BlamedDependency(in.Log, ref.Port); blamed {
					dep = d
					in.Nomaintainer = nomaintainerDep(repo.Root, d)
				}
			}
		}
		j := verdict.JudgeRun(in)
		if !j.Settled {
			continue
		}
		run := j.Run
		run.Platform = ref.Release
		if run.State == record.Passed {
			// What the pass proves, in the provider's own words, stamped
			// as the run settles rather than looked up when it is
			// rendered: the claim belongs to the environment that was
			// actually used, and providers get reconfigured.
			run.Evidence = caps.Evidence
		}
		// A run blocked by a port that is itself a member of this change
		// inherited its neighbour's failure, and the note says whose.
		// Nothing reaches this at one subject, where a dependency cannot
		// also be a sibling; the day a cohort lands, it is the difference
		// between "untested" and "untested because of libwidget".
		if run.State == record.Blocked && dep != "" && names(*n, dep) {
			run.Blamed = dep
		}
		switch j.Release {
		case verdict.KeepWorker:
			keep[ref.Release] = true
			// A failure keeps its environment, and the name of it belongs
			// to the guest rather than to the verdict: one guest holds one
			// environment however many subjects failed in it.
			if st.Handle != "" {
				handles[ref.Release] = heldEnv{JobID: job.Job.ID, Handle: st.Handle}
			}
		case verdict.ReleaseAndReport:
			report[ref.Release] = true
		case verdict.ReleaseQuietly:
		}
		judged[ref.Key()] = run
		seen[ref.Key()] = observed{Run: ref.Run, JobID: job.Job.ID}
	}
	if len(judged) == 0 {
		return nil
	}
	// The write is the ledger's own read-modify-write, which re-reads
	// under the flock. A run whose state moved since it was observed was
	// settled, canceled or superseded by somebody who saw a note this
	// pass did not, so this pass's word about it is dropped rather than
	// merged; a pass that lands none of its judgments writes nothing at
	// all, which is what keeps a poll from adding a notes object.
	//
	// The compare is on the run's identity and not on its state word
	// alone, because the word alone cannot see a run that came back. A
	// peer that cancels the run and starts another leaves the platform
	// reading running again, and a compare that only asked for "running"
	// would write this pass's verdict about the canceled job over the
	// live one: a verdict the user never asked for, and a worker no note
	// names any more.
	var settled *record.Record
	if err := e.Ledger(repo).Update(ctx, n.Sha, func(fresh *record.Record) error {
		applied := false
		for key, run := range judged {
			was, saw := fresh.Runs[key], seen[key]
			if was.State != saw.Run.State || fresh.Jobs[was.Platform].Job.ID != saw.JobID {
				continue
			}
			fresh.Runs[key] = run
			applied = true
		}
		for rel, held := range handles {
			// The same compare the runs get, for the same reason: a
			// release whose guest is a different one from the guest this
			// pass watched must not inherit that guest's environment name.
			job, ok := fresh.Jobs[rel]
			if !ok || job.Job.ID != held.JobID || job.Handle == held.Handle {
				continue
			}
			job.Handle = held.Handle
			fresh.Jobs[rel] = job
			applied = true
		}
		// The caller's copy becomes what the note says, so that a dropped
		// judgment leaves the reader looking at the peer's record rather
		// than at the poll this pass threw away. The exception is a note
		// that is no longer there: a peer's discard removes it mid-pass,
		// LoadOrStart mints a record for the commit, and handing that back
		// would describe a deleted branch as noted with no runs. Every run
		// judged here was running when it was observed, so a record that
		// knows none of them is not the note being settled.
		if knowsAny(*fresh, judged) {
			cur := *fresh
			settled = &cur
		}
		if !applied {
			return ledger.ErrUnchanged
		}
		return nil
	}); err != nil {
		return err
	}
	if settled != nil {
		*n = *settled
	}
	e.returnGuests(ctx, repo, n, prov, judged, keep, report)
	return nil
}

// observed is one run as this pass found it, with the job it was
// living in — the pair the compare before the write is made against.
// The job's id is carried apart from the run because it is no longer
// on it: a peer that cancels a run and starts another leaves the same
// state word behind a different guest.
type observed struct {
	Run   record.Run
	JobID string
}

// heldEnv is an environment a failure kept: the name of it, and the job
// it was named for.
type heldEnv struct{ JobID, Handle string }

// returnGuests hands back every environment this pass finished with.
//
// The right to hand one back is the ledger's to grant, over a fresh
// record under the flock: it is refused while a run in that guest is
// still live, and granted to exactly one caller. What this function
// decides is only whether the guest SHOULD go back — a failure keeps
// its environment as the debug handle — which is a judgment about what
// a failure is worth and therefore not the ledger's to make.
func (e *Engine) returnGuests(ctx context.Context, repo *git.Repo, n *record.Record, prov verify.Verifier, judged map[string]record.Run, keep, report map[string]bool) {
	for _, rel := range releasesIn(*n) {
		if keep[rel] || !judgedOn(judged, rel) {
			continue
		}
		took, err := e.Ledger(repo).ReleaseJob(ctx, n.Sha, rel)
		if err != nil || !took {
			// Refused is the ordinary answer, not a fault: a peer took
			// the release, or a run in that guest is still building.
			continue
		}
		job := n.Jobs[rel]
		job.Released = true
		n.Jobs[rel] = job
		var rerr error
		if report[rel] {
			rerr = prov.Release(ctx, job.Job)
		} else {
			// Nothing waits on this one, so it runs on a context that
			// survives our own cancellation and its answer goes nowhere.
			_ = prov.Release(context.WithoutCancel(ctx), job.Job)
		}
		if rerr != nil {
			e.noteUnreleased(ctx, repo, n, rel, rerr)
		}
	}
}

// noteUnreleased records a guest that would not go back, on the runs
// that were using it.
//
// A worker the provider refuses to free is not a verdict about the
// port, so it changes nothing but the detail — and it is said on the
// note rather than only on a terminal, because the slot is still gone
// tomorrow when somebody reads the record and wonders where it went.
//
// It is a second write and not part of the verdict's own, because it
// is news that only exists after the guest was asked and said no, and
// the guest cannot be asked until the verdicts are down. It happens
// only when a release the caller expected to succeed did not, which is
// rare enough that the extra notes object is the cheaper half of the
// trade.
func (e *Engine) noteUnreleased(ctx context.Context, repo *git.Repo, n *record.Record, release string, cause error) {
	detail := "worker not released: " + cause.Error()
	if err := e.Ledger(repo).Update(ctx, n.Sha, func(r *record.Record) error {
		changed := false
		for _, ref := range runsOn(*r, release) {
			run := ref.Run
			run.Detail = detail
			r.Runs[ref.Key()] = run
			changed = true
		}
		if !changed {
			return ledger.ErrUnchanged
		}
		return nil
	}); err != nil {
		fmt.Fprintf(e.Err, "warning: recording an unreleased worker on %s: %v\n", release, err)
		return
	}
	for _, ref := range runsOn(*n, release) {
		run := ref.Run
		run.Detail = detail
		n.Runs[ref.Key()] = run
	}
}

// judgedOn reports whether this pass concluded anything about a
// release. A guest nothing was judged on is somebody else's to hand
// back — the pass that judges it.
func judgedOn(judged map[string]record.Run, release string) bool {
	for _, run := range judged {
		if run.Platform == release {
			return true
		}
	}
	return false
}

// names reports whether a port is one of the record's own subjects.
func names(n record.Record, port string) bool {
	for _, s := range n.Subjects {
		if s.Port == port {
			return true
		}
	}
	return false
}

// knowsAny reports whether a record carries any of the runs judged —
// the test of whether it is the note those judgments were made about at
// all.
func knowsAny(n record.Record, judged map[string]record.Run) bool {
	for key := range judged {
		if _, ok := n.Runs[key]; ok {
			return true
		}
	}
	return false
}

// nomaintainerDep reports whether a blamed dependency's Portfile says
// nomaintainer — the one tree read a settlement makes, kept out of the
// judgment that uses it. The glob covers one category level and wants
// exactly one match: two categories carrying the same port name name
// nobody in particular. A port that cannot be found is simply not
// annotated, which reads the same as a maintained one, and both mean
// say nothing.
func nomaintainerDep(treeRoot, dep string) bool {
	matches, _ := filepath.Glob(filepath.Join(treeRoot, "*", dep, "Portfile"))
	if len(matches) != 1 {
		return false
	}
	b, err := os.ReadFile(matches[0])
	return err == nil && bytes.Contains(b, []byte("nomaintainer"))
}

// describeUnverifiedTip says what an unnoted tip means. The finding is
// verdict's; the reading is this function's — the records the notes ref
// holds, and the records on the branch's own history with their
// distance from the tip. Both sequences keep git's order, because the
// judgment names the first match in each and sorting them would
// quietly change which record it names. An unreadable note is stepped
// over here rather than reported: a drift sentence is a courtesy, and
// one bad note in the ref must not cost the whole line.
//
// The records are yielded one at a time, and the branch's history is
// not walked at all unless the notes answered nothing. Every element is
// a `git notes show`, so a tip whose content some record already covers
// — the amend this function mostly exists for — costs the reads up to
// that record and no rev-list.
func (e *Engine) describeUnverifiedTip(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	tipTree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return "", err
	}
	l := e.Ledger(repo)
	shas, err := l.All(ctx)
	if err != nil {
		return "", err
	}
	noted := func(yield func(verdict.Noted) bool) {
		for _, sha := range shas {
			n, err := l.Read(ctx, sha)
			if err != nil {
				continue
			}
			if !yield(verdict.Noted{Sha: git.Abbrev(sha), Record: n}) {
				return
			}
		}
	}
	if s := verdict.DriftOverTree(tipTree, noted); s != "" {
		return s, nil
	}
	ancestry, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return "", err
	}
	behind := func(yield func(verdict.Ancestor) bool) {
		for distance, sha := range ancestry {
			if distance == 0 {
				continue // the tip itself, which is the commit with no note
			}
			n, err := l.Read(ctx, sha)
			if err != nil {
				continue
			}
			if !yield(verdict.Ancestor{
				Noted:  verdict.Noted{Sha: git.Abbrev(sha), Record: n},
				Behind: distance,
			}) {
				return
			}
		}
	}
	return verdict.DriftBehind(branch, behind), nil
}

// LatestNote is the branch's most recent verification record: the
// tip's note, or the nearest one behind it.
func (e *Engine) LatestNote(ctx context.Context, repo *git.Repo, branch string) (record.Record, error) {
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return record.Record{}, err
	}
	for _, sha := range shas {
		if n, err := e.Ledger(repo).Read(ctx, sha); err == nil {
			return n, nil
		}
	}
	return record.Record{}, git.ErrNoNote
}
