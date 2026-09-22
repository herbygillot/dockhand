package tart

import (
	"context"
	"os"
	"syscall"
	"testing"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/stretchr/testify/require"
)

// Test-only helpers kept out of the provider's production surface.
func (p *Provider) settings() (Config, error) { return settings(p.Config) }

func (p *Provider) describeEnvironment(ctx context.Context) (Environment, error) {
	c, err := settings(p.Config)
	if err != nil {
		return Environment{}, err
	}
	progress.Report(ctx, "Inspecting Tart image %s", c.Image)
	return p.machineFor(c, nil).Environment(ctx)
}

// writeExecutable writes a fixture program while no fork can start. A child
// forked meanwhile for a parallel test's command inherits the write descriptor
// until it execs, and running the fixture then fails with ETXTBSY, "text file
// busy". Forks hold syscall.ForkLock for writing, so the read lock keeps every
// child from ever holding the descriptor.
func writeExecutable(t *testing.T, path, script string) {
	t.Helper()
	syscall.ForkLock.RLock()
	err := os.WriteFile(path, []byte(script), 0700)
	syscall.ForkLock.RUnlock()
	require.NoError(t, err)
}
