package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
)

const ImageManifestProtocol = 2
const capabilityObservationProtocol = 1

// ImageManifest describes the environment declared by images created through setup.
// Runtime verification observes the same properties independently.
type ImageManifest struct {
	Protocol          int             `json:"protocol"`
	Source            string          `json:"source"`
	Platform          record.Platform `json:"platform"`
	MacPortsPrefix    string          `json:"macports_prefix,omitempty"`
	MacPortsVersion   string          `json:"macports_version"`
	GuestAgentVersion string          `json:"guest_agent_version"`
	XcodeVersion      string          `json:"xcode_version,omitempty"`
}

type capabilityInspection struct {
	Capabilities record.EnvironmentCapabilities
	Problem      string
}

func capabilityIdentity(capabilities record.EnvironmentCapabilities) string {
	raw, _ := json.Marshal(struct {
		Protocol     int
		Capabilities record.EnvironmentCapabilities
	}{capabilityObservationProtocol, capabilities})
	return "sha256:" + digest(raw)
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
	value, err := p.State.ImageCapabilities(ctx, ProviderName, environmentDigest)
	if errors.Is(err, state.ErrNotFound) {
		return state.ImageCapabilities{}, false, nil
	}
	if err == nil && value.CapabilityDigest != capabilityIdentity(value.Capabilities) {
		return state.ImageCapabilities{}, false, nil
	}
	return value, err == nil, err
}

func capabilityProblem(value state.ImageCapabilities, config Config, accepted record.BuildConfig) string {
	if value.Provider != ProviderName || value.EnvironmentDigest != accepted.EnvironmentDigest || value.CapabilityDigest != capabilityIdentity(value.Capabilities) {
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
	var manifest *ImageManifest
	if len(strings.TrimSpace(string(manifestRaw))) > 0 {
		value := ImageManifest{}
		if err := json.Unmarshal(manifestRaw, &value); err != nil {
			problems = append(problems, "image manifest is invalid: "+err.Error())
		} else {
			manifest = &value
		}
	}
	if manifest != nil && (manifest.Protocol == 1 || manifest.Protocol == ImageManifestProtocol) {
		manifestPrefix := manifest.MacPortsPrefix
		if manifest.Protocol == 1 && manifestPrefix == "" {
			manifestPrefix = "/opt/local"
		}
		if filepath.IsAbs(manifestPrefix) && filepath.Clean(manifestPrefix) == manifestPrefix {
			result.Capabilities.MacPortsPrefix = manifestPrefix
		}
	}
	if _, err = run(nil, "passwordless sudo is unavailable", "sudo", "-n", "/usr/bin/true"); err != nil {
		return result, err
	}
	foreign, err := run(nil, "checking foreign package managers", "/bin/sh", "-c", `for path in /opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg /etc/paths.d/homebrew /etc/paths.d/fink; do [ ! -e "$path" ] || printf '%s\n' "$path"; done`)
	if err != nil {
		return result, err
	}
	if found := strings.TrimSpace(string(foreign)); found != "" {
		problems = append(problems, "foreign package manager found: "+strings.ReplaceAll(found, "\n", ", "))
	}
	port := filepath.Join(result.Capabilities.MacPortsPrefix, "bin", "port")
	version, err := run(nil, "MacPorts is unavailable", port, "version")
	if err != nil {
		return result, err
	}
	fields := strings.Fields(string(version))
	if len(fields) >= 2 && fields[0] == "Version:" {
		result.Capabilities.MacPortsVersion = fields[1]
	} else if version != nil {
		problems = append(problems, "MacPorts returned an unrecognized version: "+strings.TrimSpace(string(version)))
	}
	installed, err := run(nil, "checking active ports", port, "-q", "installed", "active")
	if err != nil {
		return result, err
	}
	if active := strings.TrimSpace(string(installed)); active != "" {
		problems = append(problems, "image has active ports: "+strings.ReplaceAll(active, "\n", ", "))
	}
	tcl := `package require macports
mportinit
puts "$::macports::os_platform $::macports::os_major $::macports::build_arch"
`
	platform, err := run(strings.NewReader(tcl), "MacPorts platform evaluation failed", filepath.Join(result.Capabilities.MacPortsPrefix, "bin", "port-tclsh"))
	if err != nil {
		return result, err
	}
	platformFields := strings.Fields(string(platform))
	if len(platformFields) == 3 {
		result.Capabilities.Platform = record.Platform{OS: platformFields[0], Version: platformFields[1], Architecture: platformFields[2]}
	} else if platform != nil {
		problems = append(problems, "MacPorts returned an unrecognized platform: "+strings.TrimSpace(string(platform)))
	}
	selected, err := run(nil, "developer tools are unavailable", "/usr/bin/xcode-select", "-p")
	if err != nil {
		return result, err
	}
	developerDirectory := strings.TrimSpace(string(selected))
	switch {
	case developerDirectory == "/Library/Developer/CommandLineTools":
		result.Capabilities.DeveloperTools = record.DeveloperToolsCommandLine
	case strings.HasSuffix(developerDirectory, ".app/Contents/Developer") && filepath.IsAbs(developerDirectory):
		result.Capabilities.DeveloperTools = record.DeveloperToolsXcode
		xcode, err := run(nil, "Xcode is unavailable", "/usr/bin/xcodebuild", "-version")
		if err != nil {
			return result, err
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(xcode)), "\n")
		if version, ok := strings.CutPrefix(first, "Xcode "); ok && version != "" {
			result.Capabilities.XcodeVersion = version
		} else if xcode != nil {
			problems = append(problems, "xcodebuild returned an unrecognized version: "+strings.TrimSpace(string(xcode)))
		}
	default:
		if selected != nil {
			problems = append(problems, "unexpected developer directory: "+developerDirectory)
		}
	}
	if _, err = run(nil, "compiler is unavailable", "/usr/bin/xcrun", "--find", "clang"); err != nil {
		return result, err
	}
	if manifest != nil {
		agent, err := run(nil, "declared guest agent is unavailable", "/opt/dockhand/bin/tart-guest-agent", "--version")
		if err != nil {
			return result, err
		}
		agentFields := strings.Fields(string(agent))
		if len(agentFields) == 3 && agentFields[0] == "tart-guest-agent" && agentFields[1] == "version" {
			result.Capabilities.GuestAgentVersion = agentFields[2]
		} else if agent != nil {
			problems = append(problems, "guest agent returned an unrecognized version: "+strings.TrimSpace(string(agent)))
		}
		problems = append(problems, manifestProblems(*manifest, result.Capabilities)...)
	}
	result.Problem = strings.Join(problems, "; ")
	return result, nil
}

func manifestProblems(manifest ImageManifest, capabilities record.EnvironmentCapabilities) []string {
	problems := []string{}
	if manifest.Protocol != 1 && manifest.Protocol != ImageManifestProtocol {
		return []string{fmt.Sprintf("image manifest uses unsupported protocol %d", manifest.Protocol)}
	}
	prefix := manifest.MacPortsPrefix
	if manifest.Protocol == 1 && prefix == "" {
		prefix = "/opt/local"
	}
	if manifest.Source == "" || manifest.Platform != capabilities.Platform || prefix != capabilities.MacPortsPrefix || manifest.MacPortsVersion != capabilities.MacPortsVersion || manifest.XcodeVersion != capabilities.XcodeVersion || manifest.GuestAgentVersion != capabilities.GuestAgentVersion {
		problems = append(problems, "image manifest does not match observed capabilities")
	}
	return problems
}

func newImageCapabilities(environmentDigest string, inspection capabilityInspection) state.ImageCapabilities {
	return state.ImageCapabilities{
		Provider: ProviderName, EnvironmentDigest: environmentDigest,
		CapabilityDigest: capabilityIdentity(inspection.Capabilities), Capabilities: inspection.Capabilities,
		Problem: inspection.Problem, ObservedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}
