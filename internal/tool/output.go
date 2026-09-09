package tool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

// Opts is what a one-shot command takes besides its binary.
type Opts struct {
	// Args are the arguments after the binary.
	Args []string
	// Env is the whole environment the command runs with; nil inherits
	// the process's own, as os/exec does.
	Env []string
	// Stdin feeds the command; nil is no input.
	Stdin io.Reader
	// OwnSession puts the child in a session of its own, so a signal
	// aimed at whoever invoked this process does not reach it.
	//
	// IT IS FOR A CHILD MEANT TO OUTLIVE THE CALL THAT STARTED IT. A
	// process inherits its parent's process group, so a job controller
	// that reaps the group on completion — which agent shells, CI
	// runners and `timeout` all do — takes the child with it. Measured
	// on a detached bump: the virtual machine came up holding the
	// INVOKING SHELL'S pgid, and a TERM to that group stopped it
	// mid-build, leaving "the environment stopped before the run
	// reported an outcome" and no account of why, because the process
	// that would have written one had already exited.
	//
	// It is not a way to leak processes. Whoever sets it owes an answer
	// for what it started; dockhand's is the lease, which is what
	// reclaims a worker nobody is holding.
	OwnSession bool
	// Limit bounds how many bytes are kept from each of the child's
	// two streams. Zero keeps everything, which is what every caller
	// whose stdout is a document — an archive, a JSON body, a tag
	// list — needs, and it is the default for exactly that reason.
	//
	// It is here for the caller whose child's output is a story rather
	// than a document, where a process that will not stop talking
	// would otherwise put its whole spool into dockhand's memory and,
	// worse, into an error message somebody has to read. Past the
	// bound the bytes are dropped, Result.Truncated says so, and the
	// child runs on: see capped for why it is not stopped instead.
	Limit int
}

// Result is everything a finished command left behind — what it wrote
// on each stream, how it ended, and whether Limit cut it short.
//
// It is returned whether the command succeeded or FAILED, and that is
// the whole point of the type. A child that exits non-zero has usually
// already said the thing its caller needs: `gh api --include` answers
// a conditional request with "HTTP/2.0 304 Not Modified" on stdout and
// then exits one, because a 304 is not a 2xx. A transport that dropped
// those bytes left the protocol fact recoverable only from the
// wrapper's error prose — which is how the cheapest and most
// successful answer a revalidation can get came to be counted against
// the host that gave it. What the child wrote is evidence; how it
// exited is a separate fact; this type keeps both and mixes neither.
//
// A caller that only cares about the happy path loses nothing: err is
// still non-nil on any failure, and ignoring Result then costs it
// exactly what it used to get.
type Result struct {
	// Stdout is what the command wrote on standard output, byte for
	// byte and untrimmed, because it is data.
	Stdout []byte
	// Stderr is what it wrote on standard error, trimmed, because it
	// is a message.
	Stderr string
	// Code is the exit status, or -1 when the process did not exit on
	// its own: killed by a signal, or never started.
	Code int
	// Truncated reports that Opts.Limit cut one of the streams short,
	// so that a caller parsing Stdout can tell a whole answer from a
	// fragment of one instead of failing to parse a fragment and
	// blaming the child for it.
	Truncated bool
}

// Failure is Output's error when the command did not succeed: it ran
// and exited non-zero, was killed, or could not be started. Its text
// is the trimmed stderr when the command wrote any, else what os/exec
// reported — "exit status 1", "signal: killed", a start failure — so a
// wrapper prefixes it with its own context and the message reads as it
// always has. Unwrap reaches the exec error, so a context's
// cancellation is still findable with errors.Is.
//
// It carries no stdout, deliberately: the bytes come back on the
// Result beside it, where they are one value rather than something to
// be dug out of an error chain by whichever wrapper still has the
// original type. An error travels further than it stays typed — every
// wrapper on this tree reformats a child's words with %s on purpose,
// so that a child's exit status is not handed on as dockhand's — and a
// fact that must survive that trip does not belong here.
type Failure struct {
	// Code is the exit status, or -1 when the process did not exit on
	// its own: killed by a signal, or never started.
	Code int
	// Stderr is the command's standard error, trimmed.
	Stderr string
	// Err is what os/exec reported.
	Err error
}

func (e *Failure) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	return e.Err.Error()
}

func (e *Failure) Unwrap() error { return e.Err }

// Output runs a resolved binary to completion and hands back
// everything it left behind, whether or not it succeeded. err is a
// *Failure when the command exited non-zero, was killed, or never
// started; the Result is filled in either way.
//
// bin is a path a Finder resolved. Resolving is left to the caller
// because each wrapper words a miss differently: git passes it
// through, gh appends an install hint, upstream and vendored replace
// it with sentinels of their own.
func Output(ctx context.Context, bin string, o Opts) (Result, error) {
	cmd := exec.CommandContext(ctx, bin, o.Args...)
	cmd.Env = o.Env
	cmd.Stdin = o.Stdin
	if o.OwnSession {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	out, errb := capped{limit: o.Limit}, capped{limit: o.Limit}
	cmd.Stdout, cmd.Stderr = &out, &errb

	runErr := cmd.Run()
	res := Result{
		Stdout:    out.buf.Bytes(),
		Stderr:    strings.TrimSpace(errb.buf.String()),
		Truncated: out.cut || errb.cut,
	}
	if runErr == nil {
		return res, nil
	}
	res.Code = -1
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		res.Code = ee.ExitCode()
	}
	return res, &Failure{Code: res.Code, Stderr: res.Stderr, Err: runErr}
}

// capped collects one stream up to a bound and remembers whether it
// dropped anything.
//
// Refusing the write would be the obvious way to say "enough", and it
// is the wrong one: os/exec's copier treats a writer's error as the
// command's error, so a child that talked too much would come back as
// a command that FAILED rather than one that was verbose — the bound
// would change the verdict instead of bounding the memory. So the
// write is claimed in full and the excess is dropped, and cut carries
// the one fact that matters forward, so that nothing downstream reads
// a fragment as the whole answer.
type capped struct {
	buf bytes.Buffer
	// limit is the most bytes to keep; zero or less keeps everything.
	limit int
	// cut records that something was dropped.
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.limit <= 0 {
		return c.buf.Write(p)
	}
	room := c.limit - c.buf.Len()
	switch {
	case room <= 0:
		c.cut = true
		return len(p), nil
	case len(p) > room:
		c.cut = true
		if _, err := c.buf.Write(p[:room]); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// Run runs a resolved binary with stdout and stderr merged into one
// transcript, returned whether or not the command succeeded, with the
// exec error as it came. Nothing here chooses a stream or words a
// failure: that is the caller's, which is what tart's wrapper wants —
// its diagnostics land on either stream and its callers parse output
// after a non-zero exit.
func Run(ctx context.Context, bin string, o Opts) (string, error) {
	cmd := exec.CommandContext(ctx, bin, o.Args...)
	cmd.Env = o.Env
	cmd.Stdin = o.Stdin
	if o.OwnSession {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}
