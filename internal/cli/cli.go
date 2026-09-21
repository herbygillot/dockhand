package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/app"
)

var errNotImplemented = errors.New("dockhand v2: command workflows are not wired yet")

type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Options are the attachment and destination choices of the change commands.
// The defaults are the foreground and the PR: a command stays through
// verification and publication unless told to stop earlier or to detach.
type Options struct {
	Detach bool
	Trace  bool
	JSON   bool
	Diff   bool
	// AllSubports verifies every buildable member of a shared release
	// locally instead of the newest subport alone.
	AllSubports bool
}

func Run(ctx context.Context, args []string, streams Streams, config app.Config) error {
	return run(ctx, args, streams, config, app.Build)
}

func run(ctx context.Context, args []string, streams Streams, config app.Config, build serviceBuilder) error {
	root, runtime, err := newRoot(config, build)
	if err != nil {
		return err
	}
	root.SetArgs(append([]string{}, args...))
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	executed, err := root.ExecuteContextC(ctx)
	if runtime.json && executed != nil && executed != root {
		if writeErr := writeEnvelope(streams.Out, executed, runtime.outcome, err); writeErr != nil {
			return errors.Join(err, writeErr)
		}
	}
	return err
}

// Envelope is the shape of every JSON result: the verb, the exit code the
// process will return, the one-line error if any, and the command's result.
type Envelope struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error"`
	Result   any    `json:"result"`
}

func writeEnvelope(out io.Writer, cmd *cobra.Command, result any, err error) error {
	envelope := Envelope{Command: strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" "), ExitCode: ExitCode(err), Result: result}
	if err != nil {
		envelope.Error = err.Error()
	}
	return json.NewEncoder(out).Encode(envelope)
}

// emit records a command's result for the envelope in JSON mode.
func (r *runtime) emit(result any) error {
	r.outcome = result
	return nil
}

func execute(ctx context.Context, command string, args []string, options Options, streams Streams, services *app.Services) error {
	return errNotImplemented
}
