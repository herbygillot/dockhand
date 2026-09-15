package provision

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/tart"
)

func (n *native) WriteManifest(ctx context.Context, name string, manifest []byte) error {
	if _, err := n.guest(ctx, name, nil, "sudo", "-n", "/bin/mkdir", "-p", "/opt/dockhand"); err != nil {
		return err
	}
	_, err := n.guest(ctx, name, bytes.NewReader(append(manifest, '\n')), "sudo", "-n", "/usr/bin/tee", "/opt/dockhand/image.json")
	return err
}

func (n *native) Validate(ctx context.Context, name string, config Config) (validation, error) {
	if _, err := n.guest(ctx, name, nil, "sudo", "-n", "/usr/bin/true"); err != nil {
		return validation{}, fmt.Errorf("passwordless sudo is unavailable: %w", err)
	}
	foreign, err := macos.ForeignPackageManagers(ctx, n.target(name))
	if err != nil {
		return validation{}, err
	}
	if len(foreign) > 0 {
		return validation{}, fmt.Errorf("foreign package manager found: %s", strings.Join(foreign, ", "))
	}
	facts, err := installation.Inspect(ctx, n.target(name), config.GuestPrefix)
	if err != nil {
		return validation{}, err
	}
	if len(facts.Problems) > 0 {
		return validation{}, fmt.Errorf("MacPorts installation: %s", strings.Join(facts.Problems, "; "))
	}
	if len(facts.ActivePorts) > 0 {
		return validation{}, fmt.Errorf("prepared image has active ports: %s", strings.Join(facts.ActivePorts, ", "))
	}
	if err := installation.CheckTclPackages(ctx, n.target(name), config.GuestPrefix); err != nil {
		return validation{}, err
	}
	if err := macos.CheckCompiler(ctx, n.target(name)); err != nil {
		return validation{}, err
	}
	xcodeVersion, err := n.validateXcode(ctx, name, config)
	if err != nil {
		return validation{}, err
	}
	agentVersion, err := tart.ObserveGuestAgentVersion(ctx, func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, name, input, args...)
	})
	if err != nil {
		return validation{}, err
	}
	return validation{Platform: facts.Platform, MacPortsVersion: facts.Version, GuestAgentVersion: agentVersion, XcodeVersion: xcodeVersion}, nil
}

func (n *native) validateXcode(ctx context.Context, name string, config Config) (string, error) {
	tools, err := macos.InspectDeveloperTools(ctx, n.target(name))
	if err != nil {
		return "", err
	}
	if len(tools.Problems) != 0 {
		return "", fmt.Errorf("developer tools: %s", strings.Join(tools.Problems, "; "))
	}
	if config.XcodeVersion == "" {
		if !tools.CommandLineTools() {
			return "", fmt.Errorf("base image selects unexpected developer directory %s", tools.Directory)
		}
		return "", nil
	}
	if tools.Directory != "/Applications/Xcode.app/Contents/Developer" {
		return "", fmt.Errorf("Xcode image selects unexpected developer directory %s", tools.Directory)
	}
	if tools.XcodeVersion != config.XcodeVersion {
		return "", fmt.Errorf("image has Xcode %s; expected %s", tools.XcodeVersion, config.XcodeVersion)
	}
	return tools.XcodeVersion, nil
}
