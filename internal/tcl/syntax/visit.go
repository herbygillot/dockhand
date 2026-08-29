package syntax

import "github.com/herbygillot/dockhand/internal/text"

// SegmentVisitor handles every segment kind. Adding a segment kind to the
// package breaks every implementation at compile time, which is the point:
// consumers that must be total implement this instead of a type switch.
type SegmentVisitor[T any] interface {
	Literal(Literal) T
	VarSub(VarSub) T
	CmdSub(CmdSub) T
	Braced(Braced) T
	Quoted(Quoted) T
}

// VisitSegment dispatches s to the matching method of v.
func VisitSegment[T any](s Segment, v SegmentVisitor[T]) T {
	switch s := s.(type) {
	case Literal:
		return v.Literal(s)
	case VarSub:
		return v.VarSub(s)
	case CmdSub:
		return v.CmdSub(s)
	case Braced:
		return v.Braced(s)
	case Quoted:
		return v.Quoted(s)
	}
	panic("syntax: unknown segment kind") // unreachable: Segment is sealed
}

// ItemVisitor handles every item kind, with the same totality contract as
// SegmentVisitor.
type ItemVisitor[T any] interface {
	Command(Command) T
	Comment(Comment) T
}

// VisitItem dispatches it to the matching method of v.
func VisitItem[T any](it Item, v ItemVisitor[T]) T {
	switch it := it.(type) {
	case Command:
		return v.Command(it)
	case Comment:
		return v.Comment(it)
	}
	panic("syntax: unknown item kind") // unreachable: Item is sealed
}

// SpanOf returns the span of any item.
func SpanOf(it Item) text.Span {
	switch it := it.(type) {
	case Command:
		return it.Span
	case Comment:
		return it.Span
	}
	panic("syntax: unknown item kind")
}

// SegmentSpan returns the span of any segment.
func SegmentSpan(s Segment) text.Span {
	switch s := s.(type) {
	case Literal:
		return s.Span
	case VarSub:
		return s.Span
	case CmdSub:
		return s.Span
	case Braced:
		return s.Span
	case Quoted:
		return s.Span
	}
	panic("syntax: unknown segment kind")
}
