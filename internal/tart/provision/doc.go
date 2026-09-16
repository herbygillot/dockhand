// Package provision constructs and validates local Tart images for verification.
//
// Provisioner prepares disposable guests, installs the guest agent, developer
// tools, and MacPorts, then validates and adopts the completed image. It owns
// provisioning progress, cleanup, and replacement recovery. Check mode validates
// an existing image through a disposable clone. VM control and installation
// mechanics are supplied by tart/host, macos, and macports/installation.
package provision
