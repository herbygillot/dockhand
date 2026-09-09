package lease

import (
	"context"
	"github.com/herbygillot/dockhand/internal/proc"
	"time"
)

// liveness is what this host's process table says about one (PID,
// start-time) pair, and it has three values because the third one is
// the whole reason the check exists. A bool would mean both "that
// process is gone" and "I could not ask", and the act on the other side
// of "gone" destroys a virtual machine (rule 7).
type liveness uint8

const (
	// unknownLiveness is the refusing zero: this host could not answer.
	// Standing reads it as LiveElsewhere — reported, never seized.
	unknownLiveness liveness = iota
	running
	gone
)

// startupWindow and clockSlack are what "the same process" means when
// the two clocks that timed its birth are not the same clock.
//
// record.OwnerID.Since is what dockhand's own time.Now() read shortly
// after the process started; the process table's start time is what the
// kernel recorded when it forked. The first is therefore always at or
// after the second, and MEASURED on this machine the gap is the Go
// runtime's startup plus ps's truncation to whole seconds: 291 ms and
// 479 ms on two consecutive runs. A minute is comfortably above that
// even for a process that started while the machine was thrashing, and
// comfortably below any plausible PID reuse — macOS rolls PIDs over at
// 99999, so a number comes back only after the machine has churned tens
// of thousands of processes, which does not happen inside a minute at a
// rate that would also land within it.
//
// The slack on the other side is for a clock that stepped backwards
// between the two readings — an NTP correction — and it is small
// because a large one would start admitting genuine strangers.
//
// Both errors are protected against in the safe direction: a pair
// judged NOT the same process reads as DeadElsewhere and is seizable,
// so the window is generous on purpose, and a stranger wrongly judged
// alive merely leaves an obligation standing for another pass.
const (
	startupWindow = time.Minute
	clockSlack    = 2 * time.Second
)

// processIs reports what this host says about the process a lease names.
//
// A PID ALONE IS NOT AN IDENTITY, which is why record.OwnerID carries
// Since. PIDs are reused, so a same-host recovery that checks only "is
// something alive at 4821" eventually finds an unrelated process and
// refuses the reclaim forever — or, once the number comes round to a
// process that is genuinely alive, refuses it for good. The pair is what
// identifies a process; the number alone is a coincidence waiting to
// happen.
//
// A pid at or below zero is not a process this can ask about: a record
// written without one says nothing, and nothing is not death.
func processIs(ctx context.Context, pid int, since time.Time) liveness {
	if pid <= 0 {
		return unknownLiveness
	}
	start, found, err := processStart(ctx, pid)
	switch {
	case err != nil:
		return unknownLiveness
	case !found:
		return gone
	case sameProcess(since, start):
		return running
	default:
		// The number is here and the process behind it is not the one the
		// record named: a reused PID, which is exactly the case the pair
		// exists to catch.
		return gone
	}
}

// sameProcess reports whether a recorded Since and a kernel start time
// name one birth. See startupWindow for the measurement.
func sameProcess(since, start time.Time) bool {
	gap := since.Sub(start)
	return gap >= -clockSlack && gap <= startupWindow
}

// processStart is when this host started the process at pid, whether it
// found one at all, and the failure to look.
//
// A variable so that Standing is provable without a second process to
// point it at: a test scripts the process table, which is the only way
// to write a case for "a PID that is present with a different start
// time" at all.
// The probe itself is internal/proc: the same question lockfile asks,
// for a different reason. What stays here is the JUDGMENT — whether a
// pid and a birth name one process — which is lease's and not a leaf's.
var processStart = proc.Start
