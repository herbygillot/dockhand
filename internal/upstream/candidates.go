package upstream

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/record"
)

// The selection core shared by every automatic path. A catalog of tags, a
// list of releases, or a livecheck listing each produce candidates; this
// file owns what happens next, so a policy change lands once.

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

// followsPrereleases reports a port already on a prerelease, which selects
// prereleases from its catalog as well as stable releases.
func followsPrereleases(current string) bool { return version.Classify(current) == version.Prerelease }

// newest asks MacPorts for the single newest candidate that passes the
// port's livecheck expression, compared against the current version.
// noun and hint word the two failure modes for the caller's catalog.
func (s *Service) newest(ctx context.Context, current, expression string, candidates []macports.VersionCandidate, noun, hint string) (int, int, error) {
	selection, err := s.Versions.SelectVersion(ctx, current, expression, candidates)
	if err != nil {
		return 0, 0, err
	}
	if len(selection.Indices) == 0 {
		return 0, 0, fmt.Errorf("%w: no eligible version matches %s", ErrReleaseMissing, noun)
	}
	if len(selection.Indices) != 1 {
		return 0, 0, fmt.Errorf("%w: multiple %s compare equal as the newest version; specify %s explicitly", ErrReleaseAmbiguous, noun, hint)
	}
	index := selection.Indices[0]
	if index < 0 || index >= len(candidates) || selection.Comparison < -1 || selection.Comparison > 1 {
		return 0, 0, fmt.Errorf("upstream: invalid version selection")
	}
	return index, selection.Comparison, nil
}

// classified records the release's stability and whether it takes the port
// out of stable. Explicit selections are never refused on this basis.
func classified(release record.Release, current string) record.Release {
	release.Stability = string(version.Classify(release.Version))
	release.LeavesStable = version.LeavesStable(current, release.Version)
	return release
}

// finish records the chosen release on the result with its assessment.
// selected words an update; among names the catalog for the current case.
func (result *Result) finish(release record.Release, current string, selected, among string) {
	release = classified(release, current)
	result.Release = &release
	result.CandidateVersion = release.Version
	if release.NoUpdate {
		result.Assessment = Current
		result.Detail = fmt.Sprintf("Already current at %s; latest eligible version%s is %s", current, among, release.Version)
		return
	}
	result.Assessment = UpdateAvailable
	result.Detail = selected
}
