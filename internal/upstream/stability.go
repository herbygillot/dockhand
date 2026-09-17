package upstream

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream/releasever"
)

// classified records the release's stability and whether it takes the port
// out of stable. Explicit selections are never refused on this basis.
func classified(release record.Release, current string) record.Release {
	release.Stability = string(releasever.Classify(release.Version))
	release.LeavesStable = releasever.LeavesStable(current, release.Version)
	return release
}

// automatic reports whether a port's current version supports automatic
// selection: a stable or prerelease numeric spelling. Unknown spellings such
// as patch letters need an explicit version.
func automatic(current string) bool {
	return releasever.Classify(current) != releasever.Unknown
}

// admits reports whether a candidate may be selected automatically. Stable
// candidates always qualify; prerelease candidates qualify only for a port
// that already rides a prerelease, as -devel ports do.
func admits(current, candidate string) bool {
	switch releasever.Classify(candidate) {
	case releasever.Stable:
		return true
	case releasever.Prerelease:
		return releasever.Classify(current) == releasever.Prerelease
	}
	return false
}
