package guestssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/progress"
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
	started, said := time.Now(), time.Time{}
	for {
		output, err := guest.Command(ctx, nil, "/usr/bin/true")
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrTransport) {
			return err
		}
		// What ssh says, at -v, on the first try and every half minute:
		// setup -vvvv waited four minutes saying nothing (#89).
		if said.IsZero() || time.Since(said) >= 30*time.Second {
			said = time.Now()
			progress.VerboseReport(ctx, "waiting for the guest's SSH (%s so far): %s", time.Since(started).Round(time.Second), lastLine(output, err))
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
			return fmt.Errorf("the guest didn't accept SSH within %s: %w%s", wait, err, noRouteHint(output, err))
		case <-time.After(interval):
		}
	}
}

// lastLine is the last line ssh wrote, or its error where it wrote none.
func lastLine(output []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if line := strings.TrimSpace(lines[len(lines)-1]); line != "" {
		return line
	}
	return err.Error()
}

// noRouteHint says what "No route to host" means where the wait ended on
// it: this Mac sent the guest's address somewhere other than Tart's
// network, as a VPN or a firewall routing its subnet does, or the guest's
// network never came up, or Local Network privacy, which decision 43 found
// doesn't hold Apple's /usr/bin/ssh, holding it on a newer macOS, as
// #89's 27.2 may; empty for any other failure (#89).
func noRouteHint(output []byte, err error) string {
	if !strings.Contains(string(output), "No route to host") && !strings.Contains(err.Error(), "No route to host") {
		return ""
	}
	return "; this Mac has no route to the guest's address: a VPN or firewall that routes Tart's 192.168.64.0/24 network elsewhere does this, which route -n get <address> shows; so does Local Network privacy where it holds the app dockhand runs from, which System Settings › Privacy & Security › Local Network allows; or the guest's network didn't come up"
}
