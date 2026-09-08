// Package plan holds the value that separates deciding from doing: a
// complete, inert description of an intended change — the byte edits,
// their provenance, the precondition hash, and the exact predicted
// delta. All intelligence is spent at plan time and captured here;
// realizing is deliberately dumb — verify preconditions, write bytes,
// re-evaluate, demand the predicted delta exactly.
//
// Under D21 a plan is internal interchange, never a user artifact: one
// intent's computation feeds every realization — committed onto a
// branch, applied in place, verified in a VM before it exists anywhere
// real, rendered for a human — without any of them re-deciding
// anything. The JSON rendering survives only as --plan's inspection
// output; nothing reads a plan back.
package plan

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// Format is the plan wire format version this build writes.
const Format = 2

// Change is one field's movement within an evaluation context.
type Change struct {
	Field string   `json:"field"`
	Old   []string `json:"old,omitempty"`
	New   []string `json:"new,omitempty"`
}

// ContextDelta is one evaluation context's movement: field changes, or
// the context appearing or disappearing entirely.
type ContextDelta struct {
	Subport  string   `json:"subport"`
	Variants string   `json:"variants,omitempty"`
	Added    bool     `json:"added,omitempty"`
	Removed  bool     `json:"removed,omitempty"`
	Changes  []Change `json:"changes,omitempty"`
}

// FileEdit is one whole file the plan rewrites beside the Portfile: a
// patch under files/ relocated onto the new source, today. Path is
// relative to the portdir ("files/patch-foo.diff"), slash-separated,
// and Content is the file's entire new bytes rather than a span edit
// over its old ones.
//
// Whole bytes and not spans, deliberately. The Portfile's edits are
// spans with a precondition hash because the Portfile is what the user
// may have touched between plan and realization, and a span edit is
// what lets the drift be named. A refreshed patch is derived from the
// fetched source and from the old patch's bytes in the base commit;
// the planner produced it complete, and a realizer that re-derived it
// from spans would be re-planning. Reason is the sentence a renderer
// and a pull request body say about it ("2 hunks moved").
//
// WHOLE BYTES STILL NEED A PRECONDITION, and for the whole of the
// overhaul this type had none. The plan's one precondition was
// PortfileSHA256, so a change whose Portfile was untouched at the base
// but whose patch file somebody had rewritten in between committed the
// planner's stale relocation straight over the newer file and passed
// every drift check on the way. The argument above is about who DERIVES
// the bytes, and it is right; it is not an argument about whether the
// bytes the plan was derived FROM are still there.
type FileEdit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Reason  string `json:"reason"`
	// Was is the hex sha256 of this file's bytes AS THE PLANNER READ
	// THEM, which is what makes the whole-file write checkable: the
	// realizer holds it against the base tree and refuses a plan derived
	// from bytes that are no longer there.
	//
	// EMPTY MEANS THE PLANNER FOUND NO SUCH FILE — a creation — and that
	// is a claim about the world, not a missing field. A realizer refuses
	// a creation over a file that exists, for the same reason it refuses
	// a rewrite of bytes that changed: both are the plan being about some
	// other state of this portdir. Rule 7 is satisfied by that second
	// refusal rather than by a third value: a producer that forgets to
	// set Was is claiming the file is new, and the first realization over
	// a real portdir says so instead of overwriting quietly.
	Was string `json:"was,omitempty"`
}

// Finding is something examining the port turned up that nobody asked
// about: an instruction comment telling whoever updates this port to
// bump something else, a check the planner could not make. It is what
// the planner SAW, not what the record KEEPS.
//
// It is plan's own type and not record's, so that plan — the value
// every intent produces and every realization consumes — depends on
// nothing durable. A planner runs with a Portfile and a parse tree in
// hand and no store, no ref and no repository; making it speak the
// note's shape would put the durable record underneath every planner
// and every test that builds a plan by hand, to buy nothing but a
// shared struct.
//
// What the note keeps beyond this is exactly what the note is for and
// the planner cannot know: WHEN the finding was made part of a change,
// and WHETHER anyone has answered it since. Both are stamped when the
// change is minted, which is the one place a plan-time finding becomes
// a durable one and therefore the one place a kind or a disposition
// this tree does not know can be refused.
type Finding struct {
	// Kind is what sort of finding this is, in the vocabulary the note
	// classifies by — "instruction-comment", "patches-unchecked".
	//
	// A plain string here and a typed enum in the record, deliberately.
	// The enum is the note's, because the note outlives the process that
	// wrote it and a kind a later build cannot classify is a kind nobody
	// should be able to write; a plan lives for one process. Stating the
	// kind as a word and converting it at the single stamping site keeps
	// the enum's one gate where it belongs instead of spreading the
	// note's type down into every planner.
	Kind string `json:"kind"`
	// Ports are the ports the finding is about — the context being
	// changed, for a finding read out of its Portfile.
	Ports []string `json:"ports,omitempty"`
	// Candidates are the ports the finding named, with what it said
	// about each.
	Candidates []Candidate `json:"candidates,omitempty"`
	// Criterion is the measurement in words a reader can check. The
	// mechanical criterion is necessary and never sufficient, so it is
	// stated rather than implied.
	Criterion string `json:"criterion,omitempty"`
	// Source and Quote are where a non-mechanical finding came from and
	// what it actually said — a comment in the Portfile, cited the way a
	// reader would cite it. A finding that cannot be traced back to its
	// words is an assertion.
	Source string `json:"source,omitempty"`
	Quote  string `json:"quote,omitempty"`
	// Disposition says whether this finding is a question or a
	// statement: Proposed for one a human still owes an answer to,
	// Accepted for one that opens with its own verdict and asks nothing.
	//
	// A planner states it because only the planner knows which it made.
	// A finding still proposed holds an unattended publication until
	// somebody answers it, so a statement that arrived proposed would
	// wait forever for an answer nobody can give; the distinction is
	// made where the sentence is written and carried, not re-derived
	// from the kind at the far end.
	//
	// Dismissed is absent from this end on purpose. Dismissal is a
	// person's answer to a finding that already exists, written onto the
	// record long after the plan is gone.
	Disposition string `json:"disposition,omitempty"`
}

// The two dispositions a plan-time finding can carry. They are the
// note's own words so the stamp at mint is a conversion rather than a
// translation table, and they are plain strings for the reason Kind is.
const (
	// Proposed is a finding nobody has answered yet: a question the
	// change carries until a person takes it up or says no.
	Proposed = "proposed"
	// Accepted is a finding that is already its own answer — the check
	// that could not be made, reported so a reader knows it was not
	// made. Nothing here is a question.
	Accepted = "accepted"
)

// Candidate is one port a finding named, whether or not the finding
// proposes doing anything to it.
//
// The ports named and passed over are recorded beside the ones put
// forward, because they are exactly what a reviewer must check by hand:
// a port left out is a decision, and a decision no reader can see is a
// decision nobody can disagree with.
type Candidate struct {
	Port string `json:"port"`
	// Reason is why this port is here, either way — in the words the
	// finding would use to a person.
	Reason string `json:"reason,omitempty"`
}

// Plan is the value. A plan carries its own identity: the intent that
// made it knows, at plan time, what the change is called — Port names
// the evaluation context, Slug is the change's short identity
// ("jq-1.8.2", "jq-checksums", "jq-rev1"), and Summary is its one-line
// description in the project's commit format. Realization composes
// from these — a branch is dockhand/<slug>, a commit message is the
// summary — instead of every realizer re-deriving names the planner
// already had.
// ClosesTicket is the one part of the identity that is not a name. It
// is carried here rather than folded into Summary because Summary is
// the commit's subject and a trailer is not a subject: the realizer
// composes the message from both, and the plan states which is which.
type Plan struct {
	Format         int         `json:"format"`
	Intent         string      `json:"intent"`
	Port           string      `json:"port"`
	Slug           string      `json:"slug"`
	Summary        string      `json:"summary"`
	ClosesTicket   string      `json:"closes_ticket,omitempty"`
	Portdir        string      `json:"portdir"`
	Subport        string      `json:"subport,omitempty"`
	PortfileSHA256 string      `json:"portfile_sha256"`
	Edits          []edit.Edit `json:"edits"`
	// Files are the whole files the plan rewrites beside the Portfile,
	// each at its portdir-relative path. They ride on the plan for the
	// same reason the edits do: the intent decided them, and every
	// realization — the commit, the diff, the gate's shadow — writes
	// them without deciding anything again. Materialize does not touch
	// them; it is the Portfile's precondition and the Portfile's edits,
	// and a file here arrives complete.
	//
	// Absent rather than null when there is none, on Riders' precedent,
	// so every plan document that predates them hashes to what it did.
	Files []FileEdit `json:"files,omitempty"`
	// Riders names the housekeeping rules whose edits are in Edits, in
	// the order the rules ran. It is the names and not the edits because
	// the edits are already here: what a note and a pull request body
	// need is the vocabulary — "modeline" — that a reader can look up.
	//
	// A plan with no riders omits the key, so every plan that predates
	// them hashes to exactly what it did.
	Riders []string `json:"riders,omitempty"`
	// Findings are what examining the port turned up that nobody asked
	// about — today, an instruction comment telling whoever updates this
	// port to bump something else, quoted verbatim.
	//
	// They ride on the plan because the realizer is what writes a note,
	// and a finding made at plan time has nowhere else to go: the
	// examination happens with the source and the parse tree in hand,
	// and the record is born at mint. Nothing here is a proposal the
	// plan acts on — a finding proposes and never executes — so a plan
	// carrying one produces exactly the same edits as one that does not.
	//
	// Absent rather than null when there is none, on Riders' precedent,
	// so every plan document that predates them hashes to what it did.
	Findings []Finding `json:"findings,omitempty"`
	// Predicted is the delta the shadow evaluation says these edits
	// produce, in canonical wire form.
	//
	// Absent rather than null when there is none. A housekeeping change
	// predicts nothing by construction — its witness is the double proof
	// itself — and it was the first plan document in the tree whose
	// `predicted` key was JSON null rather than a list, which breaks a
	// consumer iterating `.predicted[]` that never had to guard before.
	// An absent key says the same thing the witness sentence says, once,
	// and it is how Riders and ClosesTicket already behave.
	Predicted []ContextDelta `json:"predicted,omitempty"`
}

// Encode writes the plan as JSON, with the process's own exit status
// said inside it. Under D21 a plan is internal interchange; this
// rendering survives only as --plan's debugging output, and nothing
// reads one back.
//
// The twin is an argument rather than a field on Plan: how a process
// ended is the command line's fact, and a plan is the same value
// whether it is printed, committed or verified. The envelope embeds
// the plan, so the plan's own keys keep their order and the exit
// object lands last.
func (p *Plan) Encode(w io.Writer, exit exitcode.Twin) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		*Plan
		Exit exitcode.Twin `json:"exit"`
	}{Plan: p, Exit: exit})
}

// FromDelta renders an info.Delta in the plan's canonical wire form:
// contexts sorted by (subport, variants), added and removed contexts
// carried as one-sided changes. Canonical form is what makes prediction
// comparison a plain equality between wire values.
func FromDelta(d info.Delta) []ContextDelta {
	var out []ContextDelta
	for key, changes := range d.Changed {
		cd := ContextDelta{Subport: key.Subport, Variants: string(key.Variants)}
		for _, ch := range changes {
			cd.Changes = append(cd.Changes, Change{Field: ch.Field.String(), Old: ch.Old, New: ch.New})
		}
		out = append(out, cd)
	}
	for key, vals := range d.Added {
		cd := ContextDelta{Subport: key.Subport, Variants: string(key.Variants), Added: true}
		cd.Changes = oneSided(vals, false)
		out = append(out, cd)
	}
	for key, vals := range d.Removed {
		cd := ContextDelta{Subport: key.Subport, Variants: string(key.Variants), Removed: true}
		cd.Changes = oneSided(vals, true)
		out = append(out, cd)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subport != out[j].Subport {
			return out[i].Subport < out[j].Subport
		}
		return out[i].Variants < out[j].Variants
	})
	for i := range out {
		sort.Slice(out[i].Changes, func(a, b int) bool {
			return out[i].Changes[a].Field < out[i].Changes[b].Field
		})
	}
	return out
}

// oneSided renders a context's whole Values as changes with only one
// side populated: the new side for an added context, the old side for a
// removed one.
func oneSided(vals info.Values, removed bool) []Change {
	before, after := info.Values{}, vals
	if removed {
		before, after = vals, info.Values{}
	}
	var changes []Change
	for _, ch := range info.ChangesBetween(before, after) {
		changes = append(changes, Change{Field: ch.Field.String(), Old: ch.Old, New: ch.New})
	}
	return changes
}
