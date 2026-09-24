package provision

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

func (n *native) guest(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	return n.vm().Exec(ctx, name, tart.RunOptions{Input: input, Combined: true}, args...)
}

func (n *native) ReadyAgent(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var last error
	for {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := n.guest(call, name, nil, "/usr/bin/true")
		cancel()
		if err == nil {
			return host.CheckGuestTransport(ctx, func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
				return n.guest(ctx, name, input, args...)
			})
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
func (n *native) guestStream(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	return n.vm().Exec(ctx, name, tart.RunOptions{Input: input, Output: n.progress, Combined: true}, args...)
}
