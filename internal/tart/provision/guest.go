package provision

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/tart"
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

// ReadyAgent waits for the Tart guest agent to answer `tart exec`, which
// verification relies on to mark each clone it reaches. Nothing read from
// the guest depends on it: that goes over SSH.
func (n *native) ReadyAgent(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var last error
	for {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := n.vm().Exec(call, name, tart.RunOptions{Combined: true}, "/usr/bin/true")
		cancel()
		if err == nil {
			return nil
		}
		last = err
		if run := n.run(name); run != nil {
			select {
			case <-run.Done():
				runErr := run.Err()
				if runErr == nil {
					runErr = fmt.Errorf("tart: VM %s stopped before its guest agent became ready", name)
				}
				return runErr
			default:
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("tart: guest agent in %s did not become ready: %w; last probe: %v", name, ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
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
