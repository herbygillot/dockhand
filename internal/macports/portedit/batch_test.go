package portedit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateVersionsMatchesSequentialEvaluation(t *testing.T) {
	service, request, input := probeFixture(t, "set release 12\ngithub.setup owner fixture $release v\nversion [expr {${github.version} * 10 + 7}]\n")
	probe, err := service.Probe(t.Context(), ProbeSource{Source: request.Source, Root: request.Root, Selection: request.Selection, Platform: request.Platform})
	require.NoError(t, err)
	values := []string{"13", "14", "20"}
	var sequential []string
	for _, value := range values {
		version, err := probe.EvaluateVersion(t.Context(), value)
		require.NoError(t, err)
		sequential = append(sequential, version)
	}
	require.Equal(t, []string{"137", "147", "207"}, sequential)
	batched, err := probe.EvaluateVersions(t.Context(), values)
	require.NoError(t, err)
	require.Equal(t, sequential, batched)
	single, err := probe.EvaluateVersions(t.Context(), []string{"15"})
	require.NoError(t, err)
	require.Equal(t, []string{"157"}, single)
	original, err := os.ReadFile(filepath.Join(request.Root, "devel/fixture/Portfile"))
	require.NoError(t, err)
	require.Equal(t, input.data, original, "probing restores the Portfile")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = probe.EvaluateVersions(ctx, values)
	require.ErrorIs(t, err, context.Canceled)
}
