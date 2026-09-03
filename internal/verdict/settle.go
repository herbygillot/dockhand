package verdict

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ReleaseAction is what a judgment says should happen to the worker the
// run was using. A settle is the only place dockhand hands a VM slot
// back, and with a two-guest cap the wrong answer is expensive in both
// directions: a kept green environment wastes a slot nobody will enter,
// and a released failed one destroys the debug handle that was the whole
// point of keeping it.
type ReleaseAction int

const (
	// KeepWorker leaves the environment standing. A failure the log
	// blames on the port under test is the branch's own breakage, and the
	// worker is the thing to go and look at.
	KeepWorker ReleaseAction = iota
	// ReleaseAndReport releases the worker and records a failure to do
	// so in the run's detail. This is the passing run's release: the
	// verdict is already known, so a stuck worker is not a mystery, but
	// it is a slot the next build will not have and the note should say
	// where it went.
	ReleaseAndReport
	// ReleaseQuietly releases the worker and lets a failure to do so
	// pass unrecorded. This is the compensating release — a refusal, a
	// blocked run, an errored environment — where nothing about the
	// verdict depends on the answer. Nothing waits on it either, so the
	// caller runs it on a context that survives its own cancellation.
	ReleaseQuietly
)

// RunInput is everything one running run's settlement turns on, read by
// the caller and handed over as values. There is no provider here, no
// repository and no context: a settlement is a decision about facts, and
// this is the whole fact set.
type RunInput struct {
	// Run is the run as the note currently holds it. The judgment
	// modifies a copy, so the fields it does not touch — the lint
	// record, the from-source flag, the manifests — come through
	// unchanged.
	//
	// What is NOT here is anything about the environment. A guest is
	// shared by every subject in the change, so its handle, its claim
	// and whether it has been given back belong to the job, and one
	// subject's judgment must not be able to write any of them: nine
	// verdicts reached in one guest would otherwise each name it, and
	// the eighth to be written would be the one that stood.
	Run record.Run
	// Port is the port the NOTE names, which for a subport is its own
	// name and never the portdir's base. The blame reader compares
	// against it, so the portdir's base name here would blame the
	// subport's parent for its own failure.
	Port string
	// Vanished says the poll answered verify.ErrUnknownJob: the job is
	// one the provider no longer recognizes, which is what a worker
	// deleted out from under a note looks like.
	Vanished bool
	// Status is what the poll said, when it said anything.
	Status verify.Status
	// Log is the guest log, and LogRead says it was actually readable.
	// The two are separate because an unreadable log is a real settle
	// outcome — the run still settles, with no diagnosis to quote — and
	// an empty string cannot say which of the two happened.
	//
	// It is one subject's log, and the cutting is the caller's: a
	// judgment about a port must see that port's output and nothing
	// else, or a neighbour's failure is read as this one's. Today every
	// log has one subject and arrives whole. When a cohort runner
	// writes several ports into one file, the caller passes
	// verify.SubjectLog(log, port) — the accessor, not the map under
	// it, whose implicit subject is keyed by the empty string.
	Log     string
	LogRead bool
	// Nomaintainer says the dependency BlamedDependency named has no
	// maintainer. It is only consulted on a blocked run, and a caller
	// that has not looked passes false, which reads as "say nothing".
	Nomaintainer bool
}

// Judgment is what a poll made of a running run.
type Judgment struct {
	// Settled says the poll moved the run. A job still building settles
	// nothing, and the caller must not write the run back on its
	// account: an unchanged run written anyway is a git note object
	// created for no reason, visible in every status golden that runs
	// over a live build.
	Settled bool
	// Run is the run as it now stands. It is the input run when nothing
	// settled, so a caller that writes it back regardless writes the same
	// bytes rather than a zero value.
	Run record.Run
	// Release is what to do with the worker. It is advice about the
	// guest and not an act on it: one guest holds every subject in the
	// change, so the caller collects these answers across the runs that
	// shared it and hands it back once, or keeps it because one of them
	// asked to.
	Release ReleaseAction
}

// NeedsLog reports whether settling this run requires its guest log,
// which is a question the caller must ask before judging because
// fetching one is a round trip to the guest.
//
// A failure always needs its log: the diagnosis, the refusal and the
// blame all come out of it. A pass needs one only when the run led with
// lint, because the only thing read from a passing log is what lint
// said, and a run that never linted has nothing to corroborate. Nothing
// else needs one — and a passing run's log must be read BEFORE its
// worker is released, because releasing it puts the log out of reach.
func NeedsLog(state verify.State, linted bool) bool {
	switch state {
	case verify.Passed:
		return linted
	case verify.Failed:
		return true
	case verify.Running, verify.Errored:
		return false
	}
	return false
}

// JudgeRun settles one running run against what the poll and the log
// said. It is the whole of the settle's decision: which state the run
// takes, what detail it carries, whether the environment survives.
//
// The refusal and blame readings both downgrade a failure, and both
// release the worker, because neither is a finding about the change: a
// port declining a platform is often the change working, and a
// dependency breaking before the change was reached leaves it untested
// rather than disproven. What the port itself broke on stays failed and
// keeps its environment.
//
// Keeping it is said with KeepWorker and never by writing a name onto
// the run. The environment's name is the guest's, the poll is what
// knows it, and the caller stamps it on the job once for however many
// subjects failed inside it.
func JudgeRun(in RunInput) Judgment {
	r := in.Run
	if in.Vanished {
		// No release: the worker is already gone, which is the only
		// reason the provider cannot find the job.
		r.State, r.Detail = record.Errored, "job vanished: its worker no longer exists"
		return Judgment{Settled: true, Run: r, Release: KeepWorker}
	}
	release := KeepWorker
	switch in.Status.State {
	case verify.Running:
		return Judgment{Run: in.Run, Release: KeepWorker}
	case verify.Passed:
		r.State = record.Passed
		if r.Linted && in.LogRead {
			// The log is about to become unreachable — a passing run's
			// worker is released — so what lint said is read now or
			// never. This is the lint box's corroboration.
			r.Lint = LintSummary(in.Log)
		}
		release = ReleaseAndReport
	case verify.Failed:
		r.State = record.Failed
		if in.LogRead {
			if PortDeclined(in.Log) {
				r.State = record.Unsupported
				r.Detail = "the port declines to build on this platform"
				release = ReleaseQuietly
			} else {
				// The diagnosis rides the note, so status answers "why"
				// without a log dig — the failure-side twin of the lint
				// evidence.
				r.Detail = FailureSummary(in.Log)
				// A failure that names a DIFFERENT port is a dependency
				// breaking before the change was ever reached: the branch
				// is untested, not disproven. blocked, not failed — and
				// the worker is released, because the breakage belongs to
				// a port this branch never touched (field-measured on
				// gomuks, whose verdict blamed the bump for olm).
				if dep, ok := DependencyFailure(r.Detail, in.Port); ok {
					r.State = record.Blocked
					// Blamed names a MEMBER of the change, and this is a
					// port outside it, so a blame an earlier settlement
					// wrote is cleared rather than left standing beside a
					// detail that contradicts it.
					r.Blamed = ""
					r.Detail = BlockedDetail(dep, in.Nomaintainer)
					release = ReleaseQuietly
				}
			}
		}
	case verify.Errored:
		r.State, r.Detail = record.Errored, in.Status.Detail
		release = ReleaseQuietly
	}
	return Judgment{Settled: true, Run: r, Release: release}
}

// There is no AfterRelease here any more. Folding a failed release back
// into the run was a method on the judgment while a run owned its own
// worker; a guest is now shared, so the release happens once for the
// whole platform, after every verdict in it is written, and what a
// refusal to release means is news the settle learns after the judgment
// is over. The caller records it on the runs that were using the guest.
