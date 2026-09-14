package tart

import "github.com/herbygillot/dockhand/internal/record"

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
