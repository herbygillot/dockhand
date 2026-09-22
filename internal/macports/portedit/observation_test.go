package portedit

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/stretchr/testify/require"
)

type countedObserver struct {
	*eval.Evaluator
	calls int
}

func (c *countedObserver) Observe(ctx context.Context, source macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	c.calls++
	return c.Evaluator.Observe(ctx, source, request)
}

func TestBaselineObservationReuseExcludesCandidatesAndFinalEvaluations(t *testing.T) {
	t.Parallel()
	s, _, input := probeFixture(t, "github.setup owner fixture 1.2.3 v")
	counted := &countedObserver{Evaluator: s.Ports.(*eval.Evaluator)}
	s.Ports = counted
	input.observe.Ports = counted
	mode := macports.ObservationRequest{Declarations: true}
	observe := func(contents []byte, mode macports.ObservationRequest, selected bool) {
		_, err := input.observe.One(t.Context(), contents, mode, selected)
		require.NoError(t, err)
	}
	observe(input.data, mode, true)
	observe(input.data, mode, true)
	require.Equal(t, 1, counted.calls)
	observe(input.data, mode, false)
	require.Equal(t, 2, counted.calls, "scope is part of the baseline key")
	candidate := append(append([]byte(nil), input.data...), []byte("\n# candidate\n")...)
	observe(candidate, mode, true)
	observe(candidate, mode, true)
	require.Equal(t, 4, counted.calls, "candidates are always observed afresh")
	observe(input.data, macports.ObservationRequest{}, true)
	observe(input.data, macports.ObservationRequest{}, true)
	require.Equal(t, 6, counted.calls, "final untraced evaluation must not use a cache")
	input.observe = input.observe.WithBaseline(candidate)
	observe(candidate, mode, true)
	require.Equal(t, 7, counted.calls, "a session for other baseline contents starts with no cache")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := input.observe.One(cancelled, candidate, mode, true)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 7, counted.calls)
}
