// Package ledger is custody of the verification notes: the read, the
// write, the removal, and the scan over every annotated commit. It is
// the only thing in the tree that knows a record can be kept in a git
// note, and it keeps its name because that charter is unchanged.
//
// WHAT CHANGED IS WHAT THE NOTE IS WORTH. It used to be dockhand's only
// mutable state — read, judged, and rewritten as a whole JSON document
// under a flock, with a general Update handing each caller a fresh copy
// to mutate. It is now a DERIVED EXPORT: the state ref is the
// authority, statestore.Export projects a change onto its mint commit
// and calls Write, and nothing reads a note to decide anything. The
// note exists so that `git log --notes` and a reviewer looking at the
// commit see what they see today.
//
// Three subtractions follow from that one sentence, and together they
// are the whole of this package's shrinkage.
//
// There is no read-modify-write here. Update mutated every section of
// the record through one closure, which is exactly the writer nobody
// owned; each of the four lifecycles now advances its own documents
// through statestore's typed *Txn mutators — internal/change,
// internal/run, internal/lease, internal/publish — and a projection
// derived from those documents has nothing left to amend.
//
// There is no lock here either. The one that matters is statestore's,
// taken around the state ref and this export together (ruled
// 2026-09-06), so a note and the records it was projected from land
// under one serialization. A flock in this package would be a second
// one, guarding a copy.
//
// And there is no audit ref. What became of a publication is a
// record.Publication in the store, keyed by the change rather than by
// whichever commit happened to be the tip when a forge call was made —
// the append-only rows existed for the note-as-authority era, and the
// branch that was extended after its pull request opened left one of
// them open forever.
//
// What comes back out is a record.Record and nothing else: the ledger
// has no opinion about what it found, and forms none while looking. It
// has no opinion about output either. What is said around a write
// belongs to the operation that made it; a ledger that printed would
// own an ordering it cannot see.
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
// and the two stay different answers because they are different facts.
// A commit with no note is one the export has not reached; a note that
// will not parse — or that a schema bump has made unreadable, which
// schema 4 does to every note in every checkout — is one whose bytes
// are wrong. Both are cured by re-exporting, since the store still
// holds what happened, but only the second is worth reporting, and a
// reader that could not tell them apart would report a corrupt notes
// ref as an empty one.
func (l *Ledger) Read(ctx context.Context, sha string) (record.Record, error) {
	body, err := l.repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	if err != nil {
		return record.Record{}, err
	}
	return record.Decode(body, sha)
}

// Write records r on the commit it names, replacing what was there:
// the note is that commit's current projection, and its history lives
// in the notes ref's own commits.
//
// It writes what it is handed and takes no lock, which is right for
// the one caller it has. statestore.Export builds the projection under
// the store's lock, from the state it read there, and hands it over;
// this package neither re-reads nor merges, because there is nothing
// to merge — a second writer of a derived ref would be reconciling two
// copies of one authority instead of rewriting from it.
//
// The schema is stamped by record.Encode rather than trusted from r,
// so a record decoded under an older schema and handed straight back
// cannot be written out claiming to be what it was.
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
// It hands back shas rather than records because a scan over notes is
// a scan over an export, and what an unreadable one means is the
// caller's to say: a re-export steps over it and rewrites it, where a
// report has to show that the projection on a commit is broken.
// Reading is left to each caller so each keeps its own reading. The
// order is left alone for the same reason — sorting here would quietly
// change which record a first-match scan finds.
func (l *Ledger) All(ctx context.Context) ([]string, error) {
	return l.repo.NotesList(ctx, git.VerifyNotesRef)
}
