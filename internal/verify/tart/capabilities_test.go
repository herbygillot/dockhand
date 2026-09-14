package tart

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/stretchr/testify/require"
)

func TestCapabilityProblemAppliesTheAcceptedToolchainProfile(t *testing.T) {
	capabilities := record.EnvironmentCapabilities{
		Platform: testPlatform, MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsCommandLine,
	}
	value := state.ImageCapabilities{
		Provider: ProviderName, EnvironmentDigest: "sha256:image", CapabilityDigest: capabilityIdentity(capabilities), Capabilities: capabilities,
	}
	config := Config{GuestPrefix: "/opt/local"}
	accepted := record.BuildConfig{Provider: ProviderName, Platform: testPlatform, EnvironmentDigest: value.EnvironmentDigest, CapabilitiesRequired: true}
	require.Empty(t, capabilityProblem(value, config, accepted))
	accepted.NeedsXcode = true
	require.Contains(t, capabilityProblem(value, config, accepted), "requires full Xcode")
	value.Capabilities.DeveloperTools = record.DeveloperToolsXcode
	value.Capabilities.XcodeVersion = "26.0.1"
	value.CapabilityDigest = capabilityIdentity(value.Capabilities)
	require.Empty(t, capabilityProblem(value, config, accepted))
}

func TestManifestComparisonSupportsTheOriginalProvisionedFormat(t *testing.T) {
	capabilities := record.EnvironmentCapabilities{
		Platform: testPlatform, MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6",
		DeveloperTools: record.DeveloperToolsCommandLine, GuestAgentVersion: "0.14.1",
	}
	legacy := tartvm.ImageManifest{
		Protocol: 1, Source: "ghcr.io/cirruslabs/macos-tahoe-base:latest", Platform: testPlatform,
		MacPortsVersion: "2.12.6", GuestAgentVersion: "0.14.1",
	}
	require.Empty(t, manifestProblems(legacy, capabilities))
	current := legacy
	current.Protocol = tartvm.ImageManifestProtocol
	require.NotEmpty(t, manifestProblems(current, capabilities), "the current manifest records its prefix explicitly")
	current.MacPortsPrefix = "/opt/local"
	require.Empty(t, manifestProblems(current, capabilities))
}

func TestAgentDiagnosticsDoNotDetermineCompatibility(t *testing.T) {
	capabilities := record.EnvironmentCapabilities{Platform: testPlatform, MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsCommandLine, GuestAgentVersion: "development-snapshot"}
	identity := capabilityIdentity(capabilities)
	manifest := tartvm.ImageManifest{Protocol: tartvm.ImageManifestProtocol, Source: "source", Platform: testPlatform, MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", GuestAgentVersion: "different-release-tag"}
	require.Empty(t, manifestProblems(manifest, capabilities))
	legacy := state.ImageCapabilities{Provider: ProviderName, EnvironmentDigest: "image", Capabilities: capabilities, CapabilityDigest: legacyCapabilityIdentity(capabilities)}
	config := Config{GuestPrefix: "/opt/local"}
	accepted := record.BuildConfig{Provider: ProviderName, Platform: testPlatform, EnvironmentDigest: "image", CapabilityDigest: legacy.CapabilityDigest}
	require.Empty(t, capabilityProblem(legacy, config, accepted), "already accepted historical observations remain usable")
	capabilities.GuestAgentVersion = ""
	require.Equal(t, identity, capabilityIdentity(capabilities))
	require.Empty(t, manifestProblems(manifest, capabilities))
	legacy.Capabilities.MacPortsVersion = "different"
	require.Contains(t, capabilityProblem(legacy, config, accepted), "invalid identity")
}

func TestCachedAgentVersionFailuresAreReobserved(t *testing.T) {
	f, _ := singleRun(t)
	for _, test := range []struct {
		version, problem string
		cached           bool
	}{
		{"legacy-version", "", true},
		{"legacy-version", "image manifest does not match observed capabilities", false},
		{"", "declared guest agent is unavailable", false},
		{"", "MacPorts is unavailable", false},
		{"", "current MacPorts failure", true},
	} {
		value := state.ImageCapabilities{Provider: ProviderName, EnvironmentDigest: "fixture", Capabilities: record.EnvironmentCapabilities{GuestAgentVersion: test.version}, Problem: test.problem, ObservedAt: time.Now()}
		value.CapabilityDigest = legacyCapabilityIdentity(value.Capabilities)
		if test.problem == "current MacPorts failure" {
			value.CapabilityDigest = capabilityIdentity(value.Capabilities)
		}
		require.NoError(t, f.provider.State.PutImageCapabilities(t.Context(), value))
		got, found, err := f.provider.cachedImageCapabilities(t.Context(), "fixture")
		require.NoError(t, err)
		require.Equal(t, test.cached, found, test.problem)
		if found {
			require.Equal(t, value.CapabilityDigest, got.CapabilityDigest)
		}
	}
}
