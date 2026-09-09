package tart

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/verify"
)

// Stop ends the build inside a guest and LEAVES THE GUEST RUNNING. It is
// verify.Stopper for this provider, and it exists because Release is the
// only other way to end a build and Release takes the environment with
// it — the right answer for a cancel, the wrong one for a --timeout,
// where the person stopped waiting for work they still wanted.
//
// It is idempotent by construction: a guest with no runner pid recorded
// has no build to stop, and that is success rather than a complaint.
func (p Provider) Stop(ctx context.Context, job verify.Job) error {
	if job.Provider != "tart" {
		return fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	if ok, err := HasVM(ctx, p.Tools, job.ID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: no environment named %s", verify.ErrUnknownJob, job.ID)
	}
	// A stopped guest is not running a build, so there is nothing to
	// signal and nothing to report: the work this exists to end has
	// already ended. Booting one to kill a process that is not there
	// would be the act destroying the evidence again.
	if running, err := Running(ctx, p.Tools, job.ID); err != nil {
		return err
	} else if !running {
		return nil
	}
	if _, err := Exec(ctx, p.Tools, job.ID, "/bin/sh", "-c", stopScript(stateDir)); err != nil {
		return fmt.Errorf("stopping the build in %s: %w", job.ID, err)
	}
	return nil
}

// stopScript is what the guest runs to end its own build.
//
// IT KILLS BY PARENTAGE AND NEVER BY PATTERN. `pkill -f dockhand` is how
// this would be written by somebody who had not yet watched a pattern
// match the waiter that was looking for it — the runner's pid is written
// by the runner itself, and every process to end is a descendant of it,
// so parentage answers exactly and a pattern answers approximately.
//
// THE SIGNAL IS SENT WITH sudo FIRST because the processes that matter
// are root's: the runner shell belongs to the guest user and every `port`
// under it was started with `sudo -n`, so a plain kill would reap the
// shell and leave a root compiler running in a guest dockhand had just
// declared stopped. The fallback is the unprivileged kill, for the shell
// itself and for a guest whose sudo has gone away.
//
// TERM, THEN A WAIT, THEN KILL, because verify.Stopper's contract is
// that returning nil means the work is STOPPED and not that a signal was
// sent. Two seconds is long enough for a shell and a `port` to take a
// TERM and short enough that a person watching a timeout does not wonder
// whether it worked. A survivor of both signals exits non-zero, which
// reaches the record as "could not be stopped and may still be running"
// rather than as a false report of a quiet death.
func stopScript(dir string) string {
	return `set -u
[ -f ` + dir + `/pid ] || exit 0
p=$(cat ` + dir + `/pid 2>/dev/null) || exit 0
[ -n "$p" ] || exit 0
k() {
  for c in $(/usr/bin/pgrep -P "$2" 2>/dev/null); do k "$1" "$c"; done
  sudo -n kill -"$1" "$2" 2>/dev/null || kill -"$1" "$2" 2>/dev/null || true
}
k TERM "$p"
i=0
while [ "$i" -lt 20 ] && kill -0 "$p" 2>/dev/null; do sleep 0.1; i=$((i+1)); done
if kill -0 "$p" 2>/dev/null; then k KILL "$p"; sleep 0.2; fi
if kill -0 "$p" 2>/dev/null; then exit 1; fi
exit 0
`
}
