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

func isStable(version string) bool { return releasever.Classify(version) == releasever.Stable }
