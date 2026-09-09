// Package proc asks this host one question about a process: when it
// started.
//
// It is a LEAF, and that is its whole design constraint. Two packages
// need the same answer for different reasons — lease decides whether a
// pid and a birth name one process, lockfile decides whether the
// process at a pid is old enough to have written a stamp — and until
// now each carried its own copy of the probe, identical but for a word
// of comment.
//
// The copy in lockfile documented itself as deliberate, and its reason
// was sound: lockfile is held by the notes lock, the tart admission
// lock and both dispatch locks, and reaching into lease for an
// unexported helper would put lease's thirteen internal dependencies
// underneath every mutual exclusion in the tree. That argument defeats
// importing lease. It does not defeat sharing the mechanism, because
// this package depends on nothing — the standard library and no more —
// so both callers reach it without either reaching the other.
//
// What stays with the callers is the JUDGMENT. This package answers
// when a process started; what that means about a lease or a lock is
// not its business.
package proc

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Layout is what `ps -o lstart=` prints — "Tue Sep  8 01:51:16 2026",
// with the day of the month space-padded. The format is fixed by ps and
// not by a locale variable dockhand sets.
const Layout = "Mon Jan _2 15:04:05 2006"

// Start is when this host started the process at pid, whether it found
// one at all, and the failure to look. The three are separate because
// they are three different answers, and a caller that collapsed them
// would read "I could not ask" as "there is no such process".
//
// It asks ps, which is the portable question about a process this tree
// can ask without vendoring a syscall package for one field.
//
// The seconds are whole: ps truncates. A caller comparing against a
// recorded time has to allow for that.
func Start(ctx context.Context, pid int) (time.Time, bool, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		// ps exits non-zero for a pid it has no process for, which is the
		// answer and not a failure. Anything else — no ps at all, a
		// context that ended — is a failure to look, and a caller must
		// not read it as an absent process.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	line := strings.TrimSpace(string(out))
	start, perr := time.ParseInLocation(Layout, line, time.Local)
	if perr != nil {
		return time.Time{}, false, perr
	}
	return start, true, nil
}
