package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

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
	info, err := os.Stat(config.XcodeArchive)
	if err != nil {
		return err
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Copying Xcode %s into the guest (%.1f GiB)...\n", config.XcodeVersion, float64(info.Size())/(1<<30))
	}
	const guestArchive = "/private/tmp/Xcode.xip"
	guest, err := n.guestFor(name)
	if err != nil {
		return err
	}
	// The host's digest is read while the copy goes, and the guest's copy
	// is compared with it before it's expanded: monterey's xip failed on
	// both runs, "Block-compressed payload operation failed", with an
	// archive the host expands, on an OS that had expanded it before
	// (field testing, 2026-10-16).
	digests := make(chan hostDigest, 1)
	go func() {
		digest, err := fileDigest(config.XcodeArchive)
		digests <- hostDigest{digest, err}
	}()
	if err := n.during(ctx, "Copying Xcode archive", func() error { return guest.Upload(ctx, config.XcodeArchive, guestArchive, false) }); err != nil {
		return fmt.Errorf("setup: copying Xcode archive: %w", err)
	}
	host := <-digests
	if host.err != nil {
		return fmt.Errorf("setup: reading the Xcode archive's sha256: %w", host.err)
	}
	copied, err := macos.GuestDigest(ctx, n.target(name), guestArchive)
	if err != nil {
		return fmt.Errorf("setup: checking the Xcode archive's copy: %w", err)
	}
	if copied != host.digest {
		return fmt.Errorf("setup: the Xcode archive's copy in the guest is %s, and %s is %s: the copy was cut short or changed, so it wasn't expanded", copied, config.XcodeArchive, host.digest)
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Expanding and installing Xcode %s...\n", config.XcodeVersion)
	}
	err = n.during(ctx, "Xcode installation", func() error {
		return macos.InstallXcode(ctx, n.streamTarget(name), guestArchive)
	})
	if err != nil {
		return fmt.Errorf("%w (its copy in the guest matched the host's: %s)", err, host.digest)
	}
	return nil
}

// hostDigest is the Xcode archive's digest on the host, or why it wasn't
// read.
type hostDigest struct {
	digest macos.ArchiveDigest
	err    error
}

// fileDigest is a file's size and sha256.
func fileDigest(path string) (macos.ArchiveDigest, error) {
	file, err := os.Open(path)
	if err != nil {
		return macos.ArchiveDigest{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return macos.ArchiveDigest{}, err
	}
	return macos.ArchiveDigest{Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

// InstallMacPorts installs MacPorts, keeping each port's archive: a check
// identifies the ports active as a target built by their archives
// (decision 28).
func (n *native) InstallMacPorts(ctx context.Context, name string, config Config, release macos.Release) error {
	if err := installation.Install(ctx, n.target(name), config.MacPortsVersion, release); err != nil {
		return err
	}
	return installation.KeepArchives(ctx, n.target(name), config.GuestPrefix)
}
