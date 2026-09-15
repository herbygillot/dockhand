package installation

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestInspectReportsFactsWithoutEnforcingImagePolicy(t *testing.T) {
	calls := 0
	facts, err := Inspect(t.Context(), func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		calls++
		switch strings.Join(args, " ") {
		case "/custom/bin/port version":
			return []byte("Version: 2.12.6\n"), nil
		case "/custom/bin/port -q installed active":
			return []byte("fixture @1.0_0 (active)\n"), nil
		case "/custom/bin/port-tclsh":
			script, err := io.ReadAll(input)
			require.NoError(t, err)
			require.Contains(t, string(script), "package require macports")
			require.NotContains(t, string(script), "install")
			return []byte("darwin 21 arm64\n"), nil
		default:
			t.Fatalf("unexpected modifying command: %v", args)
			return nil, nil
		}
	}, "/custom")
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.Equal(t, "2.12.6", facts.Version)
	require.Equal(t, record.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, facts.Platform)
	require.Equal(t, []string{"fixture @1.0_0 (active)"}, facts.ActivePorts)
	require.Empty(t, facts.Problems)
}

func TestInspectDistinguishesProbeFailuresFromTransportFailure(t *testing.T) {
	facts, err := Inspect(t.Context(), func(context.Context, io.Reader, ...string) ([]byte, error) { return nil, &exec.ExitError{} }, "/opt/local")
	require.NoError(t, err)
	require.Len(t, facts.Problems, 3)
	failure := errors.New("guest transport unavailable")
	_, err = Inspect(t.Context(), func(context.Context, io.Reader, ...string) ([]byte, error) { return nil, failure }, "/opt/local")
	require.ErrorIs(t, err, failure)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Inspect(ctx, func(context.Context, io.Reader, ...string) ([]byte, error) { return nil, &exec.ExitError{} }, "/opt/local")
	require.ErrorIs(t, err, context.Canceled)
}

func TestInspectRejectsMalformedFacts(t *testing.T) {
	facts, err := Inspect(t.Context(), func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "-q" {
			return nil, nil
		}
		return []byte("unexpected output"), nil
	}, "/opt/local")
	require.NoError(t, err)
	require.Len(t, facts.Problems, 2)
	require.Empty(t, facts.Version)
	require.Equal(t, record.Platform{}, facts.Platform)
}
