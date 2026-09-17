package tart

import "github.com/herbygillot/dockhand/internal/record"

// Guest agent facts shared by provisioning and guest execution: the
// tart-guest-agent release Dockhand installs, its archive digest, and where
// it lives in the guest.
const (
	GuestAgentRelease = "0.14.1"
	GuestAgentDigest  = "96596675452c8a4eed6f93c86a05b6a1e0c4bd2b0e381931b19ddeee3220eb23"
	GuestAgentPath    = "/opt/dockhand/bin/tart-guest-agent"
)

// ImageManifestProtocol identifies the current setup manifest representation.
const ImageManifestProtocol = 2

// ImageManifest describes the environment declared by a provisioned image.
type ImageManifest struct {
	Protocol        int             `json:"protocol"`
	Source          string          `json:"source"`
	Platform        record.Platform `json:"platform"`
	MacPortsPrefix  string          `json:"macports_prefix,omitempty"`
	MacPortsVersion string          `json:"macports_version"`
	// GuestAgentVersion is observed diagnostic text, not a release or compatibility constraint.
	GuestAgentVersion string `json:"guest_agent_version"`
	XcodeVersion      string `json:"xcode_version,omitempty"`
}
