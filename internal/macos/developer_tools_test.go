package macos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectDeveloperToolsOnlyObservesTarget(t *testing.T) {
	for _, tc := range []struct{ directory, version string }{
		{"/Library/Developer/CommandLineTools", ""},
		{"/Applications/Xcode Custom.app/Contents/Developer", "26.0.1"},
	} {
		t.Run(tc.directory, func(t *testing.T) {
			var calls []string
			tools, err := InspectDeveloperTools(t.Context(), func(_ context.Context, input io.Reader, args ...string) ([]byte, error) {
				require.Nil(t, input)
				call := strings.Join(args, " ")
				calls = append(calls, call)
				switch call {
				case "/usr/bin/xcode-select -p":
					return []byte(tc.directory + "\n"), nil
				case "/usr/bin/xcodebuild -version":
					return []byte("Xcode " + tc.version + "\nBuild version fixture\n"), nil
				case "/usr/bin/xcrun --find clang":
					return []byte("/toolchain/clang\n"), nil
				default:
					t.Fatalf("unexpected command during observation: %s", call)
					return nil, nil
				}
			})
			require.NoError(t, err)
			require.Empty(t, tools.Problems)
			require.Equal(t, tc.directory, tools.Directory)
			require.Equal(t, tc.version, tools.XcodeVersion)
			require.Equal(t, tc.version == "", tools.CommandLineTools())
			require.Equal(t, tc.version != "", tools.Xcode())
			if tc.version == "" {
				require.NotContains(t, calls, "/usr/bin/xcodebuild -version")
			}
		})
	}
}

func TestInspectionSeparatesUnavailableToolsFromTransportFailure(t *testing.T) {
	exit := exec.Command("/bin/sh", "-c", "exit 1").Run()
	require.Error(t, exit)
	tools, err := InspectDeveloperTools(t.Context(), func(context.Context, io.Reader, ...string) ([]byte, error) { return nil, fmt.Errorf("probe: %w", exit) })
	require.NoError(t, err)
	require.Len(t, tools.Problems, 2)
	require.Contains(t, tools.Problems[0], "developer tools are unavailable")
	transport := errors.New("guest unreachable")
	_, err = InspectDeveloperTools(t.Context(), func(context.Context, io.Reader, ...string) ([]byte, error) { return nil, transport })
	require.ErrorIs(t, err, transport)
}

func TestInspectionRejectsMalformedXcodeVersion(t *testing.T) {
	tools, err := InspectDeveloperTools(t.Context(), func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		switch args[0] {
		case "/usr/bin/xcode-select":
			return []byte("/Applications/Xcode.app/Contents/Developer"), nil
		case "/usr/bin/xcodebuild":
			return []byte("unexpected output"), nil
		default:
			return []byte("/toolchain/clang"), nil
		}
	})
	require.NoError(t, err)
	require.Empty(t, tools.XcodeVersion)
	require.Len(t, tools.Problems, 1)
	require.Contains(t, tools.Problems[0], "unrecognized version")
}

func TestCanceledToolchainCheckDoesNotInstallAnything(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := EnsureCommandLineTools(ctx, func(context.Context, io.Reader, ...string) ([]byte, error) {
		calls++
		cancel()
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestInstallXcodePassesArchiveAsArgument(t *testing.T) {
	archive := "/private/tmp/Xcode custom $(ignored).xip"
	called := false
	err := InstallXcode(t.Context(), func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		called = true
		require.Equal(t, []string{"/bin/sh", "-c"}, args[:2])
		require.NotContains(t, args[2], archive)
		require.Equal(t, archive, args[len(args)-1])
		return nil, nil
	}, archive)
	require.NoError(t, err)
	require.True(t, called)
	err = InstallXcode(t.Context(), nil, "relative.xip")
	require.ErrorContains(t, err, "absolute")
}
