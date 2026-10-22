package term

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// wrapWriter wraps standard output's prose at the width (Width), each line
// wider than it broken at its spaces, the rest under a hanging indent two
// past the line's own (part 5, item 4: about 300 lines of the command
// tests ran past 100 columns). A table's line, its cells set apart by two
// spaces or more, isn't wrapped, nor a line with no space to break at, a
// path or a URL, nor one a terminal's escape codes draw, nor a log's or
// a document's (Unwrapped). A write that ends without a newline, as
// a prompt's, is written at once, and what ends its line isn't wrapped.
type wrapWriter struct {
	w     io.Writer
	width int
	// line is what's held of the line being written; partial is a line
	// already begun at once, which is written as it comes.
	line    []byte
	partial bool
}

// TableLine is a line whose cells are set apart by two spaces or more,
// which is set in columns rather than wrapped.
var TableLine = regexp.MustCompile(`\S {2,}\S`)

func (w *wrapWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			// A line not ended is written now, as a prompt is, and the
			// line it begins isn't wrapped.
			if err := w.flush(); err != nil {
				return 0, err
			}
			if _, err := w.w.Write(p); err != nil {
				return 0, err
			}
			w.partial = true
			return n, nil
		}
		line := p[:i]
		p = p[i+1:]
		if w.partial {
			w.partial = false
			if _, err := w.w.Write(append(line, '\n')); err != nil {
				return 0, err
			}
			continue
		}
		w.line = append(w.line, line...)
		// Write, not WriteString, which a writer may take another way.
		if _, err := w.w.Write([]byte(Wrapped(string(w.line), w.width) + "\n")); err != nil {
			return 0, err
		}
		w.line = w.line[:0]
	}
	return n, nil
}

// flush writes what's held of a line.
func (w *wrapWriter) flush() error {
	if len(w.line) == 0 {
		return nil
	}
	_, err := w.w.Write(w.line)
	w.line = w.line[:0]
	return err
}

// Wrapped is one line as the output's writer sets it at width: broken at
// its spaces where it's wider and has somewhere to break, its later lines
// two past its own indent; a table's line, or one with escape codes or a
// tab, as it is.
func Wrapped(line string, width int) string {
	if width <= 0 || utf8.RuneCountInString(line) <= width || strings.ContainsAny(line, "\t\x1b") {
		return line
	}
	text := strings.TrimLeft(line, " ")
	indent := len(line) - len(text)
	if TableLine.MatchString(strings.TrimRight(text, " ")) {
		return line
	}
	hang := strings.Repeat(" ", indent+2)
	var lines []string
	current := line[:indent]
	columns := indent
	started := false
	for _, word := range strings.Split(text, " ") {
		size := utf8.RuneCountInString(word)
		if started && columns+1+size > width {
			lines = append(lines, current)
			current, columns, started = hang, len(hang), false
		}
		if started {
			current += " "
			columns++
		}
		current += word
		columns += size
		started = true
	}
	return strings.Join(append(lines, current), "\n")
}

// Unwrapped turns the wrap off for the rest of the command line, whose
// output is a log or a document read line by line, as serve's log, a
// build's log, a diff, or text for pasting.
func (s Streams) Unwrapped() {
	if held, ok := s.Out.(*switchWriter); ok {
		if wrap, ok := held.w.(*wrapWriter); ok {
			wrap.width = 0
		}
	}
}
