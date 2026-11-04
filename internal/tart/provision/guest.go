package provision

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/tart/guestssh"
)

// guest runs a command in a guest reached over SSH, returning its output.
func (n *native) guest(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	guest, err := n.guestFor(name)
	if err != nil {
		return nil, err
	}
	return guest.Command(ctx, input, args...)
}

// guestStream runs a command as guest does, showing its output as it
// arrives.
func (n *native) guestStream(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	guest, err := n.guestFor(name)
	if err != nil {
		return nil, err
	}
	streaming := *guest
	streaming.Output = n.progress
	return streaming.Command(ctx, input, args...)
}

// ReadyAgent waits for the guest to answer over SSH, as every check
// reaches one (guestssh.AwaitSSH), with dockhand's key in place: setup's
// readiness probe, where it asked the Tart guest agent with tart exec,
// the last use of it (#1346). A VM whose tart run ended stops the wait.
func (n *native) ReadyAgent(ctx context.Context, name string) error {
	guest, err := n.guestFor(name)
	if err != nil {
		return err
	}
	var running guestssh.Running
	if run := n.run(name); run != nil {
		running = run
	}
	if err := guestssh.AwaitSSH(ctx, guest, running, 5*time.Minute, time.Second); err != nil {
		return fmt.Errorf("setup: %s didn't answer over SSH: %w", name, err)
	}
	return nil
}

func (n *native) target(name string) macos.Command {
	return func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, name, input, args...)
	}
}

func (n *native) streamTarget(name string) macos.Command {
	return func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guestStream(ctx, name, input, args...)
	}
}
