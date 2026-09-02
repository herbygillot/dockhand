package ledger

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// ErrUnchanged, returned by an Update closure, abandons the write and
// leaves the note exactly as it stands. "Nothing moved" is a real
// outcome of a read-modify-write — a settle that polls three running
// jobs and finds all three still running has learned nothing to say —
// and writing the unchanged record anyway would add a notes object per
// poll, which is both noise in the ref and a difference a reader can
// see.
var ErrUnchanged = errors.New("ledger: record unchanged")

// LoadOrStart reads the commit's record, or begins one carrying the
// commit's identity: the start of every per-platform update.
//
// Only true absence starts fresh. A malformed note, a schema from the
// future, a git failure — each propagates, because treating any of
// them as absence would overwrite the very state that governs worker
// release and promotion.
func (l *Ledger) LoadOrStart(ctx context.Context, sha, port string) (record.Record, error) {
	r, err := l.Read(ctx, sha)
	if err == nil {
		if r.Runs == nil {
			// Belt to the codec's braces: its fast path only returns a
			// record whose runs are non-nil, and every caller assigns a
			// run into the map next, where a nil one panics instead of
			// erroring.
			r.Runs = map[string]record.Run{}
		}
		return r, nil
	}
	if !errors.Is(err, git.ErrNoNote) {
		return record.Record{}, err
	}
	tree, terr := l.repo.RevParse(ctx, sha+"^{tree}")
	if terr != nil {
		return record.Record{}, terr
	}
	return record.Record{
		Schema: record.Schema, Sha: sha, Tree: tree, Port: port,
		Runs: map[string]record.Run{},
	}, nil
}

// Update is the safe read-modify-write: it takes the notes flock,
// RE-READS the record inside it, hands that fresh copy to mutate, and
// writes the result before releasing the lock.
//
// The re-read is the whole point. The verify notes are dockhand's only
// mutable state and they are edited as whole JSON documents, so a
// caller acting on a copy it read before taking the lock would write
// that staleness back over a concurrent dockhand's run. Two agents
// share a checkout now, and that lost update is what the lock exists
// to prevent.
//
// A closure returning ErrUnchanged leaves the note untouched; any
// other error abandons the write and reaches the caller.
//
// It must not be called by a caller already holding LockNotes: flock
// is per open file description and every acquire opens a fresh one, so
// such a caller would wait itself out. That caller has re-read under
// its own lock already, and wants Write.
func (l *Ledger) Update(ctx context.Context, sha, port string, mutate func(r *record.Record) error) error {
	unlock, err := l.repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := l.LoadOrStart(ctx, sha, port)
	if err != nil {
		return err
	}
	if err := mutate(&r); err != nil {
		if errors.Is(err, ErrUnchanged) {
			return nil
		}
		return err
	}
	return l.Write(ctx, r)
}

// RecordRun writes one platform's run into the commit's record, under
// the lock and over a fresh read — the update a submit, a deferral, a
// cancellation and a pre-mint verdict all arrive at.
//
// Telling the user what was recorded is the caller's job, deliberately
// left out here: the ledger writes notes, and a package that also
// wrote to a terminal would own an output ordering it cannot see.
func (l *Ledger) RecordRun(ctx context.Context, sha, port, release string, run record.Run) error {
	return l.Update(ctx, sha, port, func(r *record.Record) error {
		r.Runs[release] = run
		return nil
	})
}
