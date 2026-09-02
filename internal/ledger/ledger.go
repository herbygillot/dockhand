// Package ledger is custody of the verification notes: the read, the
// flock, the re-read inside it, the write, the removal, and the scan
// over every annotated commit. It is the only thing in the tree that
// knows a verification record is kept in a git note.
//
// The split it completes is the point. record says what a record is,
// verdict says what one is worth, and this package says where one
// lives — so a judgment can be read and tested with no repository
// behind it, and a storage bug cannot hide inside a decision. What
// comes back out here is a record.Record and nothing else: the ledger
// has no opinion about what it found, and forms none while looking.
//
// It has no opinion about output either. What is said under the lock
// belongs to the verb that took it; a ledger that printed would own an
// ordering it cannot see.
package ledger

import (
	"context"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Ledger is one repository's verification notes.
type Ledger struct{ repo *git.Repo }

// Open binds a ledger to a repository. Nothing here can fail: the
// notes ref need not exist, and a repository carrying no notes at all
// reads as one whose every commit is simply unnoted.
func Open(repo *git.Repo) *Ledger { return &Ledger{repo: repo} }

// Read returns a commit's record, git.ErrNoNote when the commit has
// none.
//
// Absence is the storage layer's answer and refusal is the codec's,
// and the two must not be confused: a note that will not parse comes
// back as an error, never as a commit with no note, because the
// callers that start fresh on absence would otherwise start fresh over
// state that governs worker release and promotion.
func (l *Ledger) Read(ctx context.Context, sha string) (record.Record, error) {
	body, err := l.repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	if err != nil {
		return record.Record{}, err
	}
	return record.Decode(body, sha)
}

// Write records r on the commit it names, replacing what was there:
// the note is that commit's current record, and its history lives in
// the notes ref's own commits.
//
// It writes what it is handed and takes no lock. That makes it the
// primitive Update is built from, and the right call only for a caller
// already inside its own LockNotes critical section, which has done
// the re-read itself. Everything else wants Update, which cannot lose
// a concurrent peer's run.
func (l *Ledger) Write(ctx context.Context, r record.Record) error {
	body, err := record.Encode(r)
	if err != nil {
		return err
	}
	return l.repo.NoteWrite(ctx, git.VerifyNotesRef, r.Sha, body)
}

// Remove drops a commit's record. A commit with no note is fine —
// removal is idempotent, and discard sweeps commits that may never
// have carried one.
func (l *Ledger) Remove(ctx context.Context, sha string) error {
	return l.repo.NoteRemove(ctx, git.VerifyNotesRef, sha)
}

// All lists the commits carrying a record, in the order git reports
// them.
//
// It hands back shas rather than records because the scans over them
// disagree about what an unreadable note means: the promotion scan
// refuses on one, the drift scan and the orphan sweep step over it.
// Reading is left to each caller so each keeps its own reading. The
// order is left alone for the same reason — two of those scans return
// the first match, so sorting here would quietly change which record
// wins.
func (l *Ledger) All(ctx context.Context) ([]string, error) {
	return l.repo.NotesList(ctx, git.VerifyNotesRef)
}
