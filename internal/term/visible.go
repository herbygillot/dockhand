package term

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Visible is text that is someone else's, a pull request's diff, paths,
// title or commit subject, made safe to write to a terminal: every control
// character but newline and tab is shown in a visible form, so that a
// carriage return, an escape sequence or a backspace in it can't draw over
// what it is shown beside (#330). A C0 control and DEL show as their Unicode
// control pictures (␍, ␛, ␡), a C1 control as <U+0085>, and a byte that
// isn't UTF-8 as <0x9B>, since a terminal not reading UTF-8 takes some of
// those for C1 controls. The bidirectional formatting characters show as a
// C1 control does, <U+202E>: an override can show a Portfile line in
// another order than Tcl reads it. Text with none of these comes back as
// it was.
func Visible(text string) string {
	if !needsVisible(text) {
		return text
	}
	var b strings.Builder
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "<0x%02X>", text[0])
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20:
			b.WriteRune(0x2400 + r)
		case r == 0x7f:
			b.WriteRune('␡')
		case r >= 0x80 && r <= 0x9f, bidi(r):
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteString(text[:size])
		}
		text = text[size:]
	}
	return b.String()
}

// needsVisible says whether Visible has anything to change in text.
func needsVisible(text string) bool {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if (r == utf8.RuneError && size == 1) || (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) || bidi(r) {
			return true
		}
		i += size
	}
	return false
}

// bidi reports whether r is a bidirectional formatting character: the
// embeddings and overrides, the isolates, and the marks.
func bidi(r rune) bool {
	return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f || r == 0x061c
}
