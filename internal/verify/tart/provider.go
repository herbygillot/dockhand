package tart

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/v2/internal/model"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

var ErrNotImplemented = errors.New("tart: provider is not implemented")

type Config struct {
	Executable        string
	Image             string
	ArtifactDirectory string
}

type Provider struct{ Config Config }

func (p *Provider) Capabilities(ctx context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{}, ErrNotImplemented
}
func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	return verify.Submission{}, ErrNotImplemented
}
func (p *Provider) Lookup(ctx context.Context, id model.RequestID) (verify.Lookup, error) {
	return verify.Lookup{State: verify.RunUnknown}, ErrNotImplemented
}
func (p *Provider) Observe(ctx context.Context, run model.ProviderRun) (verify.Observation, error) {
	return verify.Observation{}, ErrNotImplemented
}
func (p *Provider) Cancel(ctx context.Context, run model.ProviderRun) error {
	return ErrNotImplemented
}
func (p *Provider) Release(ctx context.Context, resource model.ResourceHandle) (verify.ReleaseResult, error) {
	return verify.ReleaseResult{}, ErrNotImplemented
}

var _ verify.Provider = (*Provider)(nil)
