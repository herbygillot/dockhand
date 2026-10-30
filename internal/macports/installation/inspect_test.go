package installation

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
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
			return []byte("dockhand-platform: darwin 21 arm64\n"), nil
		default:
			t.Fatalf("unexpected modifying command: %v", args)
			return nil, nil
		}
	}, "/custom")
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.Equal(t, "2.12.6", facts.Version)
	require.Equal(t, model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, facts.Platform)
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
	require.Equal(t, model.Platform{}, facts.Platform)
}

// port version says its version after a warning once the port definitions
// are more than two weeks old, which setup tart read as no version, and
// refused a sound image (rc1's full stage).
func TestParseVersionReadsPastAWarning(t *testing.T) {
	t.Parallel()
	version, ok := ParseVersion([]byte("Warning: port definitions are more than two weeks old, consider updating them by running 'port selfupdate'.\nVersion: 2.12.6\n"))
	require.True(t, ok)
	require.Equal(t, "2.12.6", version)
	version, ok = ParseVersion([]byte("Version: 2.11.5\n"))
	require.True(t, ok)
	require.Equal(t, "2.11.5", version)
	_, ok = ParseVersion([]byte("port: command not found\n"))
	require.False(t, ok)
}

// The guest reads stderr with stdout, so every probe's answer may follow
// mportinit's stale-definitions warning, which failed setup tart's image
// on its version, its platform, and its active ports (rc1's full stage).
func TestInspectReadsPastTheStaleDefinitionsWarning(t *testing.T) {
	t.Parallel()
	const warning = "Warning: port definitions are more than two weeks old, consider updating them by running 'port selfupdate'.\n"
	facts, err := Inspect(t.Context(), func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "/opt/local/bin/port version":
			return []byte(warning + "Version: 2.12.6\n"), nil
		case "/opt/local/bin/port -q installed active":
			return []byte(warning), nil
		default:
			return []byte(warning + "dockhand-platform: darwin 25 arm64\n"), nil
		}
	}, "/opt/local")
	require.NoError(t, err)
	require.Empty(t, facts.Problems)
	require.Equal(t, "2.12.6", facts.Version)
	require.Equal(t, model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, facts.Platform)
	require.Empty(t, facts.ActivePorts)
}
