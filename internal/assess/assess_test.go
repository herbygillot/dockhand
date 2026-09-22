package assess_test

import (
	"context"
	"go/build"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/stretchr/testify/require"
)

func TestValidationAndCancellationPrecedeIntegrations(t *testing.T) {
	t.Parallel()
	var service *assess.Service
	_, err := service.Assess(t.Context(), assess.Request{})
	require.ErrorContains(t, err, "select ports")
	request := assess.Request{Selection: survey.Selection{Ports: []string{"fixture"}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Assess(ctx, request)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.Assess(t.Context(), request)
	require.ErrorContains(t, err, "required")
}

func TestAssessmentCapabilityDependencies(t *testing.T) {
	t.Parallel()
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	const prefix = "github.com/herbygillot/dockhand/internal/"
	allowed := map[string]bool{"git": true, "macports": true, "macports/dependency": true, "macports/portedit": true, "macports/portindex": true, "macports/survey": true, "macports/version": true, "macports/workspace": true, "progress": true, "record": true, "upstream": true}
	for _, path := range pkg.Imports {
		if strings.Contains(strings.Split(path, "/")[0], ".") {
			require.True(t, strings.HasPrefix(path, prefix) && allowed[strings.TrimPrefix(path, prefix)], "assessment must not import app, workflow state, or concrete forge clients: %s", path)
		}
	}
}
