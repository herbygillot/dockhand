package text

import (
	"fmt"
	"sort"
)

type Edit struct {
	Span Span
	New  []byte
}

type EditErrorType int

const (
	ReversedSpan EditErrorType = iota

	OutOfBounds

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

type EditError struct {
	Type EditErrorType
	Edit Edit
}

func (e EditError) Error() string {
	return fmt.Sprintf("%s at [%d,%d)", e.Type, e.Edit.Span.Start, e.Edit.Span.End)
}

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
