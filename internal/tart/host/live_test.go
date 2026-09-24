package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/stretchr/testify/require"
)

// liveMachine is the person's Tart, for tests that opt in with a prepared
// image; they clone it and never change it.
func liveMachine(t *testing.T, variable string) (Machine, string) {
	t.Helper()
	image := os.Getenv(variable)
	if image == "" {
		t.Skipf("set %s for the opt-in Tart contract test", variable)
	}
	runtime, err := (tart.Client{}).Resolve()
	require.NoError(t, err)
	return Machine{Client: runtime}, image
}

func scratchName(t *testing.T) string {
	t.Helper()
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	return "dockhand-test-" + hex.EncodeToString(suffix)
}

// discardScratch stops and deletes a test's clone however the test ended.
func discardScratch(t *testing.T, m Machine, run **Foreground, name string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if *run != nil {
			_ = (*run).Stop(ctx, 30*time.Second)
		}
		if err := m.Delete(ctx, name); err != nil {
			t.Errorf("scratch VM %s was left behind: %v", name, err)
		}
	})
}

func waitRunning(t *testing.T, ctx context.Context, m Machine, run *Foreground, name string) {
	t.Helper()
	for {
		_, running, err := m.LocalVM(ctx, name)
		require.NoError(t, err)
		if running {
			return
		}
		pause(t, ctx, run, name)
	}
}

// pause waits between polls, failing at once if the run has exited, which
// Apple's limit of two running macOS VMs per Mac can cause.
func pause(t *testing.T, ctx context.Context, run *Foreground, name string) {
	t.Helper()
	select {
	case <-run.Done():
		t.Fatalf("tart run %s exited: %v", name, run.Err())
	case <-ctx.Done():
		t.Fatalf("%s: %v", name, ctx.Err())
	case <-time.After(500 * time.Millisecond):
	}
}

// The Tart behavior dockhand depends on, against the installed Tart, so an
// upgrade that changes it fails here: the listing and description fields
// read, "not running" from a stop, a clone refused onto a listed name, and
// delete's report that a running VM "does not exist" while leaving it
// (openai/tart#1345). Run with DOCKHAND_TEST_TART_IMAGE naming a stopped,
// raw-disk image, e.g. dockhand-base-monterey.
func TestLiveTartContracts(t *testing.T) {
	m, image := liveMachine(t, "DOCKHAND_TEST_TART_IMAGE")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	exists, running, err := m.LocalVM(ctx, image)
	require.NoError(t, err)
	require.True(t, exists, "the listing names %s as a local VM", image)
	require.False(t, running)
	format, err := m.DiskFormat(ctx, image)
	require.NoError(t, err)
	require.Equal(t, "raw", format)
	_, err = m.DiskFormat(ctx, scratchName(t))
	require.ErrorIs(t, err, tart.ErrVMMissing)

	name := scratchName(t)
	var run *Foreground
	require.NoError(t, m.Clone(ctx, image, name))
	discardScratch(t, m, &run, name)
	require.ErrorContains(t, m.Clone(ctx, image, name), "refusing to overwrite existing VM")
	_, err = m.run(ctx, nil, "stop", name)
	require.ErrorIs(t, err, tart.ErrVMStopped)

	run, err = m.StartForeground(name)
	require.NoError(t, err)
	waitRunning(t, ctx, m, run, name)
	_, err = m.run(ctx, nil, "delete", name)
	require.ErrorContains(t, err, "does not exist", "Tart still reports a running VM as missing to delete (openai/tart#1345); if this fails, Tart changed")
	require.False(t, errors.Is(err, tart.ErrVMMissing))
	exists, running, err = m.LocalVM(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && running, "and leaves it running")
	require.ErrorContains(t, m.Delete(ctx, name), "cannot delete running VM")
	require.ErrorContains(t, m.Rename(ctx, name, name+"-renamed"), "cannot rename running VM")

	require.NoError(t, run.Stop(ctx, 30*time.Second))
	run = nil
	require.NoError(t, m.Delete(ctx, name))
	exists, _, err = m.LocalVM(ctx, name)
	require.NoError(t, err)
	require.False(t, exists)
}

// A running VM with an ASIF disk keeps `tart list`, and `tart get` of it,
// from answering until it stops (openai/tart#1344). Run with
// DOCKHAND_TEST_TART_ASIF_SOURCE naming an ASIF image, e.g.
// ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest once pulled. While it
// runs, every listing on the machine fails, so it is kept short.
func TestLiveTartASIFBlocksTheListing(t *testing.T) {
	m, source := liveMachine(t, "DOCKHAND_TEST_TART_ASIF_SOURCE")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	name := scratchName(t)
	var run *Foreground
	require.NoError(t, m.Clone(ctx, source, name))
	discardScratch(t, m, &run, name)
	format, err := m.DiskFormat(ctx, name)
	require.NoError(t, err)
	require.Equal(t, "asif", format)

	// Nothing touches the VM while it starts: a listing reads every VM's
	// disk, and one issued while an ASIF VM starts makes that start fail
	// with "The virtual machine failed to start" (found 2026-09-24). The
	// start is past opening its disk well within the wait.
	run, err = m.StartForeground(name)
	require.NoError(t, err)
	select {
	case <-run.Done():
		t.Fatalf("tart run %s exited: %v", name, run.Err())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(20 * time.Second):
	}
	_, err = m.Images(ctx)
	require.ErrorIs(t, err, tart.ErrListingBlocked, "if Tart fixed openai/tart#1344, dockhand can stop declining ASIF images")
	_, err = m.DiskFormat(ctx, name)
	require.ErrorIs(t, err, tart.ErrListingBlocked)

	require.NoError(t, run.Stop(ctx, 30*time.Second))
	run = nil
	_, err = m.Images(ctx)
	require.NoError(t, err, "the listing answers again once the VM stops")
}
