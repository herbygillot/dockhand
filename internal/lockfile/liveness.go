package lockfile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// attested is the half of a stamp a reader may repeat out loud: the
// Holder when this host can still see the process that wrote it, and
// the empty Holder otherwise.
//
// A STAMP IS NOT EVIDENCE THAT ITS AUTHOR IS THE HOLDER, and treating
// it as one is how a losing caller comes to name a process that is not
// running. Three ways the two come apart, all of them observed:
//
//   - The stamp is written AFTER the flock is taken (see Hold, which
//     explains why it must be), so for the microseconds between the two
//     the file still carries the PREVIOUS holder's bytes. Four
//     concurrent passes are enough to land a contender in that window.
//   - A holder that CRASHES releases its flock and does not erase its
//     stamp. The next taker that stamps overwrites it; a taker that
//     came through Acquire does not, and the file then names a dead
//     process under a live hold.
//   - The bytes are a file in .git and nothing stops a person, a stale
//     backup or another dockhand version from putting anything there.
//
// So the stamp is checked against this host's process table before it
// is believed, and the answer is a HOLDER and never a residency: the
// flock said somebody is here and no reading of a JSON blob may take
// that back. What an unbelievable stamp costs the reader is the name —
// describeHolder's "another dockhand holds the lock" — which is rule 7
// said out loud: "I could not learn who holds it" is not "nobody".
//
// The three refusals below are the three ways a stamp fails to attest:
//
//	a stamp from ANOTHER HOST, which this process table cannot answer
//	  for at all — and which flock over a filesystem shared between
//	  hosts is no authority on either
//	a PID this host has no process for, which is the ordinary crashed
//	  or exited holder
//	a PID whose process STARTED AFTER the stamp was written, which is a
//	  reused number: two live processes never share one, so anything at
//	  that PID that is younger than the stamp is a stranger
//
// The last is why Since is load-bearing rather than decorative, and why
// the comparison is one-sided. lease's own liveness check pairs a PID
// with a birth and asks whether they name ONE process; this cannot,
// because Holder.Since is a PASS start under a resident dispatcher
// (cli's onePass restamps OwnerID.Since per pass) and a pass start
// drifts hours past the birth of the process that owns it. What holds
// for both stamps is the weaker and still sufficient fact that a
// process cannot have been born after it wrote something.
func attested(ctx context.Context, h Holder) Holder {
	if !sameHost(h.Host) || h.PID <= 0 || h.Since.IsZero() {
		return Holder{}
	}
	start, found, err := processStart(ctx, h.PID)
	switch {
	case err != nil, !found, start.After(h.Since.Add(clockSlack)):
		return Holder{}
	}
	return h
}

// clockSlack is how far the process table's clock may read AHEAD of the
// stamp's own moment before the pair is judged two different processes.
//
// The two readings come from one machine — the kernel's fork time and
// dockhand's time.Now() — so the gap is normally negative by whatever
// the stamp was written after the birth. It goes the other way only
// when the wall clock stepped backwards between them, which is an NTP
// correction, and the slack is small because a large one would start
// admitting genuine strangers: a reused PID is a process that started
// after the stamp, and the whole check is that comparison.
const clockSlack = 2 * time.Second

// sameHost reports that a stamp names the machine reading it. An empty
// Host is not this host: a stamp written by a process whose
// os.Hostname() failed says nothing about where it ran, and nothing is
// not agreement.
func sameHost(host string) bool {
	if host == "" {
		return false
	}
	me, err := os.Hostname()
	if err != nil {
		return false
	}
	return host == me
}

// processStart is when this host started the process at pid, whether it
// found one at all, and the failure to look.
//
// A variable so a test can script the process table, which is the only
// way to write a case for "a PID that is present with a later start
// time" — the reused number this check exists for — without waiting for
// a machine to churn through 99999 of them.
var processStart = psStart

// psStart asks ps, which is the portable question about a process a leaf
// package can ask without vendoring a syscall package for one field.
//
// IT IS DELIBERATELY A SECOND COPY of the same question lease asks, and
// the duplication is the price of the layering rather than an oversight.
// lockfile is a leaf — an flock and a JSON blob — held by the notes
// lock, the tart admission lock and the two dispatch locks alike, and
// importing lease (which imports record, statestore and the rest of the
// durable vocabulary) to reach an unexported helper would put that whole
// graph underneath every mutual exclusion in the tree. The two also ask
// DIFFERENT questions of the same output: lease asks whether a PID and a
// birth name one process, and this asks only whether the process at a
// PID is old enough to have written a stamp — see attested.
//
// `ps -o lstart= -p N` prints one line in the C locale's own format and
// exits non-zero when there is no such process, which is the two facts
// this needs in one call. A line that does not parse is an answer this
// cannot use, and an unusable answer is a failure to look rather than a
// guess in either direction.
func psStart(ctx context.Context, pid int) (time.Time, bool, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		// ps exits non-zero for a pid it has no process for, which is the
		// answer and not a failure. Anything else — no ps at all, a
		// context that ended — is a failure to look, and attested must
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
// with the day of the month space-padded.
const psLayout = "Mon Jan _2 15:04:05 2006"
