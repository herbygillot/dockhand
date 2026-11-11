package term

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Streams are the standard streams a command reads and writes. Out is
// standard output raw, beneath it only --json's switch, for what comes
// from a file, a tool or a template: a Portfile, a diff, a log, a
// completion script, a usage page, a Markdown body. Dockhand's own
// sentences, tables and views write to Prose, which wraps at the width
// and colours the marks. The name typed by habit is the safe one: a
// payload written to Out by mistake for Prose costs a long line, where a
// wrapped Portfile was a broken one (#94, docs/designs/prose-stream.md).
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// prose is Prose's writer, built over Out by Begin and Staged.
	prose io.Writer
	// interactive stands in for a terminal in tests (AsTerminal).
	interactive bool
	// lines reads answers from In; one reader for the whole command, so
	// input it buffered for one question is there for the next.
	lines *bufio.Reader
	// mode is whether this command line reports as JSON, and its result.
	mode *outputMode
	// status is standard error's one redrawn line, which progress reports
	// print around.
	status *StatusLine
	// display is how the text is shown (WithDisplay).
	display Display
}

// AsTerminal are the streams as a terminal's, where a test stands one in:
// every stream is one, and questions are asked of In.
func (s Streams) AsTerminal() Streams {
	s.interactive = true
	return s
}

// Begin are the streams a command line runs with: its input read by line,
// its output switched off while --json gathers the envelope, which args
// ask for, its output mode, and its status line.
func Begin(streams Streams, args []string) Streams {
	if streams.lines == nil && streams.In != nil {
		streams.lines = bufio.NewReader(streams.In)
	}
	// Streams begun already, as the console hands its own to a command it
	// runs, are begun again over their raw output, with no second wrap.
	if begun, ok := streams.Out.(*switchWriter); ok {
		streams.Out = begun.w
	}
	streams.mode = &outputMode{json: asksForJSON(args), stdout: streams.Out}
	colour := streams.colourMarks()
	streams.Out = &switchWriter{w: streams.Out}
	streams.prose = streams.proseOver(streams.Out, colour)
	if streams.status == nil {
		streams.status = &StatusLine{w: streams.Err}
	}
	return streams
}

// Prose is the writer for dockhand's own sentences, tables and views: it
// wraps at the width, a terminal's or COLUMNS (wrapWriter), and colours
// the marks on a terminal. Streams never begun, as a test's, write prose
// to Out as it is.
func (s Streams) Prose() io.Writer {
	if s.prose == nil {
		return s.Out
	}
	return s.prose
}

// proseOver is Prose's writer over w: the marks coloured where colour
// says, then wrapped.
func (s Streams) proseOver(w io.Writer, colour bool) io.Writer {
	if colour {
		w = &markWriter{w: w}
	}
	return &wrapWriter{w: w, width: s.Width(), cut: s.OutTerminal(), mode: s.mode}
}

// Stamped are the streams with each line of standard output, prose and
// raw alike, passed through stamp first, beneath --json's switch: serve's
// time stamp off a terminal, which "serve: stopped" lost where Prose was
// built before it.
func (s Streams) Stamped(stamp func(io.Writer) io.Writer) Streams {
	if out, ok := s.Out.(*switchWriter); ok {
		out.w = stamp(out.w)
		return s
	}
	s.Out = stamp(s.Out)
	return s
}

// Status is the command's redrawn line on standard error.
func (s Streams) Status() *StatusLine {
	if s.status != nil {
		return s.status
	}
	return &StatusLine{w: s.Err}
}

// Say prints a line on standard error, above the line redrawn there.
func (s Streams) Say(line string) { s.Status().Say(line) }

// ErrTerminal reports whether errors and progress go to a terminal, where
// a line can be redrawn.
func (s Streams) ErrTerminal() bool { return s.onTerminal(s.Err) }

// Terminal reports whether the input is an interactive terminal, the only
// place a command may ask a question.
func (s Streams) Terminal() bool { return s.onTerminal(s.In) }

// OutTerminal reports whether the output is a terminal that can be
// redrawn.
func (s Streams) OutTerminal() bool { return s.onTerminal(s.Out) }

// onTerminal reports whether one of the streams is a terminal. A --json
// command line's never is: it's a script's, which never asks and reads
// no redrawn line. /dev/null is a character device too, so this asks the
// terminal driver rather than the file's mode, of the file under the
// writer Begin puts before the output, which watch's live view never saw
// past (the command package review's three predicates, folded).
func (s Streams) onTerminal(stream any) bool {
	if s.JSON() {
		return false
	}
	if s.interactive {
		return true
	}
	file, ok := underlying(stream).(*os.File)
	return ok && isTerminal(file.Fd())
}

// Width is the output terminal's columns, for text to wrap at; off a
// terminal, COLUMNS where it's set (Display), else 100.
func (s Streams) Width() int {
	if file, ok := underlying(s.Out).(*os.File); ok {
		if columns := terminalWidth(file.Fd()); columns > 0 {
			return columns
		}
	}
	if s.display.Columns > 0 {
		return s.display.Columns
	}
	return defaultColumns
}

// Height is the output terminal's rows, for a view to fit; 0 off a
// terminal, where nothing is fitted.
func (s Streams) Height() int {
	if file, ok := underlying(s.Out).(*os.File); ok {
		return terminalHeight(file.Fd())
	}
	return 0
}

// Beneath are the input and the output as a view drawn on the whole screen
// reads and writes them (term/screen): the output beneath the writers
// Begin puts before it, which would wrap a frame's lines, colour its
// marks, and break its one write into several.
func (s Streams) Beneath() (io.Reader, io.Writer) {
	out, _ := underlying(s.Out).(io.Writer)
	return s.In, out
}

// Unattended are the streams of a command that asks nothing, whatever its
// input, as bump does: everything that would ask goes the way it goes
// without a terminal.
func (s Streams) Unattended() Streams {
	s.In, s.interactive, s.lines = strings.NewReader(""), false, nil
	return s
}

// Ask puts a question on standard error and reads one line. Input that
// ends without an answer is an empty answer, which takes the default.
func (s Streams) Ask(question string) (string, error) {
	// A stage's report held back is said before its question, which the
	// person answers from it (Staged).
	if held, ok := s.Out.(*stageWriter); ok {
		held.Flush()
	}
	fmt.Fprint(s.Err, question)
	line, err := s.ReadLine()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// ReadLine reads one line of input, through the command's one reader.
func (s Streams) ReadLine() (string, error) {
	lines := s.lines
	if lines == nil {
		lines = bufio.NewReader(s.In)
	}
	return lines.ReadString('\n')
}

// underlying is the stream beneath the writers the streams wrap it in:
// --json's switch, the marks' colour, and a stage's report held back.
func underlying(stream any) any {
	for {
		switch w := stream.(type) {
		case *switchWriter:
			stream = w.w
		case *markWriter:
			stream = w.w
		case *wrapWriter:
			stream = w.w
		case *stageWriter:
			stream = w.to
		default:
			return stream
		}
	}
}
