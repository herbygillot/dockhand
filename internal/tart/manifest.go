package tart

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// Guest agent facts shared by provisioning and guest execution: the
// tart-guest-agent release Dockhand installs, its archive digest, and where
// it lives in the guest.
const (
	GuestAgentRelease = "0.14.1"
	GuestAgentDigest  = "96596675452c8a4eed6f93c86a05b6a1e0c4bd2b0e381931b19ddeee3220eb23"
	GuestAgentPath    = "/opt/dockhand/bin/tart-guest-agent"
)

// ImageManifestProtocol identifies the current setup manifest
// representation: 3 adds the source's digest, the Command Line Tools'
// version, and the setup protocol.
const ImageManifestProtocol = 3

// SetupProtocol identifies what setup puts in an image beyond its source
// and the versions it records: how it installs the tools, MacPorts, and
// the guest agent, and configures the guest. It is raised when that
// changes what an image holds, which ends reuse of evidence from images
// made before (decision 28); a test pins the provisioning it covers, and
// fails until it is raised or, for a change of wording only, re-pinned.
// 2 keeps each port's archive (installation.KeepArchives); 3 flushes the
// guest before stopping it, so what setup wrote last is kept, which 2's
// images lost, that setting among it.
const SetupProtocol = 3

// ImageManifest describes the environment declared by a provisioned image.
type ImageManifest struct {
	Protocol int    `json:"protocol"`
	Source   string `json:"source"`
	// SourceDigest is what Source named when setup pulled it, as the
	// registry said on either side of the pull; empty when it couldn't.
	SourceDigest    string         `json:"source_digest,omitempty"`
	Platform        model.Platform `json:"platform"`
	MacPortsPrefix  string         `json:"macports_prefix,omitempty"`
	MacPortsVersion string         `json:"macports_version"`
	// GuestAgentVersion is observed diagnostic text, not a release or compatibility constraint.
	GuestAgentVersion string `json:"guest_agent_version"`
	XcodeVersion      string `json:"xcode_version,omitempty"`
	CommandLineTools  string `json:"command_line_tools,omitempty"`
	SetupProtocol     int    `json:"setup_protocol,omitempty"`
}

// Origin is the identity of what an image was made from and with
// (decision 28): its source by digest, the setup protocol, and the tools
// and MacPorts it holds. An image made again from the same source keeps
// it; a newer source or new tools don't. Empty when the source's digest
// isn't known, as for an image made before digests were recorded.
func (m ImageManifest) Origin() string {
	if m.SourceDigest == "" || m.SetupProtocol == 0 {
		return ""
	}
	parts := []string{"source " + m.SourceDigest, fmt.Sprintf("setup %d", m.SetupProtocol), "macports " + m.MacPortsVersion}
	if m.CommandLineTools != "" {
		parts = append(parts, "tools "+m.CommandLineTools)
	}
	if m.XcodeVersion != "" {
		parts = append(parts, "xcode "+m.XcodeVersion)
	}
	return strings.Join(parts, "; ")
}

// ImageRecordDirectory is where dockhand keeps what setup recorded of one
// Tart home's images: in its own directory, ~/.dockhand/tart-images,
// keyed by the canonical Tart home, as its locks are. The engine reads an
// image's origin here without starting it.
func ImageRecordDirectory(home string) (string, error) {
	return homeDirectory(home, "tart-images")
}

// WriteImageRecord records an image's manifest as setup wrote it into the
// image.
func WriteImageRecord(home, image string, manifest ImageManifest) error {
	directory, err := ImageRecordDirectory(home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(directory, image+".json"), append(data, '\n'), 0o600)
}

// ReadImageRecord reads what setup recorded of an image, reporting false
// for an image it recorded nothing of.
func ReadImageRecord(home, image string) (ImageManifest, bool, error) {
	directory, err := ImageRecordDirectory(home)
	if err != nil {
		return ImageManifest{}, false, err
	}
	data, err := os.ReadFile(filepath.Join(directory, image+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return ImageManifest{}, false, nil
	}
	if err != nil {
		return ImageManifest{}, false, err
	}
	var manifest ImageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ImageManifest{}, false, fmt.Errorf("image %s's record: %w", image, err)
	}
	return manifest, true, nil
}
