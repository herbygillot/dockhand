package command

import (
	"fmt"
	"io"
	"sync"
)

// statusLine is standard error with at most one line redrawn in place, as
// outdated redraws its count. A line printed while it shows clears it
// first and draws it again after, so the two never share a line.
type statusLine struct {
	mu    sync.Mutex
	w     io.Writer
	shown string
}

// show draws text in place of what the line showed.
func (s *statusLine) show(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprint(s.w, "\r\033[K"+text)
	s.shown = text
}

// clear removes the line, if one shows.
func (s *statusLine) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown != "" {
		fmt.Fprint(s.w, "\r\033[K")
		s.shown = ""
	}
}

// say prints a line of its own, above the line shown.
func (s *statusLine) say(line string) {
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
