package syntax

import "github.com/herbygillot/dockhand/internal/textedit"

func SpanOf(it Item) textedit.Span {
	switch it := it.(type) {
	case Command:
		return it.Span
	case Comment:
		return it.Span
	}
	panic("syntax: unknown item kind")
}

func SegmentSpan(s Segment) textedit.Span {
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
