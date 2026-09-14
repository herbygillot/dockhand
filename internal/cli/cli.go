package cli

import (
	"context"
	"errors"
	"io"

	"github.com/herbygillot/dockhand/internal/app"
)

var ErrNotImplemented = errors.New("dockhand v2: command workflows are not wired yet")

type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Options struct {
	Wait     bool
	Trace    bool
	JSON     bool
	NoVerify bool
	Publish  bool
	Diff     bool
}

func Run(ctx context.Context, args []string, streams Streams, config app.Config) error {
	root, err := NewRoot(config)
	if err != nil {
		return err
	}
	root.SetArgs(append([]string{}, args...))
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	return root.ExecuteContext(ctx)
}

func execute(ctx context.Context, command string, args []string, options Options, streams Streams, services *app.Services) error {
	return ErrNotImplemented
}
