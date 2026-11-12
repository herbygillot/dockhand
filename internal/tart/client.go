package tart

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Client runs commands against one Tart installation and home directory.
type Client struct {
	Executable string
	Home       string
	// Env is the environment Tart runs in, which compose gives; Resolve
	// refuses none.
	Env []string
}

// RunOptions controls input, output streaming, and inherited lock descriptors.
type RunOptions struct {
	Input      io.Reader
	Output     io.Writer
	Combined   bool
	ExtraFiles []*os.File
}

// Run invokes one Tart subcommand and returns its captured standard output.
// The lock files carried under ctx are handed to it beside ExtraFiles, as
// an attempt's launch lock is to the clone it makes (filelock.Carried).
// Combined captures and optionally streams both output channels in one buffer.
// The failures dockhand acts on carry ErrListingBlocked, ErrVMMissing, or
// ErrVMStopped.
func (c Client) Run(ctx context.Context, options RunOptions, args ...string) ([]byte, error) {
	if c.Executable == "" || len(args) == 0 {
		return nil, fmt.Errorf("tart: executable and command are required")
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "tart", Path: c.Executable, Args: args, Env: c.Environment(), Stdin: options.Input, Stdout: options.Output, Combined: options.Combined, ExtraFiles: append(slices.Clone(options.ExtraFiles), filelock.Carried(ctx)...)})
	return result.Output, classify(err)
}

// Environment is the environment Tart runs in for this Tart home.
func (c Client) Environment() []string {
	return append(append([]string{}, c.Env...), "TART_HOME="+c.Home, "TART_NO_AUTO_PRUNE=1", "LC_ALL=C")
}

// KeychainLocked reports tart run's output where Virtualization.framework
// refused the VM for want of the login Keychain, locked over SSH with
// nobody at the screen: "VZErrorDomain Code=-9 'The virtual machine
// encountered a security error.' … Failed to get current host key"
// (rc7's F5, check-925, whose three attempts each failed so).
func KeychainLocked(output string) bool {
	return strings.Contains(output, "VZErrorDomain Code=-9") && (strings.Contains(output, "HostKey") || strings.Contains(output, "host key"))
}
