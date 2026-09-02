package ledger

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// EvidenceFor reports the verification covering a tip — its own
// record, or any record over the identical tree, since a message-only
// amend moves the sha and not the content — and whether that verdict
// set clears promote's gate.
//
// The strict reader's refusals PROPAGATE here and absence alone falls
// through to the scan. Promotion is where a verdict authorizes
// publication, and reading a corrupt or future-schema tip note as mere
// absence would let an older same-tree record authorize it instead.
//
// The scan returns the first same-tree record that clears the gate, in
// All's order. Which one wins is therefore git's answer, not a
// preference of this package's, and it stays that way as long as
// nothing sorts the list.
func (l *Ledger) EvidenceFor(ctx context.Context, tip string) (record.Record, bool, error) {
	r, err := l.Read(ctx, tip)
	if err == nil {
		return r, r.Promotable(), nil
	}
	if !errors.Is(err, git.ErrNoNote) {
		return record.Record{}, false, err
	}
	tree, err := l.repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return record.Record{}, false, err
	}
	noted, err := l.All(ctx)
	if err != nil {
		return record.Record{}, false, err
	}
	for _, sha := range noted {
		r, err := l.Read(ctx, sha)
		if err != nil {
			// A note removed between the listing and the read is a race
			// with a concurrent discard, not a corrupt ledger. Anything
			// else stops the scan: a note this build cannot read is one
			// it cannot rule out as the evidence.
			if errors.Is(err, git.ErrNoNote) {
				continue
			}
			return record.Record{}, false, err
		}
		if r.Tree == tree && r.Promotable() {
			return r, true, nil
		}
	}
	return record.Record{}, false, nil
}
