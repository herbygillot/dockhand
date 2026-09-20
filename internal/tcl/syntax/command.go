package syntax

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/text"
)

// Direct lists the script's own commands, without descending into bodies.
func (s *Script) Direct() []Command {
	var commands []Command
	for _, item := range s.Items {
		if command, ok := item.(Command); ok {
			commands = append(commands, command)
		}
	}
	return commands
}

// Is reports whether the command is exactly these words, each spelled as
// given and none expanded.
func (c Command) Is(src []byte, words ...string) bool {
	if len(c.Words) != len(words) {
		return false
	}
	for i, word := range c.Words {
		if word.Expand || word.Span.Text(src) != words[i] {
			return false
		}
	}
	return true
}

// LiteralArgs returns the command's arguments when every one is a bare
// literal, and false when any is quoted, braced, substituted, or expanded.
func (c Command) LiteralArgs(src []byte) ([]string, bool) {
	var values []string
	for _, word := range c.Words[1:] {
		value, ok := word.Literal(src)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// Plain reports whether the word is text and simple variable substitutions
// only: no command substitution, no indexed variable, no expansion. It is
// what a diagnostic message may safely be made of.
func (w Word) Plain() bool { return !w.Expand && plainSegments(w.Segments) }

func plainSegments(segments []Segment) bool {
	for _, segment := range segments {
		switch value := segment.(type) {
		case Literal, Braced:
		case VarSub:
			if value.HasIndex {
				return false
			}
		case Quoted:
			if !plainSegments(value.Segments) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Inner is the span of the word's content inside one layer of braces or
// quotes when the word is exactly one braced or quoted segment, and the
// word's own span otherwise.
func (w Word) Inner() text.Span {
	if w.Expand || len(w.Segments) != 1 {
		return w.Span
	}
	switch segment := w.Segments[0].(type) {
	case Braced:
		return segment.Body
	case Quoted:
		return text.Span{Start: segment.Span.Start + 1, End: segment.Span.End - 1}
	}
	return w.Span
}

// Control separates a control structure's selecting words, its conditions,
// iteration words, options, patterns, and the value a switch inspects, from
// the bodies it executes as scripts. Other commands, and an if left waiting
// for a condition, report ok false.
//
// A switch's bodies come from inside its braced list when it has one, so
// they are not among the command's own words, but their spans are the
// source's like any other. A "-" body falls through to the next and is not
// a body.
func (c Command) Control(src []byte) (controls, bodies []Word, ok bool) {
	name, _ := c.Name(src)
	words := c.Words[1:]
	switch {
	case name == "if":
		expectCondition := true
		for _, word := range words {
			literal, _ := word.Literal(src)
			switch {
			case expectCondition:
				controls = append(controls, word)
				expectCondition = false
			case literal == "then" || literal == "else":
			case literal == "elseif":
				expectCondition = true
			default:
				bodies = append(bodies, word)
			}
		}
		if expectCondition {
			return nil, nil, false
		}
	case (name == "foreach" || name == "while") && len(words) > 1:
		controls, bodies = words[:len(words)-1], words[len(words)-1:]
	case name == "catch" && len(words) > 0:
		bodies = words[:1]
	case name == "for" && len(words) == 4:
		controls, bodies = words[1:2], []Word{words[0], words[2], words[3]}
	case name == "switch":
		i := 0
		for i < len(words) {
			literal, isLiteral := words[i].Literal(src)
			if !isLiteral || !strings.HasPrefix(literal, "-") {
				break
			}
			controls = append(controls, words[i])
			i++
			if literal == "--" {
				break
			}
		}
		if i >= len(words) {
			return nil, nil, false
		}
		controls = append(controls, words[i])
		arms := words[i+1:]
		if len(arms) == 1 {
			list, isList := arms[0].BracedScript(src)
			if !isList {
				return nil, nil, false
			}
			arms = nil
			for _, item := range list.Items {
				if arm, isCommand := item.(Command); isCommand {
					arms = append(arms, arm.Words...)
				}
			}
		}
		for j, word := range arms {
			if j%2 == 0 {
				controls = append(controls, word)
			} else if literal, _ := word.Literal(src); literal != "-" {
				bodies = append(bodies, word)
			}
		}
	default:
		return nil, nil, false
	}
	return controls, bodies, true
}
