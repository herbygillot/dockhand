package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrRefused is a guest that answered SSH and refused dockhand: its host
// key isn't the image's, or it doesn't take dockhand's key or password.
// Waiting longer, or trying again, doesn't change that.
var ErrRefused = errors.New("channel: the guest refused SSH")

// Commander runs a command on a guest, as a Guest does.
type Commander interface {
	Command(ctx context.Context, input io.Reader, args ...string) ([]byte, error)
}

// Running is a VM's run, which a wait for its guest watches: Done closes
// when it ends, and Err says how.
type Running interface {
	Done() <-chan struct{}
	Err() error
}

// refusals are what ssh says of a guest that answered and refused.
var refusals = []string{"Host key verification failed", "Permission denied"}

// AwaitSSH waits up to wait for a booted guest to accept SSH, trying every
// interval: macOS guests take a minute or two to reach sshd. Only a failed
// connection is waited out. A guest that answers and refuses ends the
// wait at once (ErrRefused), where the provider's own wait had retried
// for its whole length and dropped what ssh said, and the runner's
// attempts after it, about twelve minutes (the code-organization review's
// finding 7); so does any other failure, and the VM's run ending first,
// where vm watches one.
func AwaitSSH(ctx context.Context, guest Commander, vm Running, wait, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var done <-chan struct{}
	if vm != nil {
		done = vm.Done()
	}
	for {
		output, err := guest.Command(ctx, nil, "/usr/bin/true")
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrTransport) {
			return err
		}
		for _, refusal := range refusals {
			if strings.Contains(string(output), refusal) {
				return fmt.Errorf("%w: %s: %w", ErrRefused, refusal, err)
			}
		}
		select {
		case <-done:
			if vm.Err() != nil {
				return fmt.Errorf("the VM stopped before it accepted SSH: %w", vm.Err())
			}
			return errors.New("the VM stopped before it accepted SSH")
		case <-ctx.Done():
			return fmt.Errorf("the guest didn't accept SSH within %s: %w", wait, err)
		case <-time.After(interval):
		}
	}
}
