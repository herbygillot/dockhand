package upstream

import (
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/record"
)

// classified records the release's stability and whether it takes the port
// out of stable. Explicit selections are never refused on this basis.
func classified(release record.Release, current string) record.Release {
	release.Stability = string(version.Classify(release.Version))
	release.LeavesStable = version.LeavesStable(current, release.Version)
	return release
}

// automatic reports whether a port's current version supports automatic
// selection: a stable or prerelease numeric spelling. Unknown spellings such
// as patch letters need an explicit version.
func automatic(current string) bool {
	return version.Classify(current) != version.Unknown
}

// admits reports whether a candidate may be selected automatically. Stable
// candidates always qualify; prerelease candidates qualify only for a port
// that already rides a prerelease, as -devel ports do.
func admits(current, candidate string) bool {
	switch version.Classify(candidate) {
	case version.Stable:
		return true
	case version.Prerelease:
		return version.Classify(current) == version.Prerelease
	}
	return false
}
