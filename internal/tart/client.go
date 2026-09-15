package tart

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
	command := exec.CommandContext(ctx, c.Executable, args...)
	command.Env = c.Environment()
	command.Stdin = options.Input
	command.WaitDelay = 2 * time.Second
	for _, file := range options.ExtraFiles {
		if file != nil {
			command.ExtraFiles = append(command.ExtraFiles, file)
		}
	}
	var output, stderr bytes.Buffer
	if options.Combined {
		writer := io.Writer(&output)
		if options.Output != nil {
			writer = io.MultiWriter(options.Output, &output)
		}
		command.Stdout, command.Stderr = writer, writer
	} else {
		command.Stdout = &output
		command.Stderr = &stderr
		if options.Output != nil {
			command.Stdout = io.MultiWriter(options.Output, &output)
		}
	}
	err := command.Run()
	if err == nil {
		return output.Bytes(), nil
	}
	detail := stderr.String()
	if options.Combined {
		detail = output.String()
	}
	return output.Bytes(), fmt.Errorf("tart: %s: %w: %s", args[0], errors.Join(ctx.Err(), err), strings.TrimSpace(detail))
}

// Environment supplies the host process environment used for this Tart home.
func (c Client) Environment() []string {
	return append(os.Environ(), "TART_HOME="+c.Home, "TART_NO_AUTO_PRUNE=1", "LC_ALL=C")
}
