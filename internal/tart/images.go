package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// ASIFVersion is the first Tart that lists its VMs while one with an ASIF
// disk runs, as Golden Gate's images have (openai/tart#1344, fixed by #1349
// in 2.39.0). With it, a listing in an ASIF VM's first seconds no longer
// fails that VM's start either: eleven of eleven starts listed through
// their first ten seconds succeeded on 2.39.0, where ten of eleven failed
// on 2.37.0.
var ASIFVersion = []int{2, 39, 0}

// HandlesASIF reports whether a Tart version, as `tart --version` prints
// it, is ASIFVersion or newer.
func HandlesASIF(version string) bool {
	fields := strings.FieldsFunc(strings.TrimSpace(version), func(r rune) bool { return r == '.' || r == '-' || r == '+' || r == ' ' })
	for i, minimum := range ASIFVersion {
		if i >= len(fields) {
			return false
		}
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return false
		}
		if n != minimum {
			return n > minimum
		}
	}
	return true
}

// ErrListingRaced reports a listing that failed because a VM went while
// Tart listed (openai/tart#1353): it checks each VM's directory is whole,
// then reads its config.json again to size it, so a VM another process
// deletes in between fails the whole listing ("The file “config.json”
// couldn’t be opened because there is no such file"). It happened to four
// of eleven images probed two at a time on 2026-09-27, on Tart 2.39.0, and
// the code is the same in 2.37.0. Listing again, once the delete is done,
// answers.
var ErrListingRaced = errors.New("tart: a VM went while Tart listed its VMs")

// listingAttempts bounds how often Images lists again after a raced
// listing, and listingPause how long it waits between: a delete takes well
// under a second.
var (
	listingAttempts = 5
	listingPause    = 500 * time.Millisecond
)

// ErrVMMissing reports a VM Tart does not have.
var ErrVMMissing = errors.New("tart: VM does not exist")

// ErrVMStopped reports that `tart stop` found the VM already stopped.
var ErrVMStopped = errors.New("tart: VM is not running")

type Image struct {
	Name    string
	Source  string
	Running bool
	// Accessed is when Tart last opened it, as its listing says; zero
	// where the listing doesn't say.
	Accessed time.Time
}

// VM is what `tart get` says of one VM.
type VM struct {
	Running bool
	// DiskFormat is "raw" or "asif".
	DiskFormat string
}

func (c Client) Images(ctx context.Context, options RunOptions) ([]Image, error) {
	output, err := c.Run(ctx, options, "list", "--format", "json")
	for attempt := 1; errors.Is(err, ErrListingRaced) && attempt < listingAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, errors.Join(err, ctx.Err())
		case <-time.After(listingPause):
		}
		output, err = c.Run(ctx, options, "list", "--format", "json")
	}
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name, Source, State *string
		Running             *bool
		Accessed            string
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
		accessed, _ := time.Parse(time.RFC3339, row.Accessed)
		result = append(result, Image{Name: *row.Name, Source: *row.Source, Running: running, Accessed: accessed})
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
	case failure.Command == "list" && strings.Contains(failure.Stderr, "because there is no such file"):
		return fmt.Errorf("%w: %w", ErrListingRaced, err)
	case failure.Command != "delete" && strings.Contains(failure.Stderr, "does not exist"):
		return fmt.Errorf("%w: %w", ErrVMMissing, err)
	case failure.Command == "stop" && strings.Contains(failure.Stderr, "is not running"):
		return fmt.Errorf("%w: %w", ErrVMStopped, err)
	}
	return err
}
