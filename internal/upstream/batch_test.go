package upstream_test

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

type batchProbe struct {
	port    macports.PortInfo
	single  int
	batches int
	values  []string
}

func (p *batchProbe) Port() macports.PortInfo { return p.port }
func (p *batchProbe) EvaluateVersion(_ context.Context, value string) (string, error) {
	p.single++
	return value, nil
}
func (p *batchProbe) EvaluateVersions(_ context.Context, values []string) ([]string, error) {
	p.batches++
	p.values = append([]string(nil), values...)
	return append([]string(nil), values...), nil
}

func TestBoundDiscoveryEvaluatesCandidatesInOneBatch(t *testing.T) {
	t.Parallel()
	c := &catalog{releases: []forge.Release{{Tag: "v1.9"}, {Tag: "v1.10"}, {Tag: "v1.11"}, {Tag: "v2.0", Prerelease: true}}}
	service := automaticService(t, c)
	probe := &batchProbe{port: automaticPort()}
	bound, err := service.Bind(probe)
	require.NoError(t, err)
	require.Nil(t, service.EvaluateVersions, "binding must not mutate the shared service")
	result, err := bound.Discover(t.Context())
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "1.11", result.CandidateVersion)
	require.Equal(t, 1, probe.batches)
	require.Zero(t, probe.single)
	require.ElementsMatch(t, []string{"1.9", "1.10", "1.11"}, probe.values)
}

func TestBoundDiscoveryFallsBackToSingleEvaluation(t *testing.T) {
	t.Parallel()
	c := &catalog{releases: []forge.Release{{Tag: "v1.9"}, {Tag: "v1.10"}}}
	service := automaticService(t, c)
	probe := &batchProbe{port: automaticPort()}
	bound, err := service.Bind(plainProbe{probe})
	require.NoError(t, err)
	result, err := bound.Discover(t.Context())
	require.NoError(t, err)
	require.Equal(t, "1.10", result.CandidateVersion)
	require.Zero(t, probe.batches)
	require.Equal(t, 2, probe.single)
}

// plainProbe exposes only the single-value evaluation.
type plainProbe struct{ inner *batchProbe }

func (p plainProbe) Port() macports.PortInfo { return p.inner.Port() }
func (p plainProbe) EvaluateVersion(ctx context.Context, value string) (string, error) {
	return p.inner.EvaluateVersion(ctx, value)
}
