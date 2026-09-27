package upstream

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/model"
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
	tied, comparison, err := s.newestTied(ctx, current, expression, candidates, noun)
	if err != nil {
		return 0, 0, err
	}
	if len(tied) != 1 {
		return 0, 0, ambiguousNewest(noun, hint)
	}
	return tied[0], comparison, nil
}

// newestTied is every candidate tied for newest, compared against the
// current version.
func (s *Service) newestTied(ctx context.Context, current, expression string, candidates []macports.VersionCandidate, noun string) ([]int, int, error) {
	selection, err := s.Versions.SelectVersion(ctx, current, expression, candidates)
	if err != nil {
		return nil, 0, err
	}
	if len(selection.Indices) == 0 {
		return nil, 0, fmt.Errorf("%w: no eligible version matches %s", ErrReleaseMissing, noun)
	}
	for _, index := range selection.Indices {
		if index < 0 || index >= len(candidates) || selection.Comparison < -1 || selection.Comparison > 1 {
			return nil, 0, fmt.Errorf("upstream: invalid version selection")
		}
	}
	return selection.Indices, selection.Comparison, nil
}

func ambiguousNewest(noun, hint string) error {
	return fmt.Errorf("%w: multiple %s compare equal as the newest version; specify %s explicitly", ErrReleaseAmbiguous, noun, hint)
}

// tagStyle is how a tag is spelled, apart from its numbers: v1.2 is v#.#,
// 1-2 is #-#, release-2026.09 is release-#.#.
func tagStyle(tag string) string {
	return digits.ReplaceAllString(tag, "#")
}

var digits = regexp.MustCompile(`[0-9]+`)

// sameCommitTag settles tags tied for the newest version that are one
// release: all at one commit, spelled in different styles, as a project
// changing its tag style tags a release both ways. The style the project's
// releases use lately wins: its other tags are read from the newest down,
// and the first release tagged in just one of the tied styles decides.
// When none does, the style of the tag the port follows now does. Tags at
// different commits are different releases, and are never guessed
// between.
func (s *Service) sameCommitTag(ctx context.Context, repository forge.Repository, expression string, tags []string, tied []int, history []macports.VersionCandidate, historyTags []string, current string) (int, bool, error) {
	commit := ""
	styles := map[string]int{}
	for _, index := range tied {
		tag, err := repository.Tag(ctx, tags[index])
		if err != nil {
			return 0, false, err
		}
		if commit != "" && tag.Commit != commit {
			return 0, false, nil
		}
		commit = tag.Commit
		style := tagStyle(tags[index])
		if _, repeated := styles[style]; repeated {
			return 0, false, nil
		}
		styles[style] = index
	}
	var remaining []int
	for i, tag := range historyTags {
		if !slices.ContainsFunc(tied, func(index int) bool { return tags[index] == tag }) {
			remaining = append(remaining, i)
		}
	}
	for len(remaining) > 0 {
		group, err := s.newestCaptures(ctx, expression, history, remaining)
		if err != nil {
			return 0, false, err
		}
		var found []string
		for _, i := range group {
			if style := tagStyle(historyTags[i]); !slices.Contains(found, style) {
				if _, ok := styles[style]; ok {
					found = append(found, style)
				}
			}
		}
		if len(found) == 1 {
			return styles[found[0]], true, nil
		}
		remaining = slices.DeleteFunc(remaining, func(i int) bool { return slices.Contains(group, i) })
	}
	index, ok := styles[tagStyle(current)]
	return index, ok, nil
}

// classified records the release's stability and whether it takes the port
// out of stable. Explicit selections are never refused on this basis.
func classified(release model.Release, current string) model.Release {
	release.Stability = string(version.Classify(release.Version))
	release.LeavesStable = version.LeavesStable(current, release.Version)
	return release
}

// finish records the chosen release on the result with its assessment.
// selected words an update; among names the catalog for the current case.
func (result *Result) finish(release model.Release, current string, selected, among string) {
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
