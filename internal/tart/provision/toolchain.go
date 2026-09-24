package provision

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/tart"
)

func (n *native) EnsureToolchain(ctx context.Context, name string) error {
	release, err := tart.ReleaseForPlatform(n.config.Platform)
	if err != nil {
		return err
	}
	return n.during(ctx, "Command line tools", func() error {
		return macos.EnsureCommandLineTools(ctx, n.streamTarget(name), release)
	})
}

func (n *native) InstallXcode(ctx context.Context, name string, config Config) error {
	if err := n.during(ctx, "Preparing guest Xcode storage", func() error {
		return macos.EnsureAPFSSpace(ctx, n.target(name), "/private/tmp", "disk0", "disk0s2", macos.XcodeExpansionSpaceGiB)
	}); err != nil {
		return fmt.Errorf("setup: expanding the Xcode image filesystem: %w", err)
	}
	output, err := n.command(ctx, nil, false, "ip", name, "--wait", "300")
	if err != nil {
		return err
	}
	host := strings.TrimSpace(string(output))
	if host == "" {
		return fmt.Errorf("tart: VM %s has no IP address", name)
	}
	if err := waitSSH(ctx, host); err != nil {
		return err
	}
	info, err := os.Stat(config.XcodeArchive)
	if err != nil {
		return err
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Copying Xcode %s into the guest (%.1f GiB)...\n", config.XcodeVersion, float64(info.Size())/(1<<30))
	}
	const guestArchive = "/private/tmp/Xcode.xip"
	if err := n.during(ctx, "Copying Xcode archive", func() error { return sshPush(ctx, host, config.XcodeArchive, guestArchive) }); err != nil {
		return fmt.Errorf("setup: copying Xcode archive: %w", err)
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Expanding and installing Xcode %s...\n", config.XcodeVersion)
	}
	return n.during(ctx, "Xcode installation", func() error {
		return macos.InstallXcode(ctx, n.streamTarget(name), guestArchive)
	})
}

func (n *native) InstallMacPorts(ctx context.Context, name string, config Config, release macos.Release) error {
	return installation.Install(ctx, n.target(name), config.MacPortsVersion, release)
}
