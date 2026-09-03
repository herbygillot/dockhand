// Package record is the verification record's data model and its wire
// format: the value a commit's git note holds, and the strict codec
// that turns it into bytes and back. It is a leaf — it reads and
// writes no repository, asks no provider, and reaches no tree. What
// stores a record is the ledger's business; what a record means is the
// verdict's; this package only says what a record *is*.
//
// Schema 3 is a clean break. Schema 2 held one flat port and one run
// per platform; this one holds a change — its subjects, where it is
// bound, what was found about it, and one verdict per subject per
// platform. Every field the overhaul's later steps write is declared
// here even where nothing writes it yet, because a field added to a
// note format after the fact is a coordination event between every
// checkout that reads one, and the point of landing all of them at
// once is to spend that event exactly once.
//
// Declaration order is wire order: encoding/json emits struct fields
// as declared, and this is also status --json's public surface, so
// reordering a field here moves the bytes of every note and of every
// golden that re-marshals one.
package record

import (
	"sort"
	"time"
)

// Record is a commit's verification record, stored as its git note
// under the verification notes ref: sha-keyed, local to this machine,
// read back by status.
//
// The record is born at mint rather than at first submit. That is what
// makes the per-subject facts — the portdir, the intent, the target a
// supersede decision compares — writable at all: a branch minted with
// --no-verify submits nothing, and a record that waited for a job
// would have nowhere to keep them.
//
// Two maps, not one. Jobs is keyed by release name: one guest per
// platform, shared by every subject in the change. Runs is keyed by
// RunKey(port, release): one verdict per subject per platform. The
// split is the whole reason for the shape — a shared guest is released
// once, when every run on it is terminal, so Released lives on the job
// and never on a run.
type Record struct {
	Schema int    `json:"schema"`
	Sha    string `json:"sha"`
	Tree   string `json:"tree"` // content identity: a message-only amend moves Sha, not Tree
	// Slug is the name the branch was minted under, written from the
	// plan that named it. It is recorded rather than read back out of
	// the branch name, because parsing a branch name is a guess and the
	// value that produced it is right here.
	Slug string `json:"slug,omitempty"`
	// Subjects are the members of the change, in build order.
	// Subjects[0] is the headline: the port the change is about, the one
	// a refusal names and the one the branch is named for. More than one
	// is a cohort.
	Subjects []Subject `json:"subjects,omitempty"`
	// Destination is how far this change's contract reaches, recorded
	// when it was minted rather than inferred later from what happens to
	// be running. A change bound for the branch alone is never drained:
	// nobody asked for a verdict, so the pump must not invent one.
	Destination Destination `json:"destination,omitempty"`
	// AskedBy is who asked for that destination. It is provenance and
	// never an input to any gate — the ladder's arithmetic counts human
	// and unattended promotions apart, and a field that could widen what
	// the machine is allowed to do would be an authorization.
	AskedBy Driver `json:"asked_by,omitempty"`
	// Agent is the AI agent marker, when one was set in the
	// environment. Provenance only, on the same terms as AskedBy:
	// recorded so a later question about how a change reached review can
	// be answered by a query instead of an estimate, and read by nothing
	// that decides anything.
	Agent string `json:"agent,omitempty"`
	// MintedVia says whether this change came from a deliberate single
	// target or from a sweep over many.
	MintedVia MintedVia `json:"minted_via,omitempty"`
	// Jobs are the environments this change was submitted to, keyed by
	// release name. One per platform, whatever the number of subjects.
	Jobs map[string]JobRecord `json:"jobs,omitempty"`
	// Runs are the verdicts, keyed by RunKey(port, release). A change
	// with one subject has one run per job; that the key already carries
	// the port at N==1 is deliberate, so the day a cohort lands nothing
	// has to re-key notes that already exist.
	Runs map[string]Run `json:"runs,omitempty"`
	// Hold is a person stopping this change from proceeding, with their
	// reason. A pointer because "not held" and "held for no stated
	// reason" are different facts.
	Hold *Hold `json:"hold,omitempty"`
	// Riders are the discovered todos folded into the change's own
	// commit — a modeline insertion and its kin. They are named here
	// because the pull request body vouches for what the note remembers
	// and not for what the diff can be re-read to contain.
	Riders []string `json:"riders,omitempty"`
	// Findings are what verification noticed that nobody asked about:
	// an ABI change, the dependents that would need a revision bump, an
	// instruction comment in the Portfile. A finding proposes and never
	// executes, which is what Disposition is for.
	Findings []Finding `json:"findings,omitempty"`
	// ClosesTicket is the ticket this change closes, carried to the
	// commit's trailer and the pull request's body.
	ClosesTicket string `json:"closes_ticket,omitempty"`
	// SupersededBy names the newer sibling's branch, written on the
	// older record when a port-keyed supersede takes its place. The
	// branch becomes its own end state until a person cleans it up: the
	// record still holds what was learned, and the field says why
	// nothing more will be learned.
	SupersededBy string `json:"superseded_by,omitempty"`
	// Base is the commit the change was minted on top of, with the time
	// that commit was made. Both halves are needed by different readers:
	// the sha is the honest "before" a baseline is measured at, and the
	// time is how a reader tells a change written against a week-old
	// tree from one written against today's.
	Base Base `json:"base,omitzero"`
	// Evidence names the tip whose runs this record's findings were
	// measured on, for a record that inherited them rather than earning
	// them — an extended cohort's second commit stands on the first
	// commit's verification.
	Evidence *Measured `json:"evidence,omitempty"`
}

// Hold is a person stopping a change, with the reason they gave.
type Hold struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Base is the commit a change was minted on top of.
type Base struct {
	Sha         string    `json:"sha"`
	CommittedAt time.Time `json:"committed_at"`
}

// Measured is where a record's evidence was earned, when it was not
// earned here: the tip whose runs a finding was measured on.
//
// The Go name is not the wire key. record.Evidence is already taken by
// the audit ref's own type — a published change's verified/unverified
// claim — and two exported Evidence in one package will not build. The
// field is Record.Evidence, the key stays "evidence", and only the type
// is spelled differently.
type Measured struct {
	From string `json:"from"`
}

// Headline is the subject the change is about: the port a refusal
// names, the one the branch is named for, and the one a cohort is
// built around. A record with no subjects has the zero Subject, which
// names no port — the same answer a caller gets from an empty record
// everywhere else.
func (r Record) Headline() Subject {
	if len(r.Subjects) == 0 {
		return Subject{}
	}
	return r.Subjects[0]
}

// Ports lists the change's ports in build order, headline first.
//
// It does not sort and it does not deduplicate. The order is the order
// a cohort must be built in, and a repeated port is a malformed record
// that a projection quietly collapsing it would hide.
func (r Record) Ports() []string {
	out := make([]string, 0, len(r.Subjects))
	for _, s := range r.Subjects {
		out = append(out, s.Port)
	}
	return out
}

// Portdirs lists the directories the change touched, in the same
// order, without repeats and without the empties.
//
// This one does deduplicate, because it feeds staging: the portdirs go
// into the environment ahead of its own ports tree, and staging one
// directory twice is at best wasted work. A subject that never
// recorded a portdir contributes nothing to stage, so it is skipped
// rather than staged as "".
func (r Record) Portdirs() []string {
	out := make([]string, 0, len(r.Subjects))
	seen := make(map[string]bool, len(r.Subjects))
	for _, s := range r.Subjects {
		if s.Portdir == "" || seen[s.Portdir] {
			continue
		}
		seen[s.Portdir] = true
		out = append(out, s.Portdir)
	}
	return out
}

// Platforms lists the releases this change was submitted to, sorted
// for stable rendering.
//
// It projects the jobs and never the runs. A job is one guest on one
// platform; a run is one subject's verdict on it, so a cohort of nine
// on two platforms has two platforms and eighteen runs. Reading the
// run keys would answer with the wrong number and the wrong words.
func (r Record) Platforms() []string {
	out := make([]string, 0, len(r.Jobs))
	for k := range r.Jobs {
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
// one run passed, and none failed. A port declining a platform
// (unsupported) does not block — that refusal is often the change
// working — but an unexplained failure does, because it is exactly the
// question review will ask.
//
// It is stated here, on the record, so the rule has one home; the
// verdict package presents it as a judgment rather than restating it.
// The clauses the later steps add — a hold stops a promotion, and so
// does a finding still proposed — belong with those steps' verbs, and
// are deliberately not smuggled in with the schema.
func (r Record) Promotable() bool {
	return r.AnyState(Passed) && !r.AnyState(Failed)
}
