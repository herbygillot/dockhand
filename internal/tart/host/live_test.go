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

// A VM with an ASIF disk, as Golden Gate's images have, starts while Tart
// lists its VMs, and `tart list` and `tart get` answer while it runs. Before
// Tart 2.39.0 neither held (openai/tart#1344): a running ASIF VM kept every
// listing from answering, and a listing in its first seconds failed its
// start. Run with DOCKHAND_TEST_TART_ASIF_SOURCE naming an ASIF image, e.g.
// ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest once pulled.
func TestLiveTartListsWhileAnASIFVMRuns(t *testing.T) {
	m, source := liveMachine(t, "DOCKHAND_TEST_TART_ASIF_SOURCE")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	version, err := m.Version(ctx)
	require.NoError(t, err)
	if !tart.HandlesASIF(version) {
		t.Skipf("Tart %s is older than the one that lists its VMs while an ASIF VM runs", version)
	}
	name := scratchName(t)
	var run *Foreground
	require.NoError(t, m.Clone(ctx, source, name))
	discardScratch(t, m, &run, name)
	format, err := m.DiskFormat(ctx, name)
	require.NoError(t, err)
	require.Equal(t, "asif", format)

	run, err = m.StartForeground(name)
	require.NoError(t, err)
	for range 10 {
		_, err = m.Images(ctx)
		require.NoError(t, err, "a listing while the ASIF VM starts")
		time.Sleep(time.Second)
	}
	select {
	case <-run.Done():
		t.Fatalf("tart run %s exited: %v", name, run.Err())
	case <-time.After(10 * time.Second):
	}
	exists, running, err := m.LocalVM(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && running, "it started, and is listed running")
	format, err = m.DiskFormat(ctx, name)
	require.NoError(t, err, "tart get answers while it runs")
	require.Equal(t, "asif", format)

	require.NoError(t, run.Stop(ctx, 30*time.Second))
	run = nil
}
