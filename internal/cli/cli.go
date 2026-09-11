package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/herbygillot/dockhand/v2/internal/app"
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

func Run(ctx context.Context, args []string, streams Streams, services *app.Services) error {
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(streams.Out, "Dockhand v2 groundwork. Command workflows are not wired yet.")
		return err
	}
	return ErrNotImplemented
}
