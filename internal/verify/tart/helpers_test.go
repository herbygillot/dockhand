package tart

import (
	"context"

	"github.com/herbygillot/dockhand/internal/progress"
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
