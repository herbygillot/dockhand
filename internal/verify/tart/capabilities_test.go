package tart

import (
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
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
	legacy := ImageManifest{
		Protocol: 1, Source: "ghcr.io/cirruslabs/macos-tahoe-base:latest", Platform: testPlatform,
		MacPortsVersion: "2.12.6", GuestAgentVersion: "0.14.1",
	}
	require.Empty(t, manifestProblems(legacy, capabilities))
	current := legacy
	current.Protocol = ImageManifestProtocol
	require.NotEmpty(t, manifestProblems(current, capabilities), "the current manifest records its prefix explicitly")
	current.MacPortsPrefix = "/opt/local"
	require.Empty(t, manifestProblems(current, capabilities))
}
