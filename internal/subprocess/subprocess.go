package subprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Spec describes one invocation.
type Spec struct {
	// Tool names the program in errors, such as "git" or "tart".
	Tool string
	// Command names the invocation in errors; empty selects the first argument.
	Command string
	Path    string
	Args    []string
	Dir     string
	// Env is the complete environment; nil inherits the process environment.
	Env   []string
	Stdin io.Reader
	// Stdout receives standard output in addition to the captured copy.
	Stdout io.Writer
	// StdoutOnly sends standard output to Stdout alone, uncaptured, for a
	// stream too large to keep, such as a file being transferred.
	StdoutOnly bool
	// Combined captures standard error into the same buffer as standard output.
	Combined   bool
	ExtraFiles []*os.File
	// WaitDelay bounds how long the process may outlive a canceled context;
	// zero selects two seconds.
	WaitDelay time.Duration
	// Limit bounds the captured bytes of each stream; zero imposes none.
	Limit int64
}

// Result is what the command wrote. Stderr is empty when Combined.
type Result struct {
	Output []byte
	Stderr []byte
}

// Error reports a failed command with what it wrote to standard error, or to
// the combined buffer when the streams were merged.
type Error struct {
	Tool    string
	Command string
	Stderr  string
	Cause   error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s %s: %v: %s", e.Tool, e.Command, e.Cause, e.Stderr)
}

func (e *Error) Unwrap() error { return e.Cause }

// errOutputLimit means a stream exceeded the spec's limit.
var errOutputLimit = errors.New("subprocess: output exceeds limit")

// Run executes the command and returns what it wrote, even when it failed.
func Run(ctx context.Context, spec Spec) (Result, error) {
	if spec.Path == "" {
		return Result{}, fmt.Errorf("subprocess: %s: executable is required", spec.Tool)
	}
	command := exec.CommandContext(ctx, spec.Path, spec.Args...)
	command.Dir = spec.Dir
	command.Env = spec.Env
	command.Stdin = spec.Stdin
	command.WaitDelay = spec.WaitDelay
	if command.WaitDelay == 0 {
		command.WaitDelay = 2 * time.Second
	}
	for _, file := range spec.ExtraFiles {
		if file != nil {
			command.ExtraFiles = append(command.ExtraFiles, file)
		}
	}
	output, stderr := &bounded{limit: spec.Limit}, &bounded{limit: spec.Limit}
	var out io.Writer = output
	if spec.Stdout != nil {
		out = io.MultiWriter(spec.Stdout, output)
	}
	command.Stdout = out
	if spec.StdoutOnly && spec.Stdout != nil && !spec.Combined {
		command.Stdout = spec.Stdout
	}
	if spec.Combined {
		command.Stderr = out
	} else {
		command.Stderr = stderr
	}
	err := command.Run()
	result := Result{Output: output.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	if output.exceeded || stderr.exceeded {
		err = errors.Join(errOutputLimit, err)
	}
	detail := stderr.String()
	if spec.Combined {
		detail = output.String()
	}
	name := spec.Command
	if name == "" && len(spec.Args) > 0 {
		name = spec.Args[0]
	}
	return result, &Error{Tool: spec.Tool, Command: name, Stderr: strings.TrimSpace(detail), Cause: errors.Join(ctx.Err(), err)}
}

// bounded stops accepting bytes past its limit and remembers that it did, so a
// runaway helper fails instead of exhausting memory. It wraps the buffer
// rather than embedding it: an embedded buffer would promote ReadFrom, which
// io.Copy prefers over Write, and the limit would never be consulted.
type bounded struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *bounded) Write(p []byte) (int, error) {
	if b.limit > 0 && int64(b.buffer.Len()+len(p)) > b.limit {
		b.exceeded = true
		return 0, errOutputLimit
	}
	return b.buffer.Write(p)
}

func (b *bounded) Bytes() []byte  { return b.buffer.Bytes() }
func (b *bounded) String() string { return b.buffer.String() }
