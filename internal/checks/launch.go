package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/procid"
	"github.com/herbygillot/dockhand/internal/store"
)

// launchLockPath is an attempt's launch lock: one file for each execution,
// in Launches, which every launch of the attempt and every child that can
// start work holds, and Stop takes (the lifecycle design's "Launching and
// stopping exclude each other").
func (c *Service) launchLockPath(id model.ExecutionID) string {
	return filelock.Path(c.Launches, string(id))
}

// launcher is what a launch lock's file records of the process that took
// it to launch, so a stop that can't have it names who may hold it.
type launcher struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

// Launch starts the attempt's work under its launch lock (buildenv.Build).
func (b *build) Launch(note string, start func(context.Context) (string, error)) error {
	return b.d.launch(b.ctx, []*build{b}, note, start)
}

// Launch starts a matrix run's work under every cell's launch lock: the
// cells share one launch, and a stop of any of them takes its own cell's
// lock, which the launcher holds with the rest.
func (m *matrixBuild) Launch(note string, start func(context.Context) (string, error)) error {
	builds := make([]*build, len(m.cells))
	for i, cell := range m.cells {
		builds[i] = cell.build
	}
	return m.d.launch(m.ctx, builds, note, start)
}

// launch takes each build's launch lock, reads again under it that the run
// is still this driver's and no cancel is recorded, saves note as each
// attempt's Launch, and only then runs start, with the locks' files carried
// for its children. A note start hands back changed is saved before the
// locks are let go. The locks are let go by closing this process's files
// alone, never unlocked: a child start forked, which holds the same open
// file, keeps the lock for as long as it lives. A cancel found recorded
// stops the driver, as its watch would, and nothing is started.
func (d *driver) launch(ctx context.Context, builds []*build, note string, start func(context.Context) (string, error)) error {
	var files []*os.File
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	self, err := procid.Self()
	if err != nil {
		return err
	}
	record, err := json.Marshal(launcher{PID: self.PID, Start: self.Start})
	if err != nil {
		return err
	}
	for _, b := range builds {
		file, err := filelock.Acquire(ctx, d.c.launchLockPath(b.execution.ID), filelock.Exclusive)
		if err != nil {
			return err
		}
		files = append(files, file)
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.WriteAt(record, 0); err != nil {
			return err
		}
	}
	refused := false
	if err := d.fenced(ctx, func(tx store.Tx) error {
		run, err := tx.Run(d.run.ID)
		if err != nil {
			return err
		}
		if run.CancelRequested != nil || run.State.Terminal() {
			refused = true
			return nil
		}
		for _, b := range builds {
			b.execution.Launch = note
			if err := tx.UpdateExecution(b.execution); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("%w: %w", buildenv.ErrNotLaunched, err)
	}
	if refused {
		d.canceled.Store(true)
		if d.stop != nil {
			d.stop()
		}
		return buildenv.ErrNotLaunched
	}
	carried := ctx
	for _, file := range files {
		carried = filelock.Carry(carried, file)
	}
	after, startErr := start(carried)
	if after != note {
		if err := d.fenced(ctx, func(tx store.Tx) error {
			for _, b := range builds {
				b.execution.Launch = after
				if err := tx.UpdateExecution(b.execution); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return errors.Join(startErr, err)
		}
	}
	return startErr
}

// launchLock is an attempt's launch lock as Stop takes it
// (buildenv.LaunchLock).
type launchLock struct{ path string }

func (l launchLock) Hold(ctx context.Context, within time.Duration) (func(), error) {
	bounded, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	file, err := filelock.Acquire(bounded, l.path, filelock.Exclusive)
	if err != nil {
		if ctx.Err() == nil && bounded.Err() != nil {
			return nil, fmt.Errorf("its launch lock is still held after %s, %s", within, holderWords(l.path))
		}
		return nil, err
	}
	// Unlocked as well as closed, so a child this process forks meanwhile
	// doesn't keep it from the next taker.
	return func() { _ = filelock.Release(file) }, nil
}

// holderWords say who may hold a launch lock, from what its launcher
// recorded: the launcher itself where it still runs, else a process it
// started.
func holderWords(path string) string {
	data, err := os.ReadFile(path)
	var recorded launcher
	if err != nil || json.Unmarshal(data, &recorded) != nil || recorded.PID == 0 {
		return "by a process that didn't record itself"
	}
	if alive, err := (procid.System{}).Alive(procid.Process{PID: recorded.PID, Start: recorded.Start}); err == nil && alive {
		return fmt.Sprintf("by the dockhand launching it, PID %d, which is still alive", recorded.PID)
	}
	return fmt.Sprintf("by a process that the dockhand which launched it, PID %d, now gone, started", recorded.PID)
}
