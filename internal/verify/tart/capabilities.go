package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/verify"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

const capabilityObservationProtocol = 2

type capabilityInspection struct {
	Capabilities record.EnvironmentCapabilities
	Problem      string
}

func capabilityIdentity(capabilities record.EnvironmentCapabilities) string {
	capabilities.GuestAgentVersion = ""
	return observedCapabilityIdentity(capabilityObservationProtocol, capabilities)
}

// Preserve valid historical fingerprints so running jobs can still collect
// their results. New observations exclude diagnostic agent version strings.
func legacyCapabilityIdentity(capabilities record.EnvironmentCapabilities) string {
	return observedCapabilityIdentity(1, capabilities)
}

func observedCapabilityIdentity(protocol int, capabilities record.EnvironmentCapabilities) string {
	raw, _ := json.Marshal(struct {
		Protocol     int
		Capabilities record.EnvironmentCapabilities
	}{protocol, capabilities})
	return "sha256:" + record.Digest(raw)
}

func environmentEvidence(value state.ImageCapabilities) *record.EnvironmentEvidence {
	return &record.EnvironmentEvidence{
		Provider: value.Provider, EnvironmentDigest: value.EnvironmentDigest,
		CapabilityDigest: value.CapabilityDigest, Capabilities: value.Capabilities,
	}
}

func (p *Provider) cachedImageCapabilities(ctx context.Context, environmentDigest string) (state.ImageCapabilities, bool, error) {
	if p.State == nil {
		return state.ImageCapabilities{}, false, nil
	}
	value, err := p.State.ImageCapabilities(ctx, verify.ProviderTart, environmentDigest)
	if errors.Is(err, state.ErrNotFound) {
		return state.ImageCapabilities{}, false, nil
	}
	if err == nil && !validCapabilityIdentity(value) {
		return state.ImageCapabilities{}, false, nil
	}
	if err == nil && value.Problem != "" && value.CapabilityDigest != capabilityIdentity(value.Capabilities) {
		return state.ImageCapabilities{}, false, nil
	}
	return value, err == nil, err
}

func validCapabilityIdentity(value state.ImageCapabilities) bool {
	return value.CapabilityDigest == capabilityIdentity(value.Capabilities) || value.CapabilityDigest == legacyCapabilityIdentity(value.Capabilities)
}

func capabilityProblem(value state.ImageCapabilities, config Config, accepted record.BuildConfig) string {
	if value.Provider != verify.ProviderTart || value.EnvironmentDigest != accepted.EnvironmentDigest || !validCapabilityIdentity(value) {
		return "environment capability observation has invalid identity"
	}
	if accepted.CapabilityDigest != "" && accepted.CapabilityDigest != value.CapabilityDigest {
		return "environment capabilities changed after the build was accepted"
	}
	if value.Problem != "" {
		return value.Problem
	}
	capabilities := value.Capabilities
	if capabilities.Platform != accepted.Platform {
		return fmt.Sprintf("image platform is %+v; requested %+v", capabilities.Platform, accepted.Platform)
	}
	if capabilities.MacPortsPrefix != config.GuestPrefix {
		return fmt.Sprintf("image MacPorts prefix is %s; requested %s", capabilities.MacPortsPrefix, config.GuestPrefix)
	}
	if capabilities.MacPortsVersion == "" {
		return "image has no usable MacPorts installation"
	}
	if capabilities.DeveloperTools != record.DeveloperToolsCommandLine && capabilities.DeveloperTools != record.DeveloperToolsXcode {
		return "image has no usable developer tools"
	}
	if accepted.NeedsXcode && capabilities.DeveloperTools != record.DeveloperToolsXcode {
		return "port requires full Xcode but the image provides only command line tools"
	}
	return ""
}

func (n *native) InspectCapabilities(ctx context.Context, vm, prefix string) (capabilityInspection, error) {
	result := capabilityInspection{Capabilities: record.EnvironmentCapabilities{MacPortsPrefix: prefix}}
	problems := []string{}
	run := func(input io.Reader, label string, args ...string) ([]byte, error) {
		out, err := n.guest(ctx, vm, input, args...)
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, err
		}
		problems = append(problems, label+": "+err.Error())
		return nil, nil
	}

	manifestRaw, err := run(nil, "reading image manifest", "/bin/sh", "-c", "if [ -f /opt/dockhand/image.json ]; then exec /bin/cat /opt/dockhand/image.json; fi")
	if err != nil {
		return result, err
	}
	var manifest *tartvm.ImageManifest
	if len(strings.TrimSpace(string(manifestRaw))) > 0 {
		value := tartvm.ImageManifest{}
		if err := json.Unmarshal(manifestRaw, &value); err != nil {
			problems = append(problems, "image manifest is invalid: "+err.Error())
		} else {
			manifest = &value
		}
	}
	if manifest != nil && (manifest.Protocol == 1 || manifest.Protocol == tartvm.ImageManifestProtocol) {
		manifestPrefix := manifest.MacPortsPrefix
		if manifest.Protocol == 1 && manifestPrefix == "" {
			manifestPrefix = macports.DefaultPrefix
		}
		if filepath.IsAbs(manifestPrefix) && filepath.Clean(manifestPrefix) == manifestPrefix {
			result.Capabilities.MacPortsPrefix = manifestPrefix
		}
	}
	if _, err = run(nil, "passwordless sudo is unavailable", "sudo", "-n", "/usr/bin/true"); err != nil {
		return result, err
	}
	command := macos.Command(func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, vm, input, args...)
	})
	foreign, err := macos.ForeignPackageManagers(ctx, func(_ context.Context, input io.Reader, args ...string) ([]byte, error) {
		return run(input, "checking foreign package managers", args...)
	})
	if err != nil {
		return result, err
	}
	if len(foreign) > 0 {
		problems = append(problems, "foreign package manager found: "+strings.Join(foreign, ", "))
	}
	facts, err := installation.Inspect(ctx, command, result.Capabilities.MacPortsPrefix)
	if err != nil {
		return result, err
	}
	result.Capabilities.MacPortsVersion = facts.Version
	result.Capabilities.Platform = facts.Platform
	problems = append(problems, facts.Problems...)
	if len(facts.ActivePorts) > 0 {
		problems = append(problems, "image has active ports: "+strings.Join(facts.ActivePorts, ", "))
	}
	tools, err := macos.InspectDeveloperTools(ctx, func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, vm, input, args...)
	})
	if err != nil {
		return result, err
	}
	problems = append(problems, tools.Problems...)
	if tools.CommandLineTools() {
		result.Capabilities.DeveloperTools = record.DeveloperToolsCommandLine
	}
	if tools.Xcode() {
		result.Capabilities.DeveloperTools = record.DeveloperToolsXcode
	}
	result.Capabilities.XcodeVersion = tools.XcodeVersion
	if manifest != nil {
		problems = append(problems, manifestProblems(*manifest, result.Capabilities)...)
	}
	result.Problem = strings.Join(problems, "; ")
	return result, nil
}

func manifestProblems(manifest tartvm.ImageManifest, capabilities record.EnvironmentCapabilities) []string {
	problems := []string{}
	if manifest.Protocol != 1 && manifest.Protocol != tartvm.ImageManifestProtocol {
		return []string{fmt.Sprintf("image manifest uses unsupported protocol %d", manifest.Protocol)}
	}
	prefix := manifest.MacPortsPrefix
	if manifest.Protocol == 1 && prefix == "" {
		prefix = macports.DefaultPrefix
	}
	if manifest.Source == "" || manifest.Platform != capabilities.Platform || prefix != capabilities.MacPortsPrefix || manifest.MacPortsVersion != capabilities.MacPortsVersion || manifest.XcodeVersion != capabilities.XcodeVersion {
		problems = append(problems, "image manifest does not match observed capabilities")
	}
	return problems
}

func newImageCapabilities(environmentDigest string, inspection capabilityInspection) state.ImageCapabilities {
	return state.ImageCapabilities{
		Provider: verify.ProviderTart, EnvironmentDigest: environmentDigest,
		CapabilityDigest: capabilityIdentity(inspection.Capabilities), Capabilities: inspection.Capabilities,
		Problem: inspection.Problem, ObservedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}
