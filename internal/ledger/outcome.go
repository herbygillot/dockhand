package ledger

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// The audit ref is the ledger's second custody, and its rules are not
// the verification notes'. Those are working state: read, judged,
// rewritten under a flock, and removed with the branch. These are a
// log. A row is appended and never edited, discard does not sweep them,
// and what became of a publication is a further row rather than a field
// set on the one already written.
//
// The lock is the visible difference. The verification notes take
// git.LockNotes because a whole JSON document is rewritten and a
// concurrent peer's run would be lost between the read and the write;
// there is nothing here to lose, since git's own ref update is the only
// thing two appends contend for and neither carries the other's bytes.
// Staying out of that lock also keeps the audit off the critical path:
// a row is never a reason for a verification to wait, and never a
// reason for one to deadlock.
//
// What that concedes is duplicates. Two dockhands closing the same row
// set at the same instant append two closing rows, because Settle's
// guard reads before it writes and the two reads can both see an open
// set. A log tolerates that — the rows agree, and a reader takes the
// last one — and the alternative is holding the notes lock across a
// network-derived judgment, which is a far worse trade.

// Outcome appends a publication's opening row.
//
// Appended even when the mint sha already carries rows: publishing the
// same tip twice is two publications, and a log that collapsed them
// would lose the fact that review was asked for twice.
func (l *Ledger) Outcome(ctx context.Context, row record.OutcomeRow) error {
	body, err := record.EncodeOutcomeRow(row)
	if err != nil {
		return err
	}
	return l.repo.NoteAppend(ctx, git.OutcomeNotesRef, row.MintSha, body)
}

// Settle closes a publication with what became of it: the whole opening
// row again, carrying the outcome, the merge commit when there is one,
// and when it was learned.
//
// It is a no-op in the two cases that would otherwise write a lie. A
// mint sha with no rows was never published by this dockhand, and
// inventing an outcome for a publication that has no record of
// happening would put a change into the audit that the audit cannot
// account for. A set whose last row already carries an outcome is
// closed, and closing it again is what a reconciler running every few
// minutes over a rejected branch would do forever.
func (l *Ledger) Settle(ctx context.Context, mintSha string, outcome record.Outcome, mergeSha, at string) error {
	rows, err := l.Outcomes(ctx, mintSha)
	if err != nil || len(rows) == 0 {
		return err
	}
	row := rows[len(rows)-1]
	if !row.Open() {
		return nil
	}
	row.Outcome, row.MergeSha, row.SettledAt = outcome, mergeSha, at
	return l.Outcome(ctx, row)
}

// Outcomes reads the rows recorded against a mint sha, oldest first.
//
// A commit with no rows reads as no rows and not as an error. That is
// the opposite of the verification reader's rule, and deliberately:
// absence there could mean a corrupt note being taken for a fresh
// start, over state that governs worker release and promotion. Nothing
// acts on an audit row, so absence is just a change this dockhand never
// published.
//
// A row that will not parse still propagates. An audit with an
// unreadable line has to say so, because the alternative is a query
// that silently counts fewer publications than there were.
func (l *Ledger) Outcomes(ctx context.Context, mintSha string) ([]record.OutcomeRow, error) {
	body, err := l.repo.NoteRead(ctx, git.OutcomeNotesRef, mintSha)
	if errors.Is(err, git.ErrNoNote) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return record.DecodeOutcomeRows(body)
}
