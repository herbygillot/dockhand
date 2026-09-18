package portfile

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/text"
)

// UniqueLiteral finds the one place the Portfile writes value as a whole
// token: a word or list element bounded by whitespace, braces, quotes, or a
// line continuation, outside comment lines. Two occurrences, or none, mean
// the value has no owner that can be edited with confidence.
func UniqueLiteral(src []byte, value string) (text.Span, bool) {
	var found []text.Span
	source := string(src)
	for offset := 0; ; {
		at := strings.Index(source[offset:], value)
		if at < 0 {
			break
		}
		start := offset + at
		end := start + len(value)
		offset = start + 1
		if start > 0 && !boundary(source[start-1]) || end < len(source) && !boundary(source[end]) {
			continue
		}
		if inComment(source, start) {
			continue
		}
		found = append(found, text.Span{Start: start, End: end})
	}
	if len(found) != 1 {
		return text.Span{}, false
	}
	return found[0], true
}

func boundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '{' || c == '}' || c == '"' || c == '\\'
}

// inComment reports whether the byte at offset sits on a line whose first
// non-blank character is #.
func inComment(text string, offset int) bool {
	line := strings.LastIndexByte(text[:offset], '\n') + 1
	for i := line; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t':
			continue
		case '#':
			return true
		default:
			return false
		}
	}
	return false
}
