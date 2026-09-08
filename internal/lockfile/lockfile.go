// Package lockfile is one advisory flock, shared by every dockhand
// mutual exclusion: the per-repo notes lock, the per-user tart
// admission lock, and the two locks the dispatch ruling names — the
// residency lock a scheduler holds for its whole life and the pass lock
// one cycle holds for one pass. The descriptor is the mutex, and the
// lock dies with the process, so a crashed holder cannot wedge the next
// one.
//
// The file's contents are NOT nothing any more. A lock that only
// excluded could answer "somebody holds this" and never "who, and since
// when", and both of the dispatch locks are read by verbs that are not
// their holder: `status` names the resident dispatcher and the moment it
// took the lock, a second `dispatch` exits 0 saying who is already
// there, and every 60-band remedy line is chosen off the same fact. So a
// holder may STAMP the file with who it is, and any process may READ
// that stamp — see Holder, Hold and Probe.
package lockfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrHeld reports a lock another process held for the whole of the
// caller's deadline. It is a sentinel because the holder is not always
// wedged: a peer mid-way through the very work the caller meant to do
// — a pass booting a guest — holds its lock for minutes on purpose, and
// a caller that can tell the expected case from a hung one says
// something more useful than "check for a hung dockhand".
var ErrHeld = errors.New("another dockhand holds the lock")

// Holder is who holds a lock, as the lock file itself records it.
//
// IT IS NOT record.OwnerID, and the duplication is deliberate rather
// than an oversight. This package is a leaf — an flock and a JSON blob
// — held by the notes lock, by the tart admission lock and by the two
// dispatch locks alike, and a leaf that imported the durable record
// vocabulary would put the whole record package underneath every one of
// them. The four fields below are the four an OwnerID carries plus the
// VERB, which a stamp needs and an owner does not: "dispatch (pid 4821)
// will start it" and "a `status` holds the pass lock" are different
// sentences about the same shape, and the reader of a stamp is entitled
// to know which verb it is waiting on. cli maps between the two at the
// one point that resolves either, which is where record.OwnerID.Root is
// canonicalized anyway.
//
// Every field is optional. A lock taken through Acquire carries no
// stamp at all, and a stamp that could not be parsed comes back zero —
// which is why Probe reports residency and the holder SEPARATELY: "a
// dispatcher is resident" is the load-bearing fact, and "who it is" is
// the sentence beside it. A caller that read an empty Holder as "nobody
// is resident" would be reading a torn write as an answer (rule 7).
type Holder struct {
	Root  string    `json:"root,omitempty"`
	Host  string    `json:"host,omitempty"`
	PID   int       `json:"pid,omitempty"`
	Since time.Time `json:"since,omitempty"`
	Verb  string    `json:"verb,omitempty"`
}

// Empty reports a stamp that says nothing — the file was never stamped,
// or its bytes could not be read as one. It is the rule-7 half of Probe:
// a resident lock with an empty holder is "somebody is here and I could
// not learn who", never "nobody".
func (h Holder) Empty() bool {
	return h.Root == "" && h.Host == "" && h.PID == 0 && h.Since.IsZero() && h.Verb == ""
}

// Acquire takes an exclusive lock on path, creating it (and its
// directory) as needed. Non-blocking with retry, so a wedged peer
// surfaces as a named refusal — ErrHeld, wrapped — after the deadline
// rather than a silent hang. The returned unlock must be called.
//
// It leaves the file's contents alone: a lock nobody reads needs no
// stamp, and rewriting one would make every unstamped lock in the tree
// pay for a feature two of them use. Hold is Acquire with a stamp.
func Acquire(ctx context.Context, path string, deadline time.Duration) (func(), error) {
	f, err := lock(ctx, path, deadline)
	if err != nil {
		return nil, err
	}
	return release(f), nil
}

// Hold is Acquire with the holder written into the file, for the two
// locks that are READ by processes that are not their holder: the
// residency lock a dispatcher keeps for its whole life, and the pass
// lock one pass keeps for one pass.
//
// The stamp is written AFTER the lock is taken and never before, so what
// a reader finds under an exclusive hold is the holder's own — a
// stamp written by a process that then failed to take the lock would
// name a holder that is not there. A write that fails is not a failure
// to lock: the exclusion is the descriptor's and stands either way, so
// the error is discarded and the reader meets an empty Holder, which
// Probe reports as "resident, holder unknown".
//
// THE RELEASE ERASES THE STAMP, which is the other half of writing one
// honestly and was missing from the first cut. A stamp that outlived
// its hold is what a contender reads in the window between the next
// holder's flock and the next holder's write — measured as four
// concurrent passes naming a pid that had been dead for minutes — and
// what an Acquire-taker leaves standing over a crashed holder's bytes.
// Erased under the exclusive hold and never after it, so no other
// process can be mid-write when the file is truncated; what a
// concurrent probe sees in that window is an empty stamp, which is the
// honest answer for a lock nobody has stamped yet. A CRASHED holder
// erases nothing and cannot, which is why the stamp is also checked
// against the process table when it is read — see attested.
func Hold(ctx context.Context, path string, h Holder, deadline time.Duration) (func(), error) {
	f, err := lock(ctx, path, deadline)
	if err != nil {
		return nil, err
	}
	stamp(f, h)
	return unstamp(f), nil
}

// Probe reports whether an EXCLUSIVE holder has this lock, and who it
// is, WITHOUT taking it for itself.
//
// THE PROBE IS A SHARED LOCK, and the reason it is not a try-lock is a
// defect an adversarial pass found in the draft that said "try-lock the
// lockfile": a try-lock TAKES the lock, so for the instant a bump, a
// verify, an accept or a status probed, it WAS the resident. A
// concurrent probe read "resident, holder = the other verb's pid" and
// printed "dispatch (pid N) will start it" for a pid that would exit in
// 200ms; a --wait entering that iteration dropped to watching with
// nobody judging; and a `dockhand dispatch` starting at that instant
// failed its own lock and exited 0 believing a scheduler was up, when
// the holder was a `status`. So: a dispatcher holds LOCK_EX for its
// whole life and probers take LOCK_SH|LOCK_NB, which excludes them from
// the exclusive holder and never from each other.
//
// The three answers are the three the caller needs (rule 7):
//
//	(_, false, nil)   the shared lock was taken: nobody holds it exclusively
//	(h, true,  nil)   EWOULDBLOCK: somebody does, and h is the stamp it
//	                  left IF this host can still see the process that
//	                  wrote it; the empty Holder otherwise
//	(_, false, err)   the lock could not be read at all — a permission
//	                  error, a filesystem with no flock — which is NOT
//	                  "no dispatcher", and a caller that read it as one
//	                  would appoint itself judge beside a scheduler it
//	                  could not see
//
// THE STAMP IS CHECKED BEFORE IT IS REPEATED, and attested carries the
// argument: a lock file's bytes are not evidence that their author is
// the holder, and "a pass is already running here: dispatch (pid 99999,
// since 19:00:00)" for a pid nobody can find sends an operator looking
// for a scheduler that does not exist. A stamp this host cannot vouch
// for comes back empty and the residency beside it stays true, which is
// rule 7 on the pair: the flock said somebody is here, and no reading
// of a JSON blob may take that back.
//
// A file that does not exist is the first answer and not the third: a
// lock nobody has ever taken has no holder. The file is NOT created
// here, because a probe that created files would leave one in every
// checkout a `status` was ever run in, and because creating it proves
// nothing about who holds it.
//
// The shared lock is dropped before this function returns, so a prober
// holds nothing.
func Probe(ctx context.Context, path string) (Holder, bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0o644) //nolint:gosec // the path is the composition root's, from $GIT_COMMON_DIR
	if errors.Is(err, os.ErrNotExist) {
		return Holder{}, false, nil
	}
	if err != nil {
		return Holder{}, false, err
	}
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	switch {
	case err == nil:
		// Nobody holds it exclusively. Drop the shared lock at once: a
		// prober that kept one would be the thing the next dispatcher's
		// LOCK_EX waited on.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return Holder{}, false, nil
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
		return attested(ctx, read(f)), true, nil
	default:
		return Holder{}, false, fmt.Errorf("probing %s: %w", path, err)
	}
}

// lock is the retry loop both Acquire and Hold take. Non-blocking with
// a bounded wait, so a peer that releases inside the deadline is waited
// for and a wedged one is named.
func lock(ctx context.Context, path string, deadline time.Duration) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec // the path is the composition root's, from $GIT_COMMON_DIR
	if err != nil {
		return nil, err
	}
	until := time.Now().Add(deadline)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if time.Now().After(until) {
			_ = f.Close()
			// Never advise deleting the file: a crashed holder releases
			// automatically (the lock lives on the descriptor, not the
			// file), and deleting it under a LIVE holder splits the
			// lock across two inodes — two holders, no exclusion.
			return nil, fmt.Errorf("%w past its deadline: %s — check for a running or hung dockhand; a crashed one releases the lock by itself", ErrHeld, path)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// release is the unlock every taker is handed: drop the flock, then
// close. In that order, because closing releases the lock anyway and a
// reader of this code should see the release stated rather than implied.
func release(f *os.File) func() {
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

// unstamp is Hold's release: erase the stamp, THEN drop the flock.
//
// In that order and never the reverse, because between the unlock and
// the truncate the file would be a stamped lock somebody else may
// already hold — the exact confusion the stamp exists to prevent, made
// worse by being written by the process that just left. A truncate that
// fails leaves the old bytes, which attested refuses on the next read;
// there is nothing to report and nobody to report it to, so the error
// goes the way stamp's does.
func unstamp(f *os.File) func() {
	drop := release(f)
	return func() {
		_ = f.Truncate(0)
		drop()
	}
}

// stamp writes the holder over the file's contents.
//
// WriteAt then Truncate, and never Truncate then Write: the reader is a
// concurrent Probe holding no lock, and a truncate-first write leaves a
// window in which the file is legitimately EMPTY. Writing over the old
// bytes first means the window shows a mixture of two stamps, which
// fails to parse and comes back as an empty Holder — "resident, holder
// unknown", which is the honest answer — rather than as a valid stamp
// naming nobody.
func stamp(f *os.File, h Holder) {
	b, err := json.Marshal(h)
	if err != nil {
		return
	}
	b = append(b, '\n')
	if _, err := f.WriteAt(b, 0); err != nil {
		return
	}
	_ = f.Truncate(int64(len(b)))
}

// read parses the stamp, answering with the zero Holder for anything it
// cannot read: an unstamped lock, a torn write, a file some other
// dockhand version wrote. Every one of those is "I could not learn who",
// and Probe reports it beside a residency that is true regardless.
func read(f *os.File) Holder {
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return Holder{}
	}
	var h Holder
	if err := json.Unmarshal(b, &h); err != nil {
		return Holder{}
	}
	return h
}
