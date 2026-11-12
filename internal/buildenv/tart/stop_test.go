package tart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// lock is a launch lock, held where held is set.
type lock struct {
	held  error
	taken bool
}

func (l *lock) Hold(context.Context, time.Duration) (func(), error) {
	if l.held != nil {
		return nil, l.held
	}
	l.taken = true
	return func() {}, nil
}

func stopping(t *testing.T) (*fakeMac, *Provider) {
	t.Helper()
	mac := newMac()
	p := testProvider(mac)
	p.Tart = tartvm.Client{Home: filepath.Join(t.TempDir(), ".dockhand", "tart"), Env: os.Environ()}
	require.NoError(t, os.MkdirAll(p.Tart.Home, 0o700))
	return mac, p
}

func attempt(clone string) model.GuestExecution {
	return model.GuestExecution{ID: "tart_1", Attempt: 1, ProviderRef: clone, Launch: launchNote{Clone: clone}.String()}
}

// A cancel of a Tart check stops the clone the attempt noted, then, holding
// its launch lock, which the clone's tart run holds until the VM is gone,
// deletes it and forgets its marker: ended. A clone gone already is ended
// too, and one an attempt named and never made. A lock still held, a stop
// or a delete that fails, is not known, with Tart's words; a clone a
// finished attempt kept for shell is left, and said (the lifecycle design's
// "Tart").
func TestATartStopRemovesTheClonesWorkAndLeavesAKeptOne(t *testing.T) {
	t.Parallel()
	clone := clonePrefix("run-1", "tahoe") + "-1"

	mac, p := stopping(t)
	mac.images = append(mac.images, clone)
	held := &lock{}
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: held})
	require.NoError(t, err)
	require.Empty(t, left)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.events, "stopped, then deleted")
	require.True(t, held.taken, "deleted holding the lock")
	require.NotContains(t, mac.images, clone)

	// The clone named and never made, or gone already: ended all the same.
	mac, p = stopping(t)
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err)
	// Nothing named at all: nothing was cloned, once the lock is had.
	nothing := &lock{}
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{ID: "tart_2"}, Lock: nothing})
	require.NoError(t, err)
	require.True(t, nothing.taken)

	// A tart run that still holds the lock: the VM was asked to stop, and
	// isn't known to be gone.
	mac, p = stopping(t)
	mac.images = append(mac.images, clone)
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{held: errors.New("its launch lock is still held after 1m0s, by a process that the dockhand which launched it, PID 7, now gone, started")}})
	require.ErrorContains(t, err, clone+" was asked to stop, and its launch lock is still held")
	require.Contains(t, mac.images, clone, "not deleted while something holds the lock")

	mac.stopErr = errors.New("tart: the VM didn't stop")
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.ErrorContains(t, err, "tart couldn't stop "+clone+": tart: the VM didn't stop")
	mac.stopErr, mac.deleteErr = nil, errors.New("tart: disk busy")
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.ErrorContains(t, err, "tart couldn't delete "+clone+": tart: disk busy")

	// A clone kept for shell is no unfinished work: left, and said.
	mac, p = stopping(t)
	mac.images = append(mac.images, clone)
	require.NoError(t, p.keepClone(clone, "dockhand-base-tahoe", tartvm.KeptFailed))
	left, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err)
	require.Equal(t, []string{"the clone " + clone + ", kept for dockhand shell, which dockhand clean removes"}, left)
	require.Empty(t, mac.events, "neither stopped nor deleted")

	// A name that isn't a check's clone is never touched.
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt("dockhand-base-tahoe"), Lock: &lock{}})
	require.ErrorContains(t, err, "dockhand-base-tahoe is not a check's clone")
}
