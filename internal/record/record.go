// Package record is the verification record's data model and its wire
// format: the value a commit's git note holds, and the strict codec
// that turns it into bytes and back. It is a leaf — it reads and
// writes no repository, asks no provider, and reaches no tree. What
// stores a record is the ledger's business; what a record means is the
// verdict's; this package only says what a record *is*.
//
// The types here were the lifecycle package's Note and Run. They moved
// out unchanged in every byte: the JSON field names, their declaration
// order, and the omitempty set are the schema-2 wire format that
// already exists in users' notes refs and in the goldens, so the
// struct declarations below are a contract, not a convenience.
package record

import (
	"sort"

	"github.com/herbygillot/dockhand/internal/verify"
)

// Record is a commit's verification record, stored as its git note
// under the verification notes ref: sha-keyed, local to this machine,
// read back by status. Schema 2 holds one run per platform, keyed by
// the resolved release name — a commit's verdict is a set, so a
// platform-floor investigation lives in one note instead of
// overwriting itself. Each run's Job is the serializable value the
// process that collects need not have submitted; Handle names a kept
// environment, machine-local by nature.
//
// Field order is the wire order: encoding/json emits struct fields as
// declared, so reordering these five moves the bytes of every note and
// of the status --json goldens that re-marshal them.
type Record struct {
	Schema int            `json:"schema"`
	Sha    string         `json:"sha"`
	Tree   string         `json:"tree"` // content identity: a message-only amend moves Sha, not Tree
	Port   string         `json:"port"`
	Runs   map[string]Run `json:"runs"`
}

// Run is one platform's verification: running, passed, failed,
// unsupported (the port declines the platform), blocked (a dependency
// failed before the change was reached — untested, not disproven),
// canceled, superseded, deferred (no slot when asked), or errored.
//
// State and Job carry no omitempty and are always emitted, a zero Job
// included: every deferred and every known_fail run on disk today has
// `"job":{"provider":"","id":"","started":"0001-01-01T00:00:00Z"}`
// written out in full, and dropping it would move those bytes.
type Run struct {
	State  RunState   `json:"state"`
	Job    verify.Job `json:"job"`
	Handle string     `json:"handle,omitempty"`
	Detail string     `json:"detail,omitempty"`
	// Tested says the run included the port's test suite (`port test`)
	// after the install — promote's checklist vouches only for what a
	// note remembers.
	Tested bool `json:"tested,omitempty"`
	// Linted says the run led with `port lint` — every tart run does
	// now, but the note remembers rather than the code assuming, so
	// verdicts recorded before lint existed stay honest.
	Linted bool `json:"linted,omitempty"`
	// Lint is what lint actually said, read from the log as the run
	// settles: "clean", or "2 warnings". It exists because the PR body
	// vouches per checked box, and a checked lint box with no
	// corroborating evidence was the one dishonest claim in it —
	// field-caught on the first post-lint batch.
	Lint string `json:"lint,omitempty"`
}

// Platforms lists the record's run keys, sorted for stable rendering.
// The wire order is encoding/json's own sort over the map; this is the
// order humans read, and the two agreeing is a coincidence worth not
// relying on.
func (r Record) Platforms() []string {
	out := make([]string, 0, len(r.Runs))
	for k := range r.Runs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AnyState reports whether any run is in the given state.
func (r Record) AnyState(s RunState) bool {
	for _, run := range r.Runs {
		if run.State == s {
			return true
		}
	}
	return false
}

// Promotable is the gate promote applies to a verdict set: at least
// one platform passed, and none failed. A port declining a platform
// (unsupported) does not block — that refusal is often the change
// working — but an unexplained failure does, because it is exactly the
// question review will ask.
//
// It is stated here, on the record, so the rule has one home; the
// verdict package presents it as a judgment rather than restating it.
func (r Record) Promotable() bool {
	return r.AnyState(Passed) && !r.AnyState(Failed)
}
