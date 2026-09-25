package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Dockhand learns what exists and what runs only from `tart list` and `tart
// get`, never from the files inside a Tart home, which Tart does not
// document. The JSON fields read are pinned by tests against Tart's output
// (internal/tart/live_test.go), so an upgrade that changes them fails a test
// rather than dockhand.

// ErrListingBlocked reports that Tart cannot describe its VMs because a VM
// with an ASIF disk is running: `tart list`, and `tart get` of that VM, fail
// for as long as it runs (openai/tart#1344). It clears when the VM stops, so
// callers wait it out as they wait for capacity.
var ErrListingBlocked = errors.New("tart: a running VM with an ASIF disk keeps Tart from listing its VMs until it stops (openai/tart#1344)")

// ErrVMMissing reports a VM Tart does not have.
var ErrVMMissing = errors.New("tart: VM does not exist")

// ErrVMStopped reports that `tart stop` found the VM already stopped.
var ErrVMStopped = errors.New("tart: VM is not running")

type Image struct {
	Name    string
	Source  string
	Running bool
}

// VM is what `tart get` says of one VM.
type VM struct {
	Running bool
	// DiskFormat is "raw" or "asif".
	DiskFormat string
}

func (c Client) Images(ctx context.Context, options RunOptions) ([]Image, error) {
	output, err := c.Run(ctx, options, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name, Source, State *string
		Running             *bool
	}
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("tart: invalid image listing: %w", err)
	}
	result := make([]Image, 0, len(rows))
	for _, row := range rows {
		if row.Name == nil || row.Source == nil || row.Running == nil && row.State == nil {
			return nil, fmt.Errorf("tart: invalid image listing: an entry lacks Name, Source, or Running and State, which dockhand reads")
		}
		running := row.Running != nil && *row.Running || row.State != nil && *row.State == "running"
		result = append(result, Image{Name: *row.Name, Source: *row.Source, Running: running})
	}
	return result, nil
}

// Get describes one VM.
func (c Client) Get(ctx context.Context, options RunOptions, name string) (VM, error) {
	output, err := c.Run(ctx, options, "get", name, "--format", "json")
	if err != nil {
		return VM{}, err
	}
	var row struct {
		Running    *bool
		State      *string
		DiskFormat *string
	}
	if err := json.Unmarshal(output, &row); err != nil {
		return VM{}, fmt.Errorf("tart: invalid description of %s: %w", name, err)
	}
	if row.DiskFormat == nil || row.Running == nil && row.State == nil {
		return VM{}, fmt.Errorf("tart: invalid description of %s: it lacks DiskFormat, or Running and State, which dockhand reads", name)
	}
	running := row.Running != nil && *row.Running || row.State != nil && *row.State == "running"
	return VM{Running: running, DiskFormat: *row.DiskFormat}, nil
}

// classify names the Tart failures dockhand acts on. "does not exist" is
// trusted only where it cannot be the collision Tart's delete has, which
// reports a running VM that way (openai/tart#1345); a delete's outcome is
// read from the listing instead.
func classify(err error) error {
	var failure *subprocess.Error
	if !errors.As(err, &failure) {
		return err
	}
	switch {
	case strings.Contains(failure.Stderr, "image info") && strings.Contains(failure.Stderr, "Resource temporarily unavailable"):
		return fmt.Errorf("%w: %w", ErrListingBlocked, err)
	case failure.Command != "delete" && strings.Contains(failure.Stderr, "does not exist"):
		return fmt.Errorf("%w: %w", ErrVMMissing, err)
	case failure.Command == "stop" && strings.Contains(failure.Stderr, "is not running"):
		return fmt.Errorf("%w: %w", ErrVMStopped, err)
	}
	return err
}
