package upstream

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// VersionProbe evaluates a source version against one captured Portfile.
type VersionProbe interface {
	Port() macports.PortInfo
	EvaluateVersion(context.Context, string) (string, error)
}

// Discovery binds catalog selection to a source-specific version evaluator.
// It owns neither a Git checkout nor a workflow job.
type Discovery struct {
	service Service
	port    macports.PortInfo
}

func (s *Service) Bind(probe VersionProbe) (*Discovery, error) {
	if s == nil || probe == nil {
		return nil, fmt.Errorf("upstream: service and source version probe are required")
	}
	bound := *s
	bound.EvaluateVersion = probe.EvaluateVersion
	bound.EvaluateVersions = nil
	if batch, ok := probe.(interface {
		EvaluateVersions(context.Context, []string) ([]string, error)
	}); ok {
		bound.EvaluateVersions = batch.EvaluateVersions
	}
	return &Discovery{service: bound, port: probe.Port()}, nil
}
func (d *Discovery) Discover(ctx context.Context) (Result, error) {
	return d.service.DiscoverPort(ctx, d.port)
}
func (d *Discovery) Resolve(ctx context.Context, requested string) (record.Release, error) {
	return d.service.Resolve(ctx, d.port, requested)
}
