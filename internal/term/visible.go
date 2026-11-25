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
// those for C1 controls. Text with none of these comes back as it was.
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
		case r >= 0x80 && r <= 0x9f:
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
		if (r == utf8.RuneError && size == 1) || (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
			return true
		}
		i += size
	}
	return false
}
