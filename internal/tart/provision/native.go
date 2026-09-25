package provision

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

type native struct {
	config   Config
	progress io.Writer
	mu       sync.Mutex
	runs     map[string]*host.Foreground
	// saidBlocked records that setup has said it is waiting for the listing.
	saidBlocked bool
}

func newNative(config Config, progress io.Writer) *native {
	if progress != nil {
		progress = &progressWriter{writer: progress}
	}
	return &native{config: config, progress: progress, runs: map[string]*host.Foreground{}}
}

func (n *native) command(ctx context.Context, input io.Reader, stream bool, args ...string) ([]byte, error) {
	return n.commandWithGuard(ctx, input, stream, nil, args...)
}

func (n *native) commandWithGuard(ctx context.Context, input io.Reader, stream bool, guard *os.File, args ...string) ([]byte, error) {
	var output io.Writer
	if stream && n.progress != nil {
		output = n.progress
	}
	client := tart.Client{Executable: n.config.Executable, Home: n.config.Home}
	return client.Run(ctx, tart.RunOptions{Input: input, Output: output, Combined: true, ExtraFiles: []*os.File{guard}}, args...)
}

func (n *native) vm() host.Machine {
	return host.Machine{Client: tart.Client{Executable: n.config.Executable, Home: n.config.Home}, Blocked: n.waitListing}
}

// waitListing waits out a listing blocked by a running ASIF VM, the
// person's or a guest another setup left, saying so once per setup.
func (n *native) waitListing(ctx context.Context) error {
	n.mu.Lock()
	said := n.saidBlocked
	n.saidBlocked = true
	n.mu.Unlock()
	if !said && n.progress != nil {
		_, _ = fmt.Fprintln(n.progress, "A running VM with an ASIF disk keeps Tart from listing its VMs (openai/tart#1344); waiting for it to stop...")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", tart.ErrListingBlocked, ctx.Err())
	case <-time.After(listingRetry):
		return nil
	}
}

// listingRetry is how often setup asks again while the listing is blocked.
var listingRetry = 15 * time.Second
