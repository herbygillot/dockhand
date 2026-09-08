package lease

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
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
var processStart = psStart

// psStart asks ps, which is the portable question about a process this
// tree can ask without vendoring a syscall package for one field.
//
// `ps -o lstart= -p N` prints one line in the C locale's own format and
// exits non-zero when there is no such process, which is the two facts
// this needs in one call. The format is fixed by ps and not by a locale
// variable dockhand sets, so the layout is a constant here; a line that
// does not parse is an answer this cannot use, and an unusable answer is
// unknownLiveness rather than a guess in either direction.
//
// The seconds are whole: ps truncates. See startupWindow, which is sized
// for that and for the runtime's own startup.
func psStart(ctx context.Context, pid int) (time.Time, bool, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		// ps exits non-zero for a pid it has no process for, which is the
		// answer and not a failure. Anything else — no ps at all, a
		// context that ended — is a failure to look, and the caller must
		// not read it as an absent process.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	line := strings.TrimSpace(string(out))
	start, perr := time.ParseInLocation(psLayout, line, time.Local)
	if perr != nil {
		return time.Time{}, false, perr
	}
	return start, true, nil
}

// psLayout is what `ps -o lstart=` prints — "Tue Sep  8 01:51:16 2026",
// with the day of the month space-padded — measured on this machine
// rather than assumed.
const psLayout = "Mon Jan _2 15:04:05 2006"
