package tool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
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
}

// Failure is Output's error when the command did not succeed: it ran
// and exited non-zero, was killed, or could not be started. Its text
// is the trimmed stderr when the command wrote any, else what os/exec
// reported — "exit status 1", "signal: killed", a start failure — so a
// wrapper prefixes it with its own context and the message reads as it
// always has. Unwrap reaches the exec error, so a context's
// cancellation is still findable with errors.Is.
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

// Output runs a resolved binary to completion and returns its stdout
// and exit status. On failure stdout is nil, code is the exit status
// (-1 when there is none) and err is a *Failure; every wrapper today
// discards a failed command's stdout, and a caller that wants it is
// asking for Run.
//
// bin is a path a Finder resolved. Resolving is left to the caller
// because each wrapper words a miss differently: git passes it
// through, gh appends an install hint, upstream and vendored replace
// it with sentinels of their own.
func Output(ctx context.Context, bin string, o Opts) (stdout []byte, code int, err error) {
	cmd := exec.CommandContext(ctx, bin, o.Args...)
	cmd.Env = o.Env
	cmd.Stdin = o.Stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return nil, code, &Failure{Code: code, Stderr: strings.TrimSpace(errb.String()), Err: err}
	}
	return out.Bytes(), 0, nil
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
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}
