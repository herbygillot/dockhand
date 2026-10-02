package macports

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Exclusion is why MacPorts CI wouldn't build a port on a platform, by the
// option that says so.
type Exclusion string

const (
	// ExcludedReplaced is a port replaced by another: replaced_by.
	ExcludedReplaced Exclusion = "replaced_by"
	// ExcludedPlatforms is a port whose platforms exclude the release,
	// which MacPorts marks known to fail.
	ExcludedPlatforms Exclusion = "platforms"
	// ExcludedKnownFail is a port declared known to fail: known_fail.
	ExcludedKnownFail Exclusion = "known_fail"
	// ExcludedArchs is a port that doesn't build for the architecture:
	// supported_archs.
	ExcludedArchs Exclusion = "supported_archs"
)

// Eligibility is whether MacPorts CI would build a port on a platform: the
// zero value where it would, else why not, and what the option said, the
// replacement, the platforms, or the architectures.
type Eligibility struct {
	Excluded Exclusion
	Detail   string
}

// Eligible reports a port MacPorts CI would build.
func (e Eligibility) Eligible() bool { return e.Excluded == "" }

// Reason words an exclusion for a plan, which names the environment
// beside it.
func (e Eligibility) Reason() string {
	switch e.Excluded {
	case ExcludedReplaced:
		return "replaced by " + e.Detail
	case ExcludedPlatforms:
		return "its platforms, " + e.Detail + ", exclude this release"
	case ExcludedKnownFail:
		return "the Portfile marks it known_fail here"
	case ExcludedArchs:
		return "supported_archs " + e.Detail + " only"
	}
	return ""
}

// BuildEligibility is whether MacPorts CI would build a port on a
// platform, reading each option as MacPorts reads it: known_fail as
// MacPorts tests it, which it defaults to yes where the port's platforms
// exclude the release, so that exclusion is named for its platforms;
// supported_archs as a Tcl list. An option that couldn't be read is an
// error, which is neither eligible nor excluded, and the caller keeps it
// apart (the private-helper review's finding 1).
func BuildEligibility(port PortInfo, platform model.Platform) (Eligibility, error) {
	replacedBy, _, err := port.option("replaced_by")
	if err != nil {
		return Eligibility{}, err
	}
	if by := strings.TrimSpace(replacedBy); by != "" {
		return Eligibility{Excluded: ExcludedReplaced, Detail: by}, nil
	}
	knownFail, err := port.KnownFail()
	if err != nil {
		return Eligibility{}, err
	}
	if knownFail {
		if compatible, known := port.PlatformsCompatible(); known && !compatible {
			return Eligibility{Excluded: ExcludedPlatforms, Detail: port.Options["platforms"]}, nil
		}
		return Eligibility{Excluded: ExcludedKnownFail}, nil
	}
	archs, _, err := port.optionList("supported_archs")
	if err != nil {
		return Eligibility{}, err
	}
	if len(archs) > 0 && platform.Architecture != "" && !slices.Contains(archs, "noarch") && !slices.Contains(archs, platform.Architecture) {
		return Eligibility{Excluded: ExcludedArchs, Detail: strings.Join(archs, " ")}, nil
	}
	return Eligibility{}, nil
}
