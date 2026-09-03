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
	// Riders names the housekeeping rules whose edits are in Edits, in
	// the order the rules ran. It is the names and not the edits because
	// the edits are already here: what a note and a pull request body
	// need is the vocabulary — "modeline" — that a reader can look up.
	//
	// A plan with no riders omits the key, so every plan that predates
	// them hashes to exactly what it did.
	Riders []string `json:"riders,omitempty"`
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
