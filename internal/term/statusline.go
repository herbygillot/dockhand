package term

import (
	"fmt"
	"io"
	"sync"
)

// StatusLine is standard error with at most one line redrawn in place, as
// outdated redraws its count. A line printed while it shows clears it
// first and draws it again after, so the two never share a line.
type StatusLine struct {
	mu    sync.Mutex
	w     io.Writer
	shown string
}

// Show draws text in place of what the line showed.
func (s *StatusLine) Show(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
