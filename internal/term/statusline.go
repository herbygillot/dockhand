package term

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// StatusLine is standard error with at most one line redrawn in place, as
// outdated redraws its count. A line printed while it shows clears it
// first and draws it again after, so the two never share a line.
type StatusLine struct {
	mu sync.Mutex
	w  io.Writer
	// columns is the terminal's width the line is cut short of, 0 where
	// it isn't known, off a terminal, and nothing is cut.
	columns func() int
	shown   string
}

// Show draws text in place of what the line showed, cut short of the
// terminal's width with "…": a line that wrapped left its first row
// behind at each redraw, since the clear reaches only the cursor's row,
// and a check's progress read as one long line of every step (#332's try).
func (s *StatusLine) Show(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.columns != nil {
		if width := s.columns(); width > 1 && utf8.RuneCountInString(text) >= width {
			text = strings.TrimRight(string([]rune(text)[:width-2]), " ") + "…"
		}
	}
	fmt.Fprint(s.w, "\r\033[K"+text)
	s.shown = text
}

// Clear removes the line, if one shows.
func (s *StatusLine) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown != "" {
		fmt.Fprint(s.w, "\r\033[K")
		s.shown = ""
	}
}

// Say prints a line of its own, above the line shown.
func (s *StatusLine) Say(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown != "" {
		fmt.Fprint(s.w, "\r\033[K")
	}
	fmt.Fprintln(s.w, line)
	if s.shown != "" {
		fmt.Fprint(s.w, s.shown)
	}
}
