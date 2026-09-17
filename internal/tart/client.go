package tart

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/subprocess"
	"io"
	"os"
)

// Client runs commands against one Tart installation and home directory.
type Client struct {
	Executable string
	Home       string
}

// RunOptions controls input, output streaming, and inherited lock descriptors.
type RunOptions struct {
	Input      io.Reader
	Output     io.Writer
	Combined   bool
	ExtraFiles []*os.File
}

// Run invokes one Tart subcommand and returns its captured standard output.
// Combined captures and optionally streams both output channels in one buffer.
func (c Client) Run(ctx context.Context, options RunOptions, args ...string) ([]byte, error) {
	if c.Executable == "" || len(args) == 0 {
		return nil, fmt.Errorf("tart: executable and command are required")
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "tart", Path: c.Executable, Args: args, Env: c.Environment(), Stdin: options.Input, Stdout: options.Output, Combined: options.Combined, ExtraFiles: options.ExtraFiles})
	return result.Output, err
}

// Environment supplies the host process environment used for this Tart home.
func (c Client) Environment() []string {
	return append(os.Environ(), "TART_HOME="+c.Home, "TART_NO_AUTO_PRUNE=1", "LC_ALL=C")
}
