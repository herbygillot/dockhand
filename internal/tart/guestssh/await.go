package guestssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
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
	// Only "No route to host" for a quarter of the wait, at most a minute,
	// with the guest's address routed to Tart's bridge, which rules out a
	// VPN, is Local Network privacy or a guest network that never came
	// up, which waiting longer doesn't change (#89).
	var unrouted time.Time
	early := min(wait/4, time.Minute)
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
			progress.VerboseReport(ctx, "waiting for SSH on the guest (%s elapsed): %s", time.Since(started).Round(time.Second), lastLine(output, err))
		}
		for _, refusal := range refusals {
			if strings.Contains(string(output), refusal) {
				return fmt.Errorf("%w: %s: %w", ErrRefused, refusal, err)
			}
		}
		if address := noRouteAddress(output); address == "" {
			unrouted = time.Time{}
		} else if unrouted.IsZero() {
			unrouted = time.Now()
		} else if router, ok := guest.(Router); ok && time.Since(unrouted) >= early {
			if iface, routeErr := router.Route(ctx, address); routeErr == nil && strings.HasPrefix(iface, "bridge") {
				return fmt.Errorf("the guest did not accept SSH after %s; the route to its address uses Tart's %s: %w%s", time.Since(started).Round(time.Second), iface, err, noRouteHint(guest, output, err))
			}
		}
		select {
		case <-done:
			if vm.Err() != nil {
				return fmt.Errorf("the VM stopped before it accepted SSH: %w", vm.Err())
			}
			return errors.New("the VM stopped before it accepted SSH")
		case <-ctx.Done():
			return fmt.Errorf("the guest didn't accept SSH within %s: %w%s", wait, err, noRouteHint(guest, output, err))
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

// Router reads which interface this Mac routes an address through, as
// route -n get says it: a Guest does.
type Router interface {
	Route(ctx context.Context, address string) (string, error)
}

// noRouteSaid is ssh's line for an address it found no route to.
var noRouteSaid = regexp.MustCompile(`connect to host (\S+) port \d+: No route to host`)

// noRouteAddress is the address ssh found no route to, or empty where it
// said something else.
func noRouteAddress(output []byte) string {
	if found := noRouteSaid.FindSubmatch(output); found != nil {
		return string(found[1])
	}
	return ""
}

// terminalApps are the apps TERM_PROGRAM names, as System Settings lists
// them under Local Network.
var terminalApps = map[string]string{"ghostty": "Ghostty", "Apple_Terminal": "Terminal", "iTerm.app": "iTerm2", "vscode": "Visual Studio Code"}

// terminalApp is the app dockhand runs from, by the TERM_PROGRAM its
// environment names, or "the app you ran dockhand from".
func terminalApp(env []string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "TERM_PROGRAM="); ok {
			if app, known := terminalApps[value]; known {
				return app
			}
		}
	}
	return "the app you ran dockhand from"
}

// noRouteHint says what "No route to host" means where the wait ended on
// it, most likely first: Local Network privacy, which holds Apple's
// /usr/bin/ssh too where the app it runs from is denied the local network
// (macOS 27.2 and Ghostty, #89), naming that app; a VPN or firewall
// routing Tart's network elsewhere; or a guest network that never came
// up. Empty for any other failure.
func noRouteHint(guest Commander, output []byte, err error) string {
	if !strings.Contains(string(output), "No route to host") && !strings.Contains(err.Error(), "No route to host") {
		return ""
	}
	var env []string
	if g, ok := guest.(*Guest); ok {
		env = g.Env
	}
	return fmt.Sprintf("; SSH reported no route to the guest. If Local Network access is disabled for %s, enable it in System Settings › Privacy & Security › Local Network and retry. A VPN or firewall may also block or redirect Tart's 192.168.64.0/24 network; inspect the route with route -n get <address>. The guest's network may also have failed to start", terminalApp(env))
}
