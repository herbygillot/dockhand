package record

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Schema is the note format this build writes, and the only one it
// reads. Encode stamps it; Decode refuses anything else.
const Schema = 3

// NotesRef names the notes namespace the refusals below tell a user to
// clear. It repeats git.VerifyNotesRef because this package is a leaf
// that must not reach the repository, and the messages are pinned to
// the byte by the exit table — a parameter would let a caller print a
// ref the record was never in.
const NotesRef = "dockhand/verify"

// The refusals, as identities a caller can branch on. The sentence a
// person reads is built at the point of refusal and is not any of
// these words: a message may be reworded without moving what code
// matches, which is the whole reason the two are separate.
var (
	// ErrMalformed reports bytes that are not a record at all. It is
	// never an absence: a note that does not parse must not read as a
	// commit with no note, or a corrupt record silently authorizes a
	// fresh start over state that governs worker release.
	ErrMalformed = errors.New("record: note does not parse")
	// ErrSchemaTooNew reports a note a newer dockhand wrote. It is
	// refused rather than half-read, because a newer build may record
	// state this one would act on wrongly.
	ErrSchemaTooNew = errors.New("record: note is from a newer dockhand")
	// ErrSchemaTooOld reports a note from before schema 3. There is no
	// lift: the schema-3 record is a different shape, not a wider one,
	// and the remedy is to discard the note and re-earn the evidence.
	ErrSchemaTooOld = errors.New("record: note predates schema 3")
	// ErrShaMismatch reports a note that names a commit other than the
	// one it is attached to — the note was copied or mangled, and acting
	// on it would release or promote against the wrong tip.
	ErrShaMismatch = errors.New("record: note describes another commit")
)

// refusal is one of the four, as a caller matches it and as a person
// reads it. Unwrap answers with both the identity and the cause when
// there is a cause, so `errors.Is(err, ErrMalformed)` and the json
// error underneath it are both still reachable.
type refusal struct {
	kind  error
	cause error
	msg   string
}

func (e *refusal) Error() string { return e.msg }

func (e *refusal) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

// refuse builds a refusal: the identity, the cause it wraps if any,
// and the sentence.
func refuse(kind, cause error, format string, a ...any) error {
	return &refusal{kind: kind, cause: cause, msg: fmt.Sprintf(format, a...)}
}

// Encode renders a record as the bytes a note holds: two-space indent,
// no trailing newline, fields in declaration order, map keys sorted by
// encoding/json, and HTML escaping left on, so a Detail carrying <, >
// or & is written as the notes on disk already carry it.
//
// The schema is stamped here rather than trusted from the caller, and
// on this function's own copy: a record read back from a note keeps
// whatever schema it was decoded under, and no caller should have to
// remember to reset it before writing.
//
// git completes the final line when it stores the result, so a note
// read back is Encode's bytes plus one newline, never Encode's alone.
func Encode(r Record) ([]byte, error) {
	r.Schema = Schema
	return json.MarshalIndent(r, "", "  ")
}

// Decode reads a note's bytes as the record for wantSha.
//
// Notes govern worker release and promotion, so they are validated
// strictly rather than read hopefully. What is NOT refused is a key
// this build does not know: encoding/json ignores it, and that is the
// additive policy stated on purpose — a field appended by a later
// build is read past by this one, and only a change to what an
// existing key MEANS bumps the number.
//
// The schema is checked before the sha. A note from another schema is
// unreadable whatever commit it names, and the remedy — remove it — is
// the same either way; checking the sha first would answer a schema-2
// note with a sentence about corruption.
func Decode(b []byte, wantSha string) (Record, error) {
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return Record{}, refuse(ErrMalformed, err,
			"note on %s does not parse: %v — `git notes --ref=%s remove %s` clears it",
			wantSha, err, NotesRef, wantSha)
	}
	if r.Schema > Schema {
		return Record{}, refuse(ErrSchemaTooNew, nil,
			"note on %s was written by a newer dockhand (schema %d, this build speaks %d); upgrade dockhand",
			wantSha, r.Schema, Schema)
	}
	// Anything older is refused outright, and the schema-1 lift that
	// used to sit here is gone. Schema 3 keys its runs by subject and
	// platform, splits the environment off from the verdict, and records
	// the change at mint; an older note answers none of those questions,
	// so a lift would have to invent the answers. The remedy is both
	// halves of one sentence — discard what is there, then re-mint the
	// evidence — because removing the note alone leaves a branch that
	// looks unverified for a reason nobody can see.
	if r.Schema < Schema {
		return Record{}, refuse(ErrSchemaTooOld, nil,
			"note on %s is schema %d and this build reads only %d — the old evidence cannot be carried over; "+
				"`git notes --ref=%s remove %s` discards it, and `dockhand verify <branch>` re-earns it",
			wantSha, r.Schema, Schema, NotesRef, wantSha)
	}
	// A note that records no sha of its own names no commit and so
	// cannot be a copy of another one's. The check is for the note that
	// names the WRONG commit, which is what a copied or mangled one
	// looks like.
	if r.Sha != "" && r.Sha != wantSha {
		return Record{}, refuse(ErrShaMismatch, nil,
			"note on %s claims to describe %s — corrupt; `git notes --ref=%s remove %s` clears it",
			wantSha, r.Sha, NotesRef, wantSha)
	}
	return r, nil
}
