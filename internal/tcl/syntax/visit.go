package syntax

import (
	"iter"
)

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
