package main

import (
	"context"
	"go/build"
	"strings"
	"testing"

	portsurvey "github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/stretchr/testify/require"
)

func TestValidationAndCancellationPrecedeIntegrations(t *testing.T) {
	t.Parallel()
	var service *Service
	_, err := service.Assess(t.Context(), Request{})
	require.ErrorContains(t, err, "select ports")
	request := Request{Selection: portsurvey.Selection{Ports: []string{"fixture"}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Assess(ctx, request)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.Assess(t.Context(), request)
	require.ErrorContains(t, err, "required")
}

// The survey assesses ports with MacPorts and upstream alone: never the
// dockhand command line, its engine or store, nor a concrete forge client.
func TestTheSurveyStaysOffDockhandItself(t *testing.T) {
	t.Parallel()
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	const prefix = "github.com/herbygillot/dockhand/internal/"
	for _, path := range pkg.Imports {
		for _, forbidden := range []string{"command", "engine", "store", "coord", "forge/github", "forge/gitlab"} {
			require.False(t, path == prefix+forbidden || strings.HasPrefix(path, prefix+forbidden+"/"), "the survey imports %s", path)
		}
	}
}
