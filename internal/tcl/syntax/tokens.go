package syntax

import "github.com/herbygillot/dockhand/internal/text"

// TokenRuns lists the runs of sibling tokens at every depth. A command
// contributes its words as one run. A braced word contributes the runs of
// its own commands when its body parses as a script, which is how a table
// held in a set or a subport's declarations are reached, and its list
// elements as one run otherwise. A quoted word contributes its inner text
// as a token. A command substitution contributes its commands' runs. A
// comment contributes nothing, at any depth, because a Portfile that writes
// a value in a comment does not write it.
//
// Tokens are spans of the source, so a caller can compare them, count them,
// and edit in place.
func (s *Script) TokenRuns(src []byte) [][]text.Span {
	var runs [][]text.Span
	var script func(*Script)
	var segments func([]Segment)
	var window func(text.Span)
	segments = func(segs []Segment) {
		for _, segment := range segs {
			switch value := segment.(type) {
			case CmdSub:
				script(value.Script)
			case Quoted:
				segments(value.Segments)
			}
		}
	}
	window = func(body text.Span) {
		if nested, errs := ParseScript(src, body); len(errs) == 0 {
			script(nested)
			return
		}
		elements, errs := SplitList(src, body)
		if len(errs) != 0 {
			return
		}
		var run []text.Span
		for _, element := range elements {
			if element.Len() >= 2 && src[element.Start] == '{' && src[element.End-1] == '}' {
				window(span(element.Start+1, element.End-1))
				continue
			}
			if element.Len() >= 2 && src[element.Start] == '"' && src[element.End-1] == '"' {
				element = span(element.Start+1, element.End-1)
			}
			run = append(run, element)
		}
		if len(run) > 0 {
			runs = append(runs, run)
		}
	}
	script = func(s *Script) {
		for _, item := range s.Items {
			cmd, ok := item.(Command)
			if !ok {
				continue
			}
			var run []text.Span
			for _, word := range cmd.Words {
				if !word.Expand && len(word.Segments) == 1 {
					switch segment := word.Segments[0].(type) {
					case Braced:
						window(segment.Body)
						continue
					case Quoted:
						run = append(run, word.Inner())
						segments(segment.Segments)
						continue
					}
				}
				run = append(run, word.Span)
				segments(word.Segments)
			}
			if len(run) > 0 {
				runs = append(runs, run)
			}
		}
	}
	script(s)
	return runs
}
