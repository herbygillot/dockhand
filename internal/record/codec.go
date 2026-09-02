package record

import (
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/verify"
)

// Schema is the note format this build writes, and the highest it will
// read. Encode stamps it; Decode refuses anything above it.
const Schema = 2

// NotesRef names the notes namespace the refusals below tell a user to
// clear. It repeats git.VerifyNotesRef because this package is a leaf
// that must not reach the repository, and the messages are pinned to
// the byte by the exit table — a parameter would let a caller print a
// ref the record was never in.
const NotesRef = "dockhand/verify"

// unrecordedPlatform keys the single run lifted out of a schema-1
// note that never named its platform.
const unrecordedPlatform = "(unrecorded)"

// legacyNote is schema 1: one flat verdict. Notes are local and
// short-lived, but an in-flight branch should survive the upgrade.
// Nothing writes this shape; it exists to be read.
type legacyNote struct {
	Schema   int        `json:"schema"`
	Sha      string     `json:"sha"`
	Tree     string     `json:"tree"`
	Port     string     `json:"port"`
	Platform string     `json:"platform"`
	State    RunState   `json:"state"`
	Job      verify.Job `json:"job"`
	Handle   string     `json:"handle"`
	Detail   string     `json:"detail"`
}

// Encode renders a record as the bytes a note holds: two-space indent,
// no trailing newline, fields in declaration order, run keys sorted by
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
// strictly rather than read hopefully. A schema from the future is
// refused, not half-read: a newer dockhand may store what this one
// cannot honour. And the embedded sha must be the commit the note is
// attached to — a mismatch means the note was copied or mangled, and
// acting on it would release or promote against the wrong tip.
//
// Malformed bytes are an error and never an absence. Absence is the
// storage layer's answer, not this one's: a note that does not parse
// must not read as a commit with no note, or a corrupt record silently
// authorizes a fresh start over state that governs release.
func Decode(b []byte, wantSha string) (Record, error) {
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return Record{}, fmt.Errorf("note on %s does not parse: %w — `git notes --ref=%s remove %s` clears it", wantSha, err, NotesRef, wantSha)
	}
	if r.Schema > Schema {
		return Record{}, fmt.Errorf("note on %s was written by a newer dockhand (schema %d, this build speaks %d); upgrade dockhand", wantSha, r.Schema, Schema)
	}
	// The sha check is deliberately asymmetric: it applies to schema-2
	// notes only, so a schema-1 note — or one with no schema key at all
	// — reaches the lift below rather than being refused for a field it
	// may not carry.
	if r.Schema == Schema && r.Sha != "" && r.Sha != wantSha {
		return Record{}, fmt.Errorf("note on %s claims to describe %s — corrupt; `git notes --ref=%s remove %s` clears it", wantSha, r.Sha, NotesRef, wantSha)
	}
	// The fast path tests Runs, not the schema alone. A schema-2 note
	// whose runs are null — which Encode writes for a nil map — falls
	// through into the lift and comes back as one unrecorded run, the
	// same shape it has always come back as.
	if r.Schema == Schema && r.Runs != nil {
		return r, nil
	}
	var l legacyNote
	if err := json.Unmarshal(b, &l); err != nil {
		return Record{}, fmt.Errorf("note on %s: %w", wantSha, err)
	}
	key := l.Platform
	if key == "" {
		key = unrecordedPlatform
	}
	return Record{
		Schema: Schema, Sha: l.Sha, Tree: l.Tree, Port: l.Port,
		Runs: map[string]Run{key: {State: l.State, Job: l.Job, Handle: l.Handle, Detail: l.Detail}},
	}, nil
}
