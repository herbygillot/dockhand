package record

import (
	"encoding/json"
	"errors"
	"fmt"
)

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
	// ErrSchemaTooOld reports a note from before the current schema.
	// There is no lift: the note is a different shape, not a wider one,
	// and — because the note is a derived export of the state ref — the
	// remedy is to discard it and regenerate rather than to migrate it.
	ErrSchemaTooOld = errors.New("record: note predates this build's schema")
	// ErrShaMismatch reports a note that names a commit other than the
	// one it is attached to — the note was copied or mangled, and acting
	// on it would release or promote against the wrong tip.
	ErrShaMismatch = errors.New("record: note describes another commit")
	// ErrMalformedRunKey reports run-map key bytes that are not a
	// "port@platform" pair. It exists so the ONE place that reads the
	// joined spelling back refuses what it cannot split, where the three
	// hand-written scanners it replaces each answered "" and carried on.
	ErrMalformedRunKey = errors.New("record: run key is not port@platform")
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

// runKeySep separates a run key's two halves on the wire. The names
// are joinable because neither can carry an "@" — a port name is
// [A-Za-z0-9._+-] and a platform name is Apple's marketing word — so
// the key is unambiguous without quoting or escaping.
const runKeySep = '@'

// MarshalText spells a RunKey as the "port@platform" bytes a JSON
// object key must be. It exists because encoding/json will not use a
// struct as a map key at all, and the alternative — keeping the joined
// string as the Go type — is the struct-wearing-a-string's-clothes that
// RunKey was made to end. The pair is the type; the join is the wire,
// and it happens HERE and in Unmarshal below, which is one place rather
// than the three hand-written scanners it replaces.
func (k RunKey) MarshalText() ([]byte, error) {
	out := make([]byte, 0, len(k.Port)+1+len(k.Platform))
	out = append(out, k.Port...)
	out = append(out, runKeySep)
	out = append(out, k.Platform...)
	return out, nil
}

// UnmarshalText reads the pair back, refusing bytes with no separator.
// A key that names no platform is not a run this build can place, and
// answering with a half-filled pair would put every unparseable key in
// one bucket keyed by the empty platform — a collision the reader could
// never see.
func (k *RunKey) UnmarshalText(b []byte) error {
	for i := 0; i < len(b); i++ {
		if b[i] == runKeySep {
			k.Port, k.Platform = string(b[:i]), string(b[i+1:])
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrMalformedRunKey, string(b))
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
// The note is a derived export and the state ref is the authority, so a
// note this build cannot read is cleared and regenerated rather than
// lifted. What is NOT refused is a key this build does not know:
// encoding/json ignores it, and that is deliberate for the NOTE alone —
// the state documents get the opposite discipline, which is what
// DocSchema is for.
//
// The schema is checked before the sha. A note from another schema is
// unreadable whatever commit it names, and the remedy — remove it — is
// the same either way; checking the sha first would answer an
// older-schema note with a sentence about corruption.
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
	// Anything older is refused outright, and there is no lift. Schema 4
	// is a different shape and not a wider one: the change, the runs, the
	// leases and the publication are four owned sections where the older
	// note had one flat record, so a lift would have to invent the
	// answers. It costs nothing to refuse, because the note is a
	// projection — the state ref still holds what happened, and the
	// export is rewritten from it.
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
