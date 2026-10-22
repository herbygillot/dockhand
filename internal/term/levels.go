package term

import (
	"bytes"
	"fmt"
	"io"
)

// SetVerbosity records -v's count for the command line, which its
// streams' every copy reads (Verbose).
func (s Streams) SetVerbosity(count int) {
	if s.mode != nil {
		s.mode.verbose = count
	}
}

// Verbose is -v's count: 0 at the default level, 1 with -v, 2 with -vv
// (Design v3 §12).
func (s Streams) Verbose() int {
	if s.mode == nil {
		return 0
	}
	return s.mode.verbose
}

// Staged are streams for one stage of a command that runs several, as
// ship's edit, commit, check, and submission: at the default level, what
// the stage writes to standard output is held back, and said only where
// the stage held or failed (Stage.Say) or asks a question, which the
// person answers from it; with -v it's said as it's written (part 5, item
// 3: a clean bump printed four stages' full reports, 46 lines, where the
// person needs a line a stage). On a terminal, the stage under way is the
// line redrawn on standard error, until it's done or said.
func (s Streams) Staged(under string) (Streams, *Stage) {
	stage := &Stage{status: s.Status()}
	if s.Verbose() > 0 || s.JSON() {
		return s, stage
	}
	if s.ErrTerminal() {
		stage.status.Show("⋯ " + under)
	}
	held := &stageWriter{to: s.Out}
	stage.held = held
	s.Out = held
	return s, stage
}

// Stage is a stage's report held back (Staged).
type Stage struct {
	held   *stageWriter
	status *StatusLine
}

// Say says the stage's report held back, as for a stage that held or
// failed, where the person reads why.
func (st *Stage) Say() {
	if st.held != nil {
		st.status.Clear()
		st.held.Flush()
	}
}

// Done says a stage that went through in its one line, where its report
// was held back, which is -v's: the report is dropped, unless a question
// already said it.
func (st *Stage) Done(format string, args ...any) {
	if st.held == nil {
		return
	}
	st.status.Clear()
	st.held.buf.Reset()
	fmt.Fprintf(st.held.to, format+"\n", args...)
}

// stageWriter holds what a stage writes until it's said; once said, as a
// question says it, what follows is written as it comes.
type stageWriter struct {
	buf  bytes.Buffer
	to   io.Writer
	said bool
}

func (w *stageWriter) Write(p []byte) (int, error) {
	if w.said {
		return w.to.Write(p)
	}
	return w.buf.Write(p)
}

// Flush writes what's held, and what follows as it comes.
func (w *stageWriter) Flush() {
	w.said = true
	if w.buf.Len() > 0 {
		_, _ = w.to.Write(w.buf.Bytes())
		w.buf.Reset()
	}
}
