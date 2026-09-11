package syntax

import (
	"iter"

	"github.com/herbygillot/dockhand/v2/internal/text"
)

func SpanOf(it Item) text.Span {
	switch it := it.(type) {
	case Command:
		return it.Span
	case Comment:
		return it.Span
	}
	panic("syntax: unknown item kind")
}

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

func (s *Script) Commands(src []byte, descend func(Command) bool) iter.Seq[Command] {
	return func(yield func(Command) bool) {
		walkCommands(src, s, descend, yield)
	}
}

func walkCommands(src []byte, s *Script, descend func(Command) bool, yield func(Command) bool) bool {
	for _, it := range s.Items {
		cmd, ok := it.(Command)
		if !ok {
			continue
		}
		if !yield(cmd) {
			return false
		}
		if !descend(cmd) {
			continue
		}
		for _, w := range cmd.Words[1:] {
			if body, ok := w.BracedScript(src); ok {
				if !walkCommands(src, body, descend, yield) {
					return false
				}
			}
		}
	}
	return true
}
