package text

import (
	"fmt"
	"sort"
)

// Edit replaces the bytes of Span with New. A zero-length span is an
// insertion at Span.Start; empty New is a deletion; a span covering the
// whole buffer with new content is a wholesale rewrite (the template shape).
type Edit struct {
	Span Span
	New  []byte
}

// EditErrorType classifies why an edit list was refused.
type EditErrorType int

const (
	// ReversedSpan is an edit whose span has End before Start.
	ReversedSpan EditErrorType = iota
	// OutOfBounds is an edit whose span exceeds the source buffer.
	OutOfBounds
	// Overlap is a pair of edits whose spans intersect, including two
	// insertions at the same offset, whose order would be ambiguous.
	Overlap
)

func (t EditErrorType) String() string {
	switch t {
	case ReversedSpan:
		return "reversed span"
	case OutOfBounds:
		return "span out of bounds"
	case Overlap:
		return "overlapping edits"
	}
	return "unknown edit error"
}

// EditError reports a refused edit list. Edits come from recognizers, so an
// invalid list is a bug upstream, never a condition to repair here: Apply
// refuses loudly rather than guessing an order.
type EditError struct {
	Type EditErrorType
	Edit Edit
}

// Error implements the error interface.
func (e EditError) Error() string {
	return fmt.Sprintf("%s at [%d,%d)", e.Type, e.Edit.Span.Start, e.Edit.Span.End)
}

// Apply returns a new buffer with all edits applied to src. Bytes outside
// the edited spans are copied through untouched — minimality is a property
// of the construction, not a post-hoc check. The input slices are never
// modified. An empty edit list returns src's bytes unchanged.
//
// Edits are ordered by (Start, End), which makes one boundary case
// deterministic rather than order-dependent: an insertion at another edit's
// start offset lands before that edit's replacement text. Two insertions at
// the same offset have no defensible order and are refused as overlapping.
func Apply(src []byte, edits []Edit) ([]byte, error) {
	sorted := make([]Edit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Span.Start != sorted[j].Span.Start {
			return sorted[i].Span.Start < sorted[j].Span.Start
		}
		return sorted[i].Span.End < sorted[j].Span.End
	})

	grow := 0
	for i, e := range sorted {
		switch {
		case e.Span.End < e.Span.Start:
			return nil, EditError{ReversedSpan, e}
		case e.Span.Start < 0 || e.Span.End > len(src):
			return nil, EditError{OutOfBounds, e}
		}
		if i > 0 {
			prev := sorted[i-1]
			if e.Span.Start < prev.Span.End ||
				(e.Span == prev.Span && e.Span.Len() == 0) {
				return nil, EditError{Overlap, e}
			}
		}
		grow += len(e.New) - e.Span.Len()
	}

	out := make([]byte, 0, len(src)+grow)
	pos := 0
	for _, e := range sorted {
		out = append(out, src[pos:e.Span.Start]...)
		out = append(out, e.New...)
		pos = e.Span.End
	}
	out = append(out, src[pos:]...)
	return out, nil
}
