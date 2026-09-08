package statestore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
)

// ErrNoChangeAt is Export refusing a commit no change in the store is
// bound to. It is a sentinel because a re-export pass meets it
// legitimately — a note on a commit whose change was compacted away, or
// whose state ref was recreated — and must step over that note rather
// than abandon the pass, where an operation exporting the change it has
// just amended meets it only as a bug and says so.
var ErrNoChangeAt = errors.New("statestore: no change in the store is at this commit")

// Export writes the derived verify note onto a mint commit, stamped with
// the state commit it came from so a stale note is detectable. Nothing
// reads the note to make a decision; it exists so `git log --notes` and
// a reviewer looking at the commit see what they see today.
//
// It takes the ledger rather than writing a note itself, and the direct
// import is deliberate: the store PROJECTS and the ledger WRITES, and
// the alternative puts a second note-writer in a tree whose whole point
// is that there is one. No interface, because there is one ledger and
// substituting it in a test means a temporary repository.
//
// WHO CALLS IT, since a derived view nobody derives is just an absent
// one: the operation that changed a commit-bound fact, immediately
// after its Amend — mint, settle, publish. And `cycle` re-exports any
// note whose stamp is behind the current state commit, which is the
// backstop for a process that died between the two writes. Every note
// dockhand writes comes through here, so there is no second, unstamped
// writer for the re-export to silently overwrite.
//
// app.exportNote is that caller, deferred at each road's end so that one
// operation amending three times — mint, enqueue, then settle under
// --wait — exports the state it LEAVES rather than each state it passed
// through; and Cycle.reexport is the pass's unconditional sweep behind
// it. The one road that does not export is `discard`, which REMOVES
// through ledger.Remove instead: the change is over, and once compaction
// drops its record a re-export could only answer ErrNoChangeAt, which
// would leave the stale note permanently uncorrectable.
//
// THE STAMP HAS NO FIELD YET, and this comment is the honest form of
// that. record.Record — the ruled shape, one package over — carries
// Schema, Sha and Tree and no place to record the state commit this
// projection was read from, so what is written here is the projection
// and not yet the stamp. The property it costs is the detection, not the
// safety: a stale note still cannot disagree with anything, because
// nothing reads a note to decide. The re-export `cycle` owes is
// therefore unconditional until record grows the field.
//
// It takes the Lock, which is the ruling's whole point: one lock over
// the state ref AND the note export, so the note a person reads was
// projected from a state no concurrent Amend was in the middle of
// moving. It must therefore never be called from inside an Amend
// closure — the flock is not reentrant, and a nested take is a writer
// waiting out its own deadline against itself.
func (s *Store) Export(ctx context.Context, l *ledger.Ledger, sha string) error {
	unlock, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.Read(ctx)
	if err != nil {
		return err
	}
	tree, err := s.repo.RevParse(ctx, sha+"^{tree}")
	if err != nil {
		return err
	}
	rec, err := project(st, sha, tree)
	if err != nil {
		return err
	}
	return l.Write(ctx, rec)
}

// project renders one commit's note from a consistent read of the
// store. Everything it produces is a copy of what the four documents
// already say; nothing is decided here, and a projection that had to
// decide something would be a second authority wearing a view's name.
func project(st State, sha, tree string) (record.Record, error) {
	change, ok := changeAt(st, sha)
	if !ok {
		return record.Record{}, fmt.Errorf("%w: %s", ErrNoChangeAt, sha)
	}
	rec := record.Record{
		Sha:    sha,
		Tree:   tree,
		Change: change,
		Runs:   runsAt(st, sha),
		Leases: leasesOf(st, change.ID),
	}
	rec.Publication = publicationOf(st, change.ID, rec.Runs)
	return rec, nil
}

// changeAt finds the change bound to a commit. The binding is the
// change's own Tip, which is the commit a mint or an extension left
// behind and therefore the commit whose note this is; a change closed
// or superseded still answers, because the note on a dead branch's last
// commit is exactly what a reviewer looking at that commit wants.
//
// Ids are walked in sorted order so that two changes claiming one tip —
// which the lifecycle refuses, and which a recreated ref could still
// leave behind — export the same one on every pass rather than
// alternating with map order.
func changeAt(st State, sha string) (record.Change, bool) {
	for _, id := range slices.Sorted(maps.Keys(st.Changes)) {
		if c := st.Changes[id]; c.Tip == sha {
			return c, true
		}
	}
	return record.Change{}, false
}

// runsAt is the note's run map: the attempts against this commit,
// flattened to (port, platform).
//
// THE COLLAPSE IS MANY-TO-ONE and the rule is record.Record.Runs' own:
// take the attempts whose Sha is this commit, and for each (member,
// platform) keep the most recently STARTED attempt that reached a
// terminal verdict. A retry therefore replaces its predecessor in the
// note while both survive in the store, which is the right way round —
// the note is what a person reads, and the store is what happened.
//
// A key no terminal verdict covers keeps the most recently started
// UNSETTLED attempt's word instead, which is how a queued attempt
// reaches the note at all: a person reading the commit sees that a run
// is waiting rather than an empty map that says nothing is happening.
// A terminal verdict always outranks an unsettled one, whatever the
// dates, because the note's job is to say what was proved.
func runsAt(st State, sha string) map[record.RunKey]record.Run {
	type held struct {
		run      record.Run
		started  time.Time
		terminal bool
	}
	best := map[record.RunKey]held{}
	take := func(key record.RunKey, cand held) {
		switch prev, seen := best[key]; {
		case !seen,
			cand.terminal && !prev.terminal,
			cand.terminal == prev.terminal && cand.started.After(prev.started):
			best[key] = cand
		}
	}
	for _, id := range slices.Sorted(maps.Keys(st.Attempts)) {
		a := st.Attempts[id]
		if a.Sha != sha {
			continue
		}
		for _, member := range slices.Sorted(maps.Keys(a.Runs)) {
			run := a.Runs[member]
			take(record.RunKey{Port: member, Platform: a.Platform},
				held{run: run, started: a.Started, terminal: run.State.Terminal()})
		}
		if !a.Queued() {
			continue
		}
		// A queued attempt has no Runs of its own: what the note shows for
		// it is the ask, against every member it will build.
		for _, member := range a.Members {
			take(record.RunKey{Port: member, Platform: a.Platform},
				held{run: record.Run{Ask: a.Ask, State: record.Queued, Content: a.Content}, started: a.Started})
		}
	}
	if len(best) == 0 {
		return nil
	}
	runs := make(map[record.RunKey]record.Run, len(best))
	for key, h := range best {
		runs[key] = h.run
	}
	return runs
}

// leasesOf is the note's lease section: this change's leases, keyed by
// platform as record.Record declares. Two leases on one platform is the
// ordinary shape over a change's life — one per attempt — so the LIVE
// one wins, and among leases in the same standing the one that started
// last, which is the one a person asking "what is holding an
// environment for this change" means.
func leasesOf(st State, id record.ChangeID) map[string]record.Lease {
	out := map[string]record.Lease{}
	for _, key := range slices.Sorted(maps.Keys(st.Leases)) {
		l := st.Leases[key]
		if l.Change != id {
			continue
		}
		prev, seen := out[l.Platform]
		switch {
		case !seen,
			!l.Returned() && prev.Returned(),
			l.Returned() == prev.Returned() && l.ID.Started.After(prev.ID.Started):
			out[l.Platform] = l
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// publicationOf is the note's readable copy of what became of the change
// on the forge. An OPEN publication outranks a settled one — a change
// republished after a rejection is asking about the open attempt — and
// among equals the one whose first step is latest wins.
//
// Unproven is counted over the runs this note carries and not over the
// store, because it is the number a reader can check against the table
// printed beside it: the members that were published without a pass,
// which is a failure, a block, or a withholding.
func publicationOf(st State, id record.ChangeID, runs map[record.RunKey]record.Run) *record.PublicationState {
	var best record.Publication
	found := false
	for _, key := range slices.Sorted(maps.Keys(st.Publications)) {
		p := st.Publications[key]
		if p.Change != id {
			continue
		}
		switch {
		case !found,
			!p.Outcome.Settled() && best.Outcome.Settled(),
			p.Outcome.Settled() == best.Outcome.Settled() && openedAt(p).After(openedAt(best)):
			best, found = p, true
		}
	}
	if !found {
		return nil
	}
	state := &record.PublicationState{
		Number:      best.Number,
		URL:         best.URL,
		PublishedBy: best.By,
		PublishedAt: openedAt(best),
	}
	for _, run := range runs {
		//nolint:exhaustive // the three states that mean "published without a pass" are the whole of the count; every other state either proved the member or says nothing was published yet
		switch run.State {
		case record.Failed, record.Blocked, record.Withheld:
			state.Unproven++
		}
	}
	return state
}

// openedAt is when a publication was first attempted: its first step's
// instant, which is written before the effect it records rather than
// after, and the zero time for a row that has taken no step yet.
func openedAt(p record.Publication) time.Time {
	if len(p.Steps) == 0 {
		return time.Time{}
	}
	return p.Steps[0].At
}
