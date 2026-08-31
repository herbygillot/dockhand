// Package edit holds the byte-span edit with provenance — the currency
// every edit-producing component speaks. It sits between text, which
// applies raw spans, and plan, which composes edits into an intent's
// complete description: utilities that compute edits import this
// package, never the planning layer above it, so the dependency arrow
// always points from workflow down to primitive.
package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/herbygillot/dockhand/internal/text"
)

// Edit is one byte-span replacement, with enough provenance to render
// and to double-check against the file at realization time.
type Edit struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`
}

// Apply returns src with the edits applied, verifying each edit's
// recorded Old against the bytes it claims to replace. Every path that
// realizes edits comes through here — a planner shadowing its own edit
// set as much as a plan being carried out — so an edit whose Old
// disagrees with its span cannot survive planning: it fails in the
// planner's shadow, as the planner's bug, instead of surfacing at
// realization time as a false drift blamed on the user's tree.
func Apply(src []byte, edits []Edit) ([]byte, error) {
	tedits := make([]text.Edit, 0, len(edits))
	for _, e := range edits {
		span := text.Span{Start: e.Start, End: e.End}
		if e.End > len(src) || e.Start < 0 || span.Text(src) != e.Old {
			return nil, fmt.Errorf("edit at %d..%d: recorded old text does not match the source", e.Start, e.End)
		}
		tedits = append(tedits, text.Edit{Span: span, New: []byte(e.New)})
	}
	return text.Apply(src, tedits)
}

// FileSHA256 is the precondition hash: the hex sha256 of the file
// bytes an edit set was computed against.
func FileSHA256(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}
