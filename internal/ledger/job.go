package ledger

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// The environment half of the record, which schema 3 split off from the
// verdict half. A job is one guest on one platform, keyed by release
// name and shared by every subject in the change; a run is one
// subject's verdict on it, keyed by port and release both. Everything
// that is true of the guest rather than of a port lives on the job —
// the handle, the claim, and above all whether it has been given back.

// RecordSubmission writes one guest and every verdict started on it, in
// one note: the job under its release name, and one run per port under
// RunKey(port, release).
//
// One call and one write, because a job and the runs behind it are one
// fact. Written apart they would be two objects on the notes ref and,
// in between them, a record naming an environment no run is using or a
// run whose platform names no job — and a settlement, a sweep and a
// release check each read one of those as a fault.
//
// The run is asked for per port rather than handed over once. What a
// submission asserts is shared — N subjects go into one environment
// behind one job, all of them running, and they diverge only when the
// log comes back and says which one broke — but not everything a run
// carries is a verdict. Whether a member's binary archive was ignored
// is a property of the argv THAT MEMBER was handed, and one template
// copied across the roster would have the note claim of a dependent
// something the guest was never told to do. Platform is stamped from
// the release, as in RecordRun.
func (l *Ledger) RecordSubmission(ctx context.Context, sha, release string, job record.JobRecord, ports []string, run func(port string) record.Run) error {
	return l.Update(ctx, sha, func(r *record.Record) error {
		r.Jobs[release] = job
		for _, port := range ports {
			adoptSubject(r, port)
			started := run(port)
			started.Platform = release
			r.Runs[record.RunKey(port, release)] = started
		}
		return nil
	})
}

// SameRun states one run for every subject: the caller saying it has
// nothing to tell the members apart by. It is spelled out rather than
// left as an overload, so a submission that DOES differ per member —
// the archive one of them must ignore — cannot be written by accident
// as one that does not.
func SameRun(run record.Run) func(string) record.Run {
	return func(string) record.Run { return run }
}

// ReleaseJob takes the sole right to hand one guest back, and answers
// whether this caller now holds it.
//
// This is the two-map split spelled as a verb. One environment shared
// by N subjects goes back exactly once, and never while a subject is
// still building inside it. Both halves are decided under the notes
// flock over a fresh read, which is the only place they can be decided
// at all: two dockhands that each read "finished" and then each called
// Release would return the same guest twice, and one that released when
// the first of three subjects finished would take the environment out
// from under the other two.
//
// The order it imposes on the caller is the honest one — the flag goes
// down first, and the provider is asked afterwards. A crash in between
// leaks an environment, which the orphan sweep finds and a person can
// delete; releasing first and recording after would hand the same guest
// back twice, which nothing can undo. It follows that a run must be
// written terminal BEFORE the release is asked for, and a caller whose
// own verdict is still unwritten is told no, correctly.
//
// It changes nothing but the flag. The handle stays: it names what is
// being handed back, the handing back has not happened yet, and a note
// that erased the name before the provider was even asked would leave a
// failed release with nothing to point a person at.
//
// false with no error is every ordinary refusal — the note does not
// name the job, somebody already holds the release, or a run on it is
// still live. A caller holding a job the note does not name has an
// orphan, not an authorization.
//
// Whether the guest SHOULD go back is not asked here and must not be.
// A failed run's environment is kept as the debug handle, and that is a
// judgment about what a failure is worth; this package decides only
// that whoever hands it back does so once and not too early.
func (l *Ledger) ReleaseJob(ctx context.Context, sha, release string) (bool, error) {
	took := false
	err := l.Update(ctx, sha, func(r *record.Record) error {
		job, ok := r.Jobs[release]
		if !ok || job.Released || !idle(*r, release) {
			return ErrUnchanged
		}
		job.Released = true
		r.Jobs[release] = job
		took = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return took, nil
}

// idle reports that no run on the release is still using its guest.
//
// A run counts as being on the release when its key says so or when its
// Platform field does. The union is deliberate and the safe side of a
// corruption: the key and the field are two spellings of one fact, a
// note where they disagree has been mangled, and the reading that costs
// least is that the environment is still in use. An unknown state word
// is not terminal either, for the same reason.
//
// A job with no runs at all is idle, which is right: an environment
// nothing is building in is an environment to give back.
func idle(r record.Record, release string) bool {
	suffix := "@" + release
	for key, run := range r.Runs {
		if !strings.HasSuffix(key, suffix) && run.Platform != release {
			continue
		}
		if !run.State.Terminal() {
			return false
		}
	}
	return true
}
