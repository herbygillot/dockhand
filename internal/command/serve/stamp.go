package serve

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// stampWriter starts each line written through it with the time it began,
// in RFC 3339 to the second with its zone, "2026-11-01T08:25:16-05:00 ",
// where serve's output is a log file rather than a terminal: a log read
// days later says when each line was written (the M1's serve log,
// prioritization's placement of 2026-11-01).
type stampWriter struct {
	mu sync.Mutex
	w  io.Writer
	// now is the clock the time is read from.
	now func() time.Time
	// mid is a line begun and not yet ended, whose time is written.
	mid bool
}

func (s *stampWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b bytes.Buffer
	for rest := p; len(rest) > 0; {
		if !s.mid {
			b.WriteString(s.now().Format(time.RFC3339))
			b.WriteByte(' ')
			s.mid = true
		}
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, s.mid = rest[:i+1], false
		}
		b.Write(line)
		rest = rest[len(line):]
	}
	if _, err := s.w.Write(b.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}
