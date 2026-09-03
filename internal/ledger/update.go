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
// commit's identity: the start of every update that has to survive the
// note not being there yet.
//
// It takes no port, and that is the schema-3 shape rather than a
// tidying. A record's ports are its subjects, and a subject is written
// when the change is minted, from the plan that named it, with the
// portdir, the intent and the target that make it worth having. A
// storage layer accepting a port here would invent a subject out of
// whichever caller happened to open the record first, and invent it
// stripped of every one of those. What this package can honestly supply
// is identity — the sha and the tree — so identity is all it supplies.
//
// Only true absence starts fresh. A malformed note, a schema this build
// does not read, a git failure — each propagates, because treating any
// of them as absence would overwrite the very state that governs worker
// release and promotion.
func (l *Ledger) LoadOrStart(ctx context.Context, sha string) (record.Record, error) {
	r, err := l.Read(ctx, sha)
	if err == nil {
		ready(&r)
		return r, nil
	}
	if !errors.Is(err, git.ErrNoNote) {
		return record.Record{}, err
	}
	tree, terr := l.repo.RevParse(ctx, sha+"^{tree}")
	if terr != nil {
		return record.Record{}, terr
	}
	r = record.Record{Schema: record.Schema, Sha: sha, Tree: tree}
	ready(&r)
	return r, nil
}

// ready makes both of a record's maps assignable.
//
// Jobs and Runs are omitempty on the wire and the codec normalizes
// neither, so a record minted with nothing submitted yet decodes with
// two nil maps — and the next thing every caller does is assign into
// one of them, where a nil map panics rather than errors. Doing it once
// here means no closure has to remember; an empty map costs nothing on
// the way back out, because Encode drops both.
func ready(r *record.Record) {
	if r.Jobs == nil {
		r.Jobs = map[string]record.JobRecord{}
	}
	if r.Runs == nil {
		r.Runs = map[string]record.Run{}
	}
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
// It is also what makes a compare-and-set possible at all: a closure
// that checks the fresh record against what it observed before the lock
// — a run's state, the job its platform names — can drop its own
// conclusion when the note moved underneath it. Both maps are handed
// over fresh, so a compare spanning the two sees one consistent read.
//
// A closure returning ErrUnchanged leaves the note untouched; any
// other error abandons the write and reaches the caller.
//
// It must not be called by a caller already holding LockNotes: flock
// is per open file description and every acquire opens a fresh one, so
// such a caller would wait itself out. That caller has re-read under
// its own lock already, and wants Write.
func (l *Ledger) Update(ctx context.Context, sha string, mutate func(r *record.Record) error) error {
	unlock, err := l.repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := l.LoadOrStart(ctx, sha)
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

// RecordRun writes one subject's verdict on one platform into the
// commit's record, under the lock and over a fresh read — the update a
// deferral, a declined platform, a cancellation and a pre-mint verdict
// all arrive at. A submission that starts an environment wants
// RecordSubmission, which lands the guest and its runs together.
//
// The run is keyed by RunKey(port, release) from day one, while every
// change still has a single subject and the port half of the key looks
// like decoration. Re-keying notes already written is the coordination
// event this schema exists to spend exactly once.
//
// Platform is stamped from the release for the same reason Encode
// stamps the schema: the key and the field are two spellings of one
// fact, and a caller keeping them equal by hand would eventually not.
func (l *Ledger) RecordRun(ctx context.Context, sha, port, release string, run record.Run) error {
	return l.Update(ctx, sha, func(r *record.Record) error {
		run.Platform = release
		adoptSubject(r, port)
		r.Runs[record.RunKey(port, release)] = run
		return nil
	})
}

// AdoptSubjects names a change's whole roster among the record's
// subjects, in build order, before anything is concluded about any of
// them.
//
// Order is the reason it exists. Subjects[0] is the headline — the port
// the branch is named for, the one a refusal names, the one a later
// verify resolves the branch to — and the subjects of a record nobody
// minted are built by adoption, in whatever order the calls happen to
// arrive. A submission that recorded one member's verdict before
// stating the roster would put that member at the head of the change:
// a pre-flight refusal, of all things, renaming what the change is
// about.
//
// It writes nothing when the record already names every port, which is
// every change dockhand minted. A note object per submission that had
// nothing to add is noise in the ref and a difference a reader can see.
func (l *Ledger) AdoptSubjects(ctx context.Context, sha string, ports []string) error {
	return l.Update(ctx, sha, func(r *record.Record) error {
		was := len(r.Subjects)
		for _, port := range ports {
			adoptSubject(r, port)
		}
		if len(r.Subjects) == was {
			return ErrUnchanged
		}
		return nil
	})
}

// adoptSubject names a port among the record's subjects when nothing
// already does.
//
// Records are born at mint carrying their subjects, so this is reached
// only where there was no mint to be born at: a verify run over a
// branch this build did not create, or one whose note a peer discarded
// mid-pass. Without it the port would survive in the run key alone,
// where no projection reads it — Headline, Ports and Portdirs all walk
// the subjects — and the record would render a verdict about nobody.
//
// It appends and never rewrites. A subject minted with a portdir, an
// intent and a target is the good copy, and a bare one arriving later
// must not flatten it.
//
// Names is left empty rather than written as [port], which is the
// opposite of what the mint path does and is the honest answer here:
// [port] means the subports were asked about and there are none, and
// nobody asked. The ledger does not read a Portfile.
func adoptSubject(r *record.Record, port string) {
	if port == "" {
		return
	}
	for _, s := range r.Subjects {
		if s.Port == port {
			return
		}
	}
	r.Subjects = append(r.Subjects, record.Subject{Port: port})
}
