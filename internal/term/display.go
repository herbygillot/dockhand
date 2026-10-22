package term

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Display is how a command line's text is shown, as its environment asks:
// whether the marks are coloured, and the columns text wraps at where
// output isn't a terminal. The command line reads them from the
// environment (NO_COLOR, COLUMNS), as nothing below it does.
type Display struct {
	// NoColour is NO_COLOR set, or status --watch's --plain.
	NoColour bool
	// Columns is COLUMNS, where it's set; prose off a terminal wraps at it,
	// else at 100, since field testing reads dockhand through a pipe.
	Columns int
}

// WithDisplay are the streams with how their text is shown. On a terminal,
// and unless NoColour, the marks standard output prints are coloured (the
// person's "Marks only", 2026-10-22): ✗ red, ! yellow, ✓ green, and nothing
// else, no meaning resting on colour alone. Begin wraps the output.
func (s Streams) WithDisplay(display Display) Streams {
	s.display = display
	return s
}

// Plain are the streams with their marks uncoloured, for status --watch's
// --plain: line output a script or a log reads.
func (s Streams) Plain() Streams {
	s.display.NoColour = true
	if w, ok := s.Out.(*switchWriter); ok {
		if marks, ok := w.w.(*markWriter); ok {
			marks.plain = true
		}
	}
	return s
}

// colourMarks reports whether standard output's marks are coloured: a
// real terminal's, never a test's stand-in for one (AsTerminal), whose
// text is read as words.
func (s Streams) colourMarks() bool {
	file, ok := s.Out.(*os.File)
	return !s.display.NoColour && !s.JSON() && ok && isTerminal(file.Fd())
}

// defaultColumns is the width prose off a terminal wraps at, where
// COLUMNS isn't set.
const defaultColumns = 100

// The marks' colours, as ANSI's SGR codes.
const (
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	green  = "\x1b[32m"
	reset  = "\x1b[0m"
)

// markWriter colours the marks text writes: ✗ and ✓ wherever they are,
// which nothing else prints, and ! where it's a mark, first on its line
// after any indent, or after two spaces as in a table's cell, and before a
// space.
type markWriter struct {
	w io.Writer
	// plain writes the marks uncoloured (Plain).
	plain bool
	// line is the text of the line written so far, and pending a rune a
	// write split, kept for the next.
	line, pending []byte
}

func (m *markWriter) Write(p []byte) (int, error) {
	n := len(p)
	p = append(m.pending, p...)
	m.pending = nil
	var out bytes.Buffer
	for len(p) > 0 {
		if !utf8.FullRune(p) {
			m.pending = append(m.pending, p...)
			break
		}
		r, size := utf8.DecodeRune(p)
		chunk := p[:size]
		p = p[size:]
		switch {
		case m.plain:
			out.Write(chunk)
		case r == '✗':
			out.WriteString(red + "✗" + reset)
		case r == '✓':
			out.WriteString(green + "✓" + reset)
		case r == '!' && m.markPlace() && (len(p) == 0 || p[0] == ' '):
			out.WriteString(yellow + "!" + reset)
		default:
			out.Write(chunk)
		}
		if r == '\n' {
			m.line = m.line[:0]
		} else {
			m.line = append(m.line, chunk...)
		}
	}
	if _, err := m.w.Write(out.Bytes()); err != nil {
		return 0, err
	}
	return n, nil
}

// markPlace reports whether what's written of the line so far leaves a
// mark's place: nothing but spaces, or ending in two.
func (m *markWriter) markPlace() bool {
	line := string(m.line)
	return strings.TrimLeft(line, " ") == "" || strings.HasSuffix(line, "  ")
}

// Hanging is text wrapped at the output's width (Width), its first line
// after first and the rest after a hanging indent as wide as first, as
// status's attention list wraps its rows (2026-10-20): one writer for
// prose, where only that list wrapped, and lines ran to 400 columns.
// A line is never cut; a word wider than the width overruns it.
func (s Streams) Hanging(first, text string) string {
	indent := utf8.RuneCountInString(first)
	return first + breakAt(text, indent, max(s.Width(), indent+20), strings.Repeat(" ", indent))
}
