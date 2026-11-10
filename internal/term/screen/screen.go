// Package screen is the terminal a view draws on whole, as dockhand
// console does: the alternate screen, its input read in raw mode as keys,
// and a frame drawn in one write at the terminal's size. It knows nothing
// of what is drawn; the frame is the view's, a line each.
package screen

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/term"
)

// The terminal's own sequences: the alternate screen with the cursor
// hidden, and back.
const (
	enterView = "\x1b[?1049h\x1b[?25l"
	leaveView = "\x1b[?25h\x1b[?1049l"
	// clearView clears what the terminal shows and puts the cursor at its
	// top, for a command the screen is suspended for.
	clearView = "\x1b[H\x1b[2J"
)

// defaultHeight is the rows a frame has where the output isn't a terminal,
// as a test's stand-in for one.
const defaultHeight = 24

// escapeWait is how long an ESC waits for the rest of a sequence before
// it's the Escape key alone.
const escapeWait = 50 * time.Millisecond

// Line is one line of a frame: its text, and whether it's drawn reversed,
// as a selected row is.
type Line struct {
	Text    string
	Reverse bool
}

// Screen is the terminal while a view is drawn on it.
type Screen struct {
	streams term.Streams
	in      io.Reader
	out     io.Writer
	// fd is the input's descriptor where it's a terminal, set raw while
	// the screen is open, and saved its modes before; -1 where it isn't,
	// as a test's pipe.
	fd    int
	saved *unix.Termios
	keys  chan Key
	gate  gate
	done  chan struct{}
	once  sync.Once
}

// Open enters the alternate screen, and raw mode where the input is a
// terminal, and reads the input as keys. The caller defers Close, which
// gives the terminal back as it was, after a panic too.
func Open(streams term.Streams) (*Screen, error) {
	in, out := streams.Beneath()
	s := &Screen{streams: streams, in: in, out: out, fd: -1, keys: make(chan Key, 16), done: make(chan struct{})}
	s.gate.cond = sync.NewCond(&s.gate.mu)
	if file, ok := in.(*os.File); ok {
		if modes, err := unix.IoctlGetTermios(int(file.Fd()), getModes); err == nil {
			s.fd, s.saved = int(file.Fd()), modes
		}
	}
	if err := s.raw(); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(s.out, enterView); err != nil {
		return nil, errors.Join(err, s.restore())
	}
	chunks := make(chan []byte)
	go s.read(chunks)
	go s.decode(chunks)
	return s, nil
}

// Close leaves the alternate screen and gives the terminal its modes back.
// Closing again does nothing.
func (s *Screen) Close() error {
	var err error
	s.once.Do(func() {
		close(s.done)
		s.gate.stop(s.fd >= 0)
		_, err = io.WriteString(s.out, leaveView)
		err = errors.Join(err, s.restore())
	})
	return err
}

// Keys are the keys pressed, until the input ends or the screen closes.
func (s *Screen) Keys() <-chan Key { return s.keys }

// Size is the terminal's columns and rows, read again each time, so a
// resize is seen at the next frame.
func (s *Screen) Size() (width, height int) {
	width, height = s.streams.Width(), s.streams.Height()
	if height <= 0 {
		height = defaultHeight
	}
	return width, height
}

// Draw writes a frame at the screen's size, in one write.
func (s *Screen) Draw(frame []Line) error {
	width, height := s.Size()
	_, err := s.out.Write(Render(frame, width, height))
	return err
}

// Render is a frame as the bytes Draw writes: from the top left, each line
// cut or padded to width, as many as height takes, and the rest of the
// screen erased, so nothing of the last frame is left and nothing flickers.
func Render(frame []Line, width, height int) []byte {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, line := range frame[:min(len(frame), height)] {
		if i > 0 {
			b.WriteString("\r\n")
		}
		if line.Reverse {
			b.WriteString("\x1b[7m")
		}
		b.WriteString(Fit(line.Text, width))
		if line.Reverse {
			b.WriteString("\x1b[0m")
		}
	}
	b.WriteString("\x1b[J")
	return []byte(b.String())
}

// Fit is text exactly width runes wide: cut, or padded with spaces, a
// control character in it a space, which would move the cursor.
func Fit(text string, width int) string {
	runes := make([]rune, 0, width)
	for _, r := range text {
		if len(runes) == width {
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		runes = append(runes, r)
	}
	return string(runes) + strings.Repeat(" ", max(width-len(runes), 0))
}

// Suspend leaves the screen and gives the terminal back as it was, runs
// run, as a command whose output the person reads, and then, where prompt
// isn't empty, says it and waits for a key before drawing on the screen
// again. Keys typed while run runs are its own, never the screen's. The
// terminal is cleared for it: left as it was, the command's output began
// over the table's rows, in Ghostty at least (the person's console sitting,
// 2026-11-10, dockhand logs).
func (s *Screen) Suspend(ctx context.Context, run func(), prompt string) error {
	s.gate.pause(s.fd >= 0)
	defer s.gate.resume()
	if _, err := io.WriteString(s.out, leaveView+clearView); err != nil {
		return err
	}
	if err := s.restore(); err != nil {
		return err
	}
	run()
	if err := s.raw(); err != nil {
		return err
	}
	// What was pressed before run is the screen's no longer.
	for len(s.keys) > 0 {
		<-s.keys
	}
	if prompt != "" {
		if _, err := io.WriteString(s.out, "\r\n"+prompt); err != nil {
			return err
		}
		s.gate.resume()
		select {
		case <-s.keys:
		case <-ctx.Done():
		}
	}
	_, err := io.WriteString(s.out, enterView)
	return err
}

// raw sets the input terminal's raw mode: each byte read as it's typed,
// not echoed, Ctrl-C a byte rather than a signal, and a read that waits
// at most a tenth of a second, so the reader can stop between reads.
func (s *Screen) raw() error {
	if s.fd < 0 {
		return nil
	}
	modes := *s.saved
	modes.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	modes.Oflag &^= unix.OPOST
	modes.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	modes.Cflag &^= unix.CSIZE | unix.PARENB
	modes.Cflag |= unix.CS8
	modes.Cc[unix.VMIN], modes.Cc[unix.VTIME] = 0, 1
	return unix.IoctlSetTermios(s.fd, setModes, &modes)
}

// restore gives the input terminal the modes it had before Open.
func (s *Screen) restore() error {
	if s.fd < 0 {
		return nil
	}
	return unix.IoctlSetTermios(s.fd, setModes, s.saved)
}

// read reads the input while the screen isn't suspended, until it ends or
// the screen closes.
func (s *Screen) read(chunks chan<- []byte) {
	defer close(chunks)
	defer s.gate.gone()
	if s.in == nil {
		return
	}
	buf := make([]byte, 256)
	for s.gate.pass() {
		var n int
		var err error
		if s.fd >= 0 {
			// A raw read that waited its tenth of a second reads nothing,
			// which isn't the input's end.
			n, err = unix.Read(s.fd, buf)
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
		} else {
			n, err = s.in.Read(buf)
		}
		if n > 0 {
			select {
			case chunks <- append([]byte{}, buf[:n]...):
			case <-s.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// decode turns what read reads into keys, an ESC alone the Escape key once
// nothing follows it for escapeWait.
func (s *Screen) decode(chunks <-chan []byte) {
	defer close(s.keys)
	var d decoder
	var quiet <-chan time.Time
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				s.send(d.flush())
				return
			}
			if !s.send(d.feed(chunk)) {
				return
			}
			quiet = nil
			if len(d.pending) > 0 {
				quiet = time.After(escapeWait)
			}
		case <-quiet:
			quiet = nil
			if !s.send(d.flush()) {
				return
			}
		case <-s.done:
			return
		}
	}
}

// send passes keys on, reporting false once the screen is closed.
func (s *Screen) send(keys []Key) bool {
	for _, key := range keys {
		select {
		case s.keys <- key:
		case <-s.done:
			return false
		}
	}
	return true
}

// gate holds the reader while the screen is suspended, so a command run
// meanwhile reads its own input.
type gate struct {
	mu   sync.Mutex
	cond *sync.Cond
	// paused is the screen suspended, and stopped closed; parked is the
	// reader waiting at the gate, and ended the reader gone.
	paused, stopped, parked, ended bool
}

// pass waits while the screen is suspended, and reports whether to read
// on.
func (g *gate) pass() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.parked = true
	g.cond.Broadcast()
	for g.paused && !g.stopped {
		g.cond.Wait()
	}
	g.parked = false
	return !g.stopped
}

// pause holds the reader at its next pass, and with wait, waits for it to
// get there: a terminal's raw reads end within a tenth of a second, where
// a pipe's may never.
func (g *gate) pause(wait bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = true
	for wait && !g.parked && !g.ended {
		g.cond.Wait()
	}
}

func (g *gate) resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = false
	g.cond.Broadcast()
}

// stop ends the reader at its next pass, and with wait, waits for it to
// end.
func (g *gate) stop(wait bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	g.cond.Broadcast()
	for wait && !g.ended {
		g.cond.Wait()
	}
}

func (g *gate) gone() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ended = true
	g.cond.Broadcast()
}
