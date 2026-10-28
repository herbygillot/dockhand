package local

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
)

// holder is who owns a prefix, written into its lock: the process, by its
// PID and start time, and the check.
type holder struct {
	PID    int    `json:"pid"`
	Start  string `json:"start"`
	Check  string `json:"check"`
	Prefix string `json:"prefix"`
}

// String is the holder as a wait says it: "check-12 (pid 4711)".
func (h holder) String() string { return fmt.Sprintf("%s (pid %d)", h.Check, h.PID) }

// waitSaid is how long a check waits for its prefix before it says whose
// check it's waiting for.
var waitSaid = 50 * time.Millisecond

// locks is where the prefixes' locks are kept, beside the checks'
// directories: every dockhand process of the person agrees on it, whatever
// its database, provider, or home.
func (l *Local) locks() string {
	return cmp.Or(l.Locks, filepath.Join(l.temp(), fmt.Sprintf("dockhand-locks-%d", os.Getuid())))
}

// own takes the prefix for one check, from what it puts back first to its
// own put-back, by a lock keyed on the prefix's canonical path: its links
// resolved, so /opt/local and a link to it are one prefix. A second check
// waits within its context, saying whose check holds it. The holder is
// written into the lock, which release clears and lets go.
func (l *Local) own(ctx context.Context, prefix, check string, build buildenv.Build) (release func(), err error) {
	canonical, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		canonical = filepath.Clean(prefix)
	}
	path := filelock.Path(l.locks(), "prefix:"+canonical)
	quick, cancel := context.WithTimeout(ctx, waitSaid)
	file, err := filelock.Acquire(quick, path, filelock.Exclusive)
	cancel()
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		build.Progress(fmt.Sprintf("waiting for %s, held by %s", prefix, held(path)))
		file, err = filelock.Acquire(ctx, path, filelock.Exclusive)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, buildenv.Infrastructure("taking %s for the check: %w", prefix, err)
	}
	self, _ := procid.Self()
	record, _ := json.Marshal(holder{PID: os.Getpid(), Start: self.Start, Check: check, Prefix: canonical})
	if err := file.Truncate(0); err == nil {
		_, _ = file.WriteAt(record, 0)
	}
	return func() {
		_ = file.Truncate(0)
		file.Close()
	}, nil
}

// held is who holds a prefix's lock, as its record says; "another check"
// where it says nothing readable, and "a check that ended" where its
// process runs no more, which the lock's release follows.
func held(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "another check"
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	var who holder
	if err != nil || json.Unmarshal(data, &who) != nil || who.PID == 0 {
		return "another check"
	}
	if !running(who.PID, who.Start) {
		return "a check that ended, " + who.String()
	}
	return who.String()
}
