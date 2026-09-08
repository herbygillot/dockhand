// Package progress is narration: what an operation is doing, said as it
// happens. It is its own package, and a very small one, because every
// lifecycle needs to narrate and none of them may import the
// application to do it.
//
// Nothing a caller needs travels through a Sink. Facts come back in the
// operation's typed result; this is for the human watching.
package progress

import "io"

// Level is how loudly a line is said. It is an enum rather than a
// severity number because the three answers are the only ones the
// operator projections know how to place, and a fourth would be a
// decision about the report rather than about the narration.
type Level uint8

const (
	// Info is the ordinary line: a target landed, an attempt was
	// adopted. It is the zero value because a caller that forgets to
	// name a level is saying something ordinary, and there is nothing
	// here for rule 7 to protect — a narration carries no fact anybody
	// decides from, so its zero cannot mean "I could not find out".
	Info Level = iota
	// Warn is something the operator should notice and that refused
	// nothing. A refusal is a typed error on the result; this is the
	// sentence beside one.
	Warn
	// Detail is the line only a person following along wants — the
	// per-target chatter a sweep of four hundred produces.
	Detail
)

// Sink receives narration. Its methods return nothing on purpose: a
// caller cannot learn anything by narrating, and an operation cannot
// fail because nobody was listening.
type Sink interface {
	// Stage names the operation and the stage it has reached, so a
	// reader can place a line without the line having to repeat itself.
	Stage(op, stage string)
	// Say is one sentence at one level.
	Say(Level, string)
	// Stream is a raw build log, passed through; not events. It is an
	// io.Reader and not a string because a build log is unbounded and
	// arrives while it is still being written.
	Stream(io.Reader)
}

// Discard is the zero sink, for a JSON caller and for a test.
//
// It is a struct with value receivers rather than a package-level
// variable so that a caller may write progress.Discard{} in a
// composite literal without a nil check anywhere: an operation's
// Progress field is never tested for nil in this design, and a sink
// that has to be non-nil is one every test would have to remember.
type Discard struct{}

// Stage, Say and Stream are the whole of Discard: three methods that
// take what they are given and keep nothing.
func (Discard) Stage(string, string) {}
func (Discard) Say(Level, string)    {}
func (Discard) Stream(io.Reader)     {}

// Discard satisfies Sink, checked here rather than at the call site so
// that a method added to Sink is a build failure in this package
// instead of in every package that hands one in.
var _ Sink = Discard{}
