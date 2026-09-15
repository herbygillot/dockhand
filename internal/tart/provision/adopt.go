package provision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

type imageManager interface {
	Images(context.Context) (map[string]image, error)
	Clone(context.Context, string, string) error
	Rename(context.Context, string, string) error
	Delete(context.Context, string) error
}

// The caller holds the destination's write lock. Keep the previous image until
// its replacement is cloned, and restore it if cloning fails or is canceled.
func adopt(ctx context.Context, m imageManager, source, destination string, replace bool) error {
	if !replace {
		return m.Clone(ctx, source, destination)
	}
	previous := destination + "-previous"
	images, err := m.Images(ctx)
	if err != nil {
		return err
	}
	if images[destination].Running {
		return fmt.Errorf("setup: image %s must be stopped", destination)
	}
	if images[previous].Name != "" {
		return fmt.Errorf("setup: previous image %s remains from an interrupted replacement; preserve or restore it before rebuilding", previous)
	}
	if err := m.Rename(ctx, destination, previous); err != nil {
		return err
	}
	if err := m.Clone(ctx, source, destination); err != nil {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		rollback := m.Delete(recovery, destination)
		if rollback == nil {
			rollback = m.Rename(recovery, previous, destination)
		}
		if rollback != nil {
			return errors.Join(err, fmt.Errorf("setup: previous image retained at %s; restoring %s failed: %w", previous, destination, rollback))
		}
		return err
	}
	if err := m.Delete(ctx, previous); err != nil {
		return fmt.Errorf("setup: replacement is ready; previous image remains at %s: %w", previous, err)
	}
	return nil
}

type adoptionMachine struct {
	*native
	guard *os.File
}

func (m adoptionMachine) Clone(ctx context.Context, source, destination string) error {
	_, err := m.commandWithGuard(ctx, nil, true, m.guard, "clone", source, destination)
	return err
}
