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
	tahoe, err := ReleaseForDarwin(25)
	require.NoError(t, err)
	err = EnsureCommandLineTools(ctx, func(context.Context, io.Reader, ...string) ([]byte, error) {
		calls++
		cancel()
		return nil, ctx.Err()
	}, tahoe)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

// Setup installs the newest tools of the release's generation, never the
// newest offered: sorting the offers gave the Tahoe images the macOS 27
// tools (decision 13). Labels are Software Update's, old and new forms.
func TestCommandLineToolsLabelKeepsTheReleasesGeneration(t *testing.T) {
	tahoe, err := ReleaseForDarwin(25)
	require.NoError(t, err)
	offered := []string{"Command Line Tools for Xcode 27.0-27.0", "Command Line Tools for Xcode 26.6-26.6", "Command Line Tools for Xcode 26.10-26.10", "Command Line Tools for Xcode 26.4-26.4"}
	label, err := CommandLineToolsLabel(offered, tahoe)
	require.NoError(t, err)
	require.Equal(t, "Command Line Tools for Xcode 26.10-26.10", label, "versions compare numerically")

	monterey, err := ReleaseForDarwin(21)
	require.NoError(t, err)
	label, err = CommandLineToolsLabel([]string{"Command Line Tools for Xcode-14.2", "Command Line Tools for Xcode-13.4", "Command Line Tools (macOS Monterey version 12.3) for Xcode-14.0"}, monterey)
	require.NoError(t, err)
	require.Equal(t, "Command Line Tools for Xcode-14.2", label)

	_, err = CommandLineToolsLabel([]string{"Command Line Tools for Xcode 27.0-27.0"}, tahoe)
	require.ErrorContains(t, err, "offers no Command Line Tools 26, the generation Tahoe uses; offered: Command Line Tools for Xcode 27.0-27.0")
	_, err = CommandLineToolsLabel(nil, tahoe)
	require.ErrorContains(t, err, "offered: none")
}

// The tools already in a guest are held to the release's generation too,
// read from their package receipt.
func TestInstalledToolsOfAnotherGenerationAreRefused(t *testing.T) {
	tahoe, err := ReleaseForDarwin(25)
	require.NoError(t, err)
	receipt := "package-id: com.apple.pkg.CLTools_Executables\nversion: 27.0.0.0.1.1757719676\nvolume: /\n"
	var installed bool
	run := func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		if len(args) > 2 && strings.Contains(args[2], "pkgutil") {
			return []byte(receipt), nil
		}
		if len(args) > 2 && strings.Contains(args[2], "softwareupdate --install") {
			installed = true
		}
		return nil, nil
	}
	err = EnsureCommandLineTools(t.Context(), run, tahoe)
	require.ErrorContains(t, err, "guest has Command Line Tools 27.0; Tahoe uses generation 26")
	require.False(t, installed)
	receipt = "version: 26.6.0.0.1.1757719676\n"
	require.NoError(t, EnsureCommandLineTools(t.Context(), run, tahoe))
	version, err := CommandLineToolsVersion(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, "26.6", version)
}

// Without a compiler, setup lists the offers with the marker that makes
// Software Update offer the tools, installs the generation's newest, and
// removes the marker.
func TestCommandLineToolsInstallTheGenerationsNewestOffer(t *testing.T) {
	tahoe, err := ReleaseForDarwin(25)
	require.NoError(t, err)
	var compiler, removedMarker bool
	var installedLabel string
	run := func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		switch {
		case len(args) > 2 && strings.Contains(args[2], "xcode-select -p") && !compiler:
			return nil, errors.New("no developer tools")
		case args[0] == "sudo" && args[len(args)-1] == commandLineToolsMarker:
			removedMarker = true
		case len(args) > 3 && strings.Contains(args[2], "softwareupdate --list"):
			require.Equal(t, commandLineToolsMarker, args[4])
			return []byte("Command Line Tools for Xcode 27.0-27.0\nCommand Line Tools for Xcode 26.6-26.6\n"), nil
		case len(args) > 3 && strings.Contains(args[2], "softwareupdate --install"):
			installedLabel, compiler = args[4], true
		case len(args) > 2 && strings.Contains(args[2], "pkgutil"):
			return []byte("version: 26.6.0.0.1.1757719676\n"), nil
		}
		return nil, nil
	}
	require.NoError(t, EnsureCommandLineTools(t.Context(), run, tahoe))
	require.Equal(t, "Command Line Tools for Xcode 26.6-26.6", installedLabel)
	require.True(t, removedMarker)
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
