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
	"strings"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
)

// holder is who owns a prefix, written beside its lock: the process, by
// its PID and start time, and the check.
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
// directories: every dockhand process on the Mac agrees on it, whatever
// its user, database, provider, or home, as a prefix is the Mac's.
func (l *Local) locks() string {
	return cmp.Or(l.Locks, filepath.Join(l.temp(), "dockhand-locks"))
}

// own takes the prefix for one check, from what it puts back first to its
// own put-back, by a lock keyed on the prefix's canonical path: its links
// resolved, so /opt/local and a link to it are one prefix. A second check
// waits within its context, saying whose check holds it, whichever user
// ran it. The holder's record is written beside the lock, and removed with
// it.
func (l *Local) own(ctx context.Context, prefix, check string, build buildenv.Build) (release func(), err error) {
	canonical, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		canonical = filepath.Clean(prefix)
	}
	path := filelock.Path(l.locks(), "prefix:"+canonical)
	quick, cancel := context.WithTimeout(ctx, waitSaid)
	file, err := filelock.AcquireAcrossUsers(quick, path)
	cancel()
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		build.Progress(fmt.Sprintf("waiting for %s, held by %s", prefix, held(path)))
		file, err = filelock.AcquireAcrossUsers(ctx, path)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, buildenv.Infrastructure("taking %s for the check: %w", prefix, err)
	}
	// The holder is said beside the lock, in a record of the user's own,
	// since one user can't write a file another made there.
	self, _ := procid.Self()
	record, _ := json.Marshal(holder{PID: os.Getpid(), Start: self.Start, Check: check, Prefix: canonical})
	said := holderPath(path, os.Getuid())
	if out, err := os.OpenFile(said, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644); err == nil {
		_, _ = out.Write(record)
		out.Close()
	}
	return func() {
		os.Remove(said)
		file.Close()
	}, nil
}

// holder is who holds a prefix's lock now, as a wait on it says; empty
// where no running process's record says it holds it.
func (l *Local) holder(prefix string) string {
	canonical, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		canonical = filepath.Clean(prefix)
	}
	if who := held(filelock.Path(l.locks(), "prefix:"+canonical)); !strings.HasPrefix(who, "a check that ended") && who != "another check" {
		return who
	}
	return ""
}

// holderPath is where a user's process that holds a prefix's lock says
// so.
func holderPath(lock string, uid int) string { return fmt.Sprintf("%s-%d.holder", lock, uid) }

// held is who holds a prefix's lock, as its record says; "another check"
// where it says nothing readable, and "a check that ended" where its
// process runs no more, which the lock's release follows.
func held(path string) string {
	records, _ := filepath.Glob(path + "-*.holder")
	said := "another check"
	for _, record := range records {
		data, err := readNoFollow(record)
		var who holder
		if err != nil || json.Unmarshal(data, &who) != nil || who.PID == 0 {
			continue
		}
		if running(who.PID, who.Start) {
			return who.String()
		}
		said = "a check that ended, " + who.String()
	}
	return said
}

// readNoFollow reads a file, not a link another user planted in its place.
func readNoFollow(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
