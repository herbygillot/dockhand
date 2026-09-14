package tart

import "github.com/herbygillot/dockhand/v2/internal/record"

// ImageManifestProtocol identifies the current setup manifest representation.
const ImageManifestProtocol = 2

// ImageManifest describes the environment declared by a provisioned image.
type ImageManifest struct {
	Protocol          int             `json:"protocol"`
	Source            string          `json:"source"`
	Platform          record.Platform `json:"platform"`
	MacPortsPrefix    string          `json:"macports_prefix,omitempty"`
	MacPortsVersion   string          `json:"macports_version"`
	GuestAgentVersion string          `json:"guest_agent_version"`
	XcodeVersion      string          `json:"xcode_version,omitempty"`
}
