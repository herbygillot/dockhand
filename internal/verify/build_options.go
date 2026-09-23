package verify

import (
	"errors"

	"github.com/herbygillot/dockhand/internal/record"
)

// BuildOptions are a verification's choices about a local build: the test
// policy, a from-source build, whether the port needs Xcode, and the
// MacPorts Base that evaluated it on the host. The provider that builds
// locally turns them into a build configuration; the provider choice
// makes them, which is why they live here and not with that provider.
type BuildOptions struct {
	Tests      record.TestPolicy
	FromSource bool
	NeedsXcode bool
	// HostMacPortsVersion is the MacPorts Base that evaluated the port on the
	// host. It selects nothing; the provider warns when the image's observed
	// Base differs, since host evaluation and guest builds then disagree.
	HostMacPortsVersion string
}

// ErrExecutableUnavailable is a local provider whose executable is absent;
// ErrImageUnavailable a local provider with no prepared image for the
// platform. The provider choice falls back on them.
var (
	ErrExecutableUnavailable = errors.New("tart: executable is unavailable")
	ErrImageUnavailable      = errors.New("tart: no suitable prepared image is available")
)
