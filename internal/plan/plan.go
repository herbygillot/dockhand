// Package plan holds the artifact a planner emits and apply consumes: a
// complete, inert description of an intended change — the byte edits,
// their provenance, and the exact predicted delta. All intelligence is
// spent at plan time and captured here; applying is deliberately dumb —
// verify preconditions, write bytes, re-evaluate, demand the predicted
// delta exactly.
//
// Plans serialize as JSON because they travel a pipe
// (dockhand bump ... | dockhand apply -) and outlive the process that
// made them.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

// Format is the plan wire format version this build writes.
const Format = 1

// Edit is one byte-span replacement, with enough provenance to render
// and to double-check against the file at apply time.
type Edit struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`
}

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

// Plan is the artifact.
type Plan struct {
	Format         int            `json:"format"`
	Intent         string         `json:"intent"`
	Portdir        string         `json:"portdir"`
	Subport        string         `json:"subport,omitempty"`
	PortfileSHA256 string         `json:"portfile_sha256"`
	Edits          []Edit         `json:"edits"`
	Predicted      []ContextDelta `json:"predicted"`
}

// Encode writes the plan as JSON. Under D21 a plan is internal
// interchange; this rendering survives only as --plan's debugging
// output, and nothing reads one back.
func (p *Plan) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// FileSHA256 is the precondition hash: the hex sha256 of the Portfile
// bytes a plan was computed against.
func FileSHA256(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
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
