package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// ExtendRequest is one more commit on a change that already exists:
// the branch to grow, where the caller last saw its tip, the commit to
// add, and the members that commit brings with it.
//
// ExpectedTip is a lease and not a convenience. Two sessions extending
// one branch must not both win — the second would either bury the
// first's commit or discard it — so the tip the caller read is handed
// down to git, which does the swap under it. A caller that re-reads the
// tip itself before calling reopens exactly the window this closes.
type ExtendRequest struct {
	Branch      string
	ExpectedTip string
	Commit      git.Commit
	// Subjects are the members the new commit adds. They are unioned
	// with the ones the change already has rather than replacing them:
	// a cohort grows by gaining members, and the headline the branch is
	// named for stays where it is.
	Subjects []record.Subject
}

// Extend adds one commit to a change and gives the new tip its own
// record: the union of the subjects, the findings copied across with
// the dispositions a person already gave them, and Evidence naming the
// tip whose runs those findings were measured on.
//
// This is how a cohort is assembled. The finding that says "these four
// dependents need a revision bump" is measured on the headline's own
// verification, and accepting it adds four members to a change that
// has already been built once — so the new tip inherits the evidence
// rather than pretending to have earned it, and the old tip's runs are
// superseded because the thing they were about has moved.
//
// The old record is read twice, and the two reads answer different
// questions. The first is before anything moves and is a refusal: a
// note this build cannot read is not a note to extend past, and finding
// that out after the ref has advanced would leave a tip carrying
// nothing. The second is inside the new tip's own write, under the
// notes flock, and is the copy that is actually carried across —
// because the commit and the ref update are two git subprocesses wide,
// and anything a person wrote onto the old tip's note in that window
// (an answer to a finding, a hold, a rider) would otherwise be read
// from before they wrote it and silently dropped. The lease closes the
// race on the ref; only the lock closes this one.
//
// Everything that describes the CHANGE carries over — what it is
// called, where it is bound, who asked, what it closes, what it was
// minted on — and nothing that describes a RUN does: the new tip has no
// jobs and no runs, and Evidence.From is the honest pointer to where
// the evidence actually is.
//
// A failure to write that record is the answer and not a warning, which
// is where this parts company with the mint's own note. A mint whose
// note fails leaves a branch whose diff can still be re-read for
// everything the note would have said; an extend whose note fails
// leaves a new tip carrying none of the findings, none of their
// dispositions and no pointer to the verification they came from, and
// no diff can reconstruct any of it. So the error travels, naming the
// tip that exists, and the branch can be looked at.
//
// BuildCohort is its one caller: accepting a revbump proposal is the
// only thing that adds a commit to a change that has already been
// verified once.
func (e *Engine) Extend(ctx context.Context, repo *git.Repo, req ExtendRequest) (string, error) {
	l := e.Ledger(repo)
	if _, err := l.Read(ctx, req.ExpectedTip); err != nil && !errors.Is(err, git.ErrNoNote) {
		// A note this build cannot read is not a note to extend past:
		// the findings it holds would be dropped silently, and the tip
		// would move with nothing left to say what was lost.
		return "", fmt.Errorf("extending %s: reading the record at %s: %w", req.Branch, git.Abbrev(req.ExpectedTip), err)
	}
	tip, err := repo.Extend(ctx, req.Branch, req.ExpectedTip, req.Commit)
	if err != nil {
		return "", err
	}
	if err := l.Update(ctx, tip, func(r *record.Record) error {
		// Read here and not before the commit: Update holds the notes
		// flock, so this is the newest the old tip's record can be, and
		// no writer can land between it and the write below.
		old, err := l.Read(ctx, req.ExpectedTip)
		if err != nil && !errors.Is(err, git.ErrNoNote) {
			return err
		}
		r.Slug = old.Slug
		r.Subjects = unionSubjects(old.Subjects, req.Subjects)
		r.Destination = old.Destination
		r.AskedBy = old.AskedBy
		r.Agent = old.Agent
		r.MintedVia = old.MintedVia
		r.Riders = old.Riders
		r.Findings = copyFindings(old.Findings)
		r.ClosesTicket = old.ClosesTicket
		// Carried, both of them, because both are about the change and
		// not about the commit. A hold a person placed is not shed by
		// adding a commit, and a change a newer sibling replaced is still
		// replaced when it grows.
		r.Hold = old.Hold
		r.SupersededBy = old.SupersededBy
		// Never re-derived. Base is what the change was written against,
		// and asking git again here would answer with the change's own
		// first commit — a cohort reporting a tree that is zero days old
		// and an ABI baseline measured against itself.
		r.Base = old.Base
		r.Evidence = &record.Measured{From: req.ExpectedTip}
		return nil
	}); err != nil {
		return "", fmt.Errorf("extending %s: %s is committed but has no record: %w — `dockhand status` shows the branch, and the findings from %s are not on it",
			req.Branch, git.Abbrev(tip), err, git.Abbrev(req.ExpectedTip))
	}
	// The old tip's environments go back. A run that was still building
	// is building the wrong commit now, and a failed run's kept
	// environment documents code the branch has moved past — the pin a
	// field run watched hold an admission slot forever. What it
	// deliberately leaves standing is the passes, which is the point:
	// Evidence.From has just named them as this record's evidence, and a
	// sweep that superseded them would erase what it was pointing at.
	//
	// Its failure travels WITH the tip, which is the doctrine this
	// function opened with: the commit is written, the ref is advanced
	// under the lease and the record is complete, so a caller handed
	// back an empty string would report a failure it cannot name and
	// would retry from a tip that has already moved. What is left undone
	// is the sweep, and the sentence says so.
	if err := e.SupersedeStale(ctx, repo, req.Branch, tip); err != nil {
		return tip, fmt.Errorf("extending %s: %s is committed and recorded, but the old tip's environments were not released: %w — `dockhand status` shows the branch",
			req.Branch, git.Abbrev(tip), err)
	}
	return tip, nil
}

// unionSubjects grows a change's membership: the members it already
// has, in the order it already has them, then the ones it did not.
//
// Old order first, and the head untouched. Subjects[0] is the headline
// — the port the branch is named for and the one a refusal names — so
// a union that let an arriving member sort ahead of it would rename
// the change.
//
// An arriving member that names a port already present does not
// replace it. A subject minted with a portdir, an intent and a target
// is the good copy, and the same rule the ledger's own adoption
// follows applies here for the same reason. What it may do is fill a
// blank: a subject the ledger adopted from a run key carries a port and
// nothing else, and stating what it lives in takes nothing away.
func unionSubjects(old, add []record.Subject) []record.Subject {
	out := make([]record.Subject, 0, len(old)+len(add))
	at := make(map[string]int, len(old))
	for _, s := range old {
		at[s.Port] = len(out)
		out = append(out, s)
	}
	for _, s := range add {
		i, ok := at[s.Port]
		if !ok {
			at[s.Port] = len(out)
			out = append(out, s)
			continue
		}
		fillBlanks(&out[i], s)
	}
	return out
}

// fillBlanks states what the standing subject did not say, and
// overwrites nothing it did.
func fillBlanks(into *record.Subject, from record.Subject) {
	if len(into.Names) == 0 {
		into.Names = from.Names
	}
	if into.Portdir == "" {
		into.Portdir = from.Portdir
	}
	if into.Intent == "" {
		into.Intent = from.Intent
	}
	if into.Target == "" {
		into.Target = from.Target
	}
	if into.Reason == "" {
		into.Reason = from.Reason
	}
}

// copyFindings carries the change's findings onto its new tip with
// their answers intact.
//
// Dispositions are not reset and At is not re-stamped. A finding a
// person dismissed and that came back proposed on the next commit
// would ask them a question they have already answered, which is the
// whole failure Disposition exists to prevent.
//
// The Candidates slice is copied too rather than shared. Nothing
// mutates a finding in place today, but a copy that shares its backing
// array is a copy only until something does — and the verb that
// answers a finding is the next one to be written.
func copyFindings(in []record.Finding) []record.Finding {
	if len(in) == 0 {
		return nil
	}
	out := make([]record.Finding, len(in))
	copy(out, in)
	for i := range out {
		if len(in[i].Candidates) == 0 {
			continue
		}
		out[i].Candidates = make([]record.Candidate, len(in[i].Candidates))
		copy(out[i].Candidates, in[i].Candidates)
	}
	return out
}
