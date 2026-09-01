package tart

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/verify"
)

// cacheDir resolves dockhand's per-user machine-state directory — a
// variable so tests can point it somewhere disposable.
var cacheDir = func() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "dockhand"), nil
}

// Admit takes the machine-wide admission lock and counts what is
// actually running. Occupancy is DERIVED, never recorded (the D19
// stance, machine-scoped): `tart list` is the truth, which also
// counts VMs dockhand did not start — a user's own tart VM spends an
// Apple licence slot just the same, and any ledger would have missed
// it. On refusal the typed CapacityError comes back and no lock is
// held; on admission the caller holds the lock through clone and
// start — until its new VM is itself visible as running — so two
// dockhands serialize their starts instead of both counting the same
// free slot.
func Admit(ctx context.Context, capacity int) (func(), error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}
	unlock, err := lockfile.Acquire(ctx, filepath.Join(dir, "tart.lock"), 30*time.Second)
	if err != nil {
		return nil, err
	}
	busy, err := runningVMs(ctx)
	if err != nil {
		unlock()
		return nil, err
	}
	if busy >= capacity {
		unlock()
		return nil, &verify.CapacityError{Busy: busy, Cap: capacity}
	}
	return unlock, nil
}

// runningVMs counts every running VM on the machine, whoever started
// it.
func runningVMs(ctx context.Context) (int, error) {
	out, err := CLI(ctx, nil, "list")
	if err != nil {
		return 0, fmt.Errorf("%w: listing VMs for admission: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return countRunning(out), nil
}

// countRunning parses `tart list` output: one VM per line, state in
// the last column.
func countRunning(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == "running" {
			n++
		}
	}
	return n
}

// WaitRunning holds until the named VM shows as running, the run
// errand reports failure, or the deadline passes. It exists so the
// admission lock is released only once the new VM occupies its slot
// visibly — and so a `tart run` that fails at startup surfaces as its
// own error rather than being discovered through a two-minute agent
// timeout.
func WaitRunning(ctx context.Context, vm string, runErr <-chan error) error {
	deadline := time.After(30 * time.Second)
	for {
		out, err := CLI(ctx, nil, "list")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 3 && fields[1] == vm && fields[len(fields)-1] == "running" {
					return nil
				}
			}
		}
		select {
		case rerr := <-runErr:
			if rerr != nil {
				return fmt.Errorf("%w: tart run %s: %w", verify.ErrNoEnvironment, vm, rerr)
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("%w: %s never reached the running state", verify.ErrNoEnvironment, vm)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
