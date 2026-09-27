package upstream

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
)

var errAutomaticUnsupported = errors.New("upstream: automatic selection does not support this source convention")

// versionSelector orders and captures versions the way MacPorts does: vercmp
// for order and its native regex for livecheck captures.
type versionSelector interface {
	SelectVersion(context.Context, string, string, []macports.VersionCandidate) (macports.VersionSelection, error)
	ExtractVersions(context.Context, string, string, bool) ([]string, error)
}

// DiscoverPort finds a port's newest release upstream. Its version
// comparisons share one interpreter where the comparer can keep one.
func (s *Service) DiscoverPort(ctx context.Context, port macports.PortInfo) (result Result, err error) {
	err = s.withVersionSession(ctx, func(s *Service) error {
		result, err = s.discoverPort(ctx, port)
		return err
	})
	return result, err
}

// withVersionSession calls fn with the service's version comparisons in
// one interpreter, where its comparer can keep one
// (macports.VersionSessions): a port's discovery makes several, and each
// would otherwise start MacPorts' tclsh anew. A comparer that can't keep
// one, or can't start it now, is used as it is.
func (s *Service) withVersionSession(ctx context.Context, fn func(*Service) error) error {
	opener, ok := s.Versions.(macports.VersionSessions)
	if !ok {
		return fn(s)
	}
	session, err := opener.VersionSession(ctx)
	if err != nil {
		return fn(s)
	}
	local := *s
	local.Versions = session
	return errors.Join(fn(&local), session.Close())
}

func (s *Service) discoverPort(ctx context.Context, port macports.PortInfo) (result Result, err error) {
	result = Result{CurrentVersion: port.Version, Assessment: Unknown, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	defer func() {
		if err != nil {
			result.Detail = err.Error()
		}
	}()
	if s == nil || s.Versions == nil || s.EvaluateVersion == nil {
		return result, fmt.Errorf("upstream: version comparison and Portfile evaluation are required")
	}
	discovery, discoveryErr := portsource.Interpret(port, portsource.Discovery)
	if discoveryErr != nil {
		return result, fmt.Errorf("%w: %v", errAutomaticUnsupported, discoveryErr)
	}
	if discovery.Catalog == portsource.HTTPRegex {
		return s.discoverListing(ctx, port, discovery)
	}
	spec, repository, err := s.repository(port, true)
	if err != nil {
		return result, err
	}
	if !automatic(port.Version) {
		return result, fmt.Errorf("%w: require a stable or prerelease numeric version", errAutomaticUnsupported)
	}
	if spec.Livecheck.Overridden {
		return s.discoverOverridden(ctx, port, spec, repository)
	}
	followsPrereleases := followsPrereleases(port.Version)
	var observations []forge.Release
	if spec.Catalog == portsource.Releases {
		releases, ok := repository.(forge.ReleaseRepository)
		if !ok {
			return result, fmt.Errorf("upstream: %s catalog does not expose releases", spec.Forge)
		}
		observations, err = releases.Releases(ctx)
		if err != nil {
			return result, err
		}
	} else {
		tags, err := repository.ListTags(ctx)
		if err != nil {
			return result, err
		}
		for _, tag := range tags {
			observations = append(observations, forge.Release{Tag: tag.Name})
		}
	}
	seen := map[string]bool{}
	for _, observation := range observations {
		if seen[observation.Tag] {
			return result, fmt.Errorf("%w: repeated tag", forge.ErrIncomplete)
		}
		seen[observation.Tag] = true
	}
	var candidates []macports.VersionCandidate
	var tags []string
	for _, release := range observations {
		if release.Draft || release.Prerelease && !followsPrereleases {
			continue
		}
		version, matches := spec.Pattern.Version(release.Tag)
		if !matches || !admits(port.Version, version) {
			continue
		}
		subject, err := spec.MatchText(release.Tag)
		if err != nil {
			return result, err
		}
		candidates = append(candidates, macports.VersionCandidate{Version: version, MatchText: subject, CaptureVersion: version})
		tags = append(tags, release.Tag)
	}
	filters := make([]macports.VersionCandidate, len(candidates))
	copy(filters, candidates)
	for i := range filters {
		filters[i].Version = "1"
	}
	eligible, err := s.Versions.SelectVersion(ctx, "0", spec.Livecheck.Regex, filters)
	if err != nil {
		return result, err
	}
	var eligibleCandidates []macports.VersionCandidate
	var eligibleTags []string
	for _, index := range eligible.Indices {
		if index < 0 || index >= len(candidates) {
			return result, fmt.Errorf("upstream: invalid filter result")
		}
		candidate := candidates[index]
		candidate.Version = ""
		if candidate.CaptureVersion == spec.SourceVersion {
			candidate.Version = port.Version
		}
		eligibleCandidates = append(eligibleCandidates, candidate)
		eligibleTags = append(eligibleTags, tags[index])
	}
	// A tag that compares newer but predates the port's own release is an
	// old one oddly spelled, and is set aside for the next newest.
	current := spec.Pattern.Tag(spec.SourceVersion)
	var setAside []string
	var index, comparison int
	for {
		pool, poolTags := withoutTags(eligibleCandidates, eligibleTags, setAside)
		candidates, tags, err = s.evaluateNewest(ctx, spec.Livecheck.Regex, pool, poolTags)
		if err != nil {
			return result, err
		}
		var tied []int
		tied, comparison, err = s.newestTied(ctx, port.Version, spec.Livecheck.Regex, candidates, "the port's livecheck filter")
		if err != nil {
			return result, err
		}
		index = tied[0]
		if len(tied) > 1 {
			settled := false
			if index, settled, err = s.sameCommitTag(ctx, repository, spec.Livecheck.Regex, tags, tied, pool, poolTags, current); err != nil {
				return result, err
			}
			if !settled {
				return result, ambiguousNewest("the port's livecheck filter", "a tag")
			}
		}
		if comparison <= 0 {
			break
		}
		older, err := predates(ctx, repository, tags[index], current)
		if err != nil {
			return result, err
		}
		if !older {
			break
		}
		setAside = append(setAside, tags[index])
	}
	tag, err := repository.Tag(ctx, tags[index])
	if err != nil {
		return result, err
	}
	if tag.Name != tags[index] || !git.ValidObjectID(tag.Commit) {
		return result, fmt.Errorf("upstream: invalid selected tag observation")
	}
	result.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	evidenceURL, err := spec.EvidenceURL(tag.Name)
	if err != nil {
		return result, err
	}
	catalog := "tags"
	if spec.Catalog == portsource.Releases {
		catalog = "published releases"
	}
	release := model.Release{ReleaseSelection: model.ReleaseSelection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Version: candidates[index].Version, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt}
	result.finish(release, port.Version, "Selected "+tag.Name+" from "+catalog, " among "+catalog)
	if len(setAside) > 0 {
		result.Detail += fmt.Sprintf("; set aside %s, older than %s though it compares newer", strings.Join(setAside, ", "), current)
	}
	result.Evidence = []Observation{{Source: string(spec.Forge) + "-" + string(spec.Catalog), Version: release.Version, URL: evidenceURL, ObservedAt: result.ObservedAt}}
	return result, nil
}

// withoutTags are the candidates and their tags, less those set aside.
func withoutTags(candidates []macports.VersionCandidate, tags, setAside []string) ([]macports.VersionCandidate, []string) {
	var keptCandidates []macports.VersionCandidate
	var keptTags []string
	for i, tag := range tags {
		if !slices.Contains(setAside, tag) {
			keptCandidates = append(keptCandidates, candidates[i])
			keptTags = append(keptTags, tag)
		}
	}
	return keptCandidates, keptTags
}

// predates reports whether a tag's commit was made before the commit of the
// tag the port follows now: an old tag spelled so that it compares newer,
// such as dolt's v040.15, a mistyped v0.40.15, beside v1.81.4, or
// bat-extras' v20200408 beside v2024.08.24. A newer release never
// predates the current one; a new tag on the same commit is newer. It
// can't tell, and says no, where the forge can't date commits or the
// port's own tag isn't there.
func predates(ctx context.Context, repository forge.Repository, candidate, current string) (bool, error) {
	dated, ok := repository.(forge.DatedRepository)
	if !ok || current == "" || candidate == current {
		return false, nil
	}
	own, err := repository.Tag(ctx, current)
	if errors.Is(err, forge.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	other, err := repository.Tag(ctx, candidate)
	if err != nil {
		return false, err
	}
	if other.Commit == own.Commit {
		return false, nil
	}
	ownTime, err := dated.CommitTime(ctx, own.Commit)
	if err != nil {
		return false, err
	}
	otherTime, err := dated.CommitTime(ctx, other.Commit)
	if err != nil {
		return false, err
	}
	return otherTime.Before(ownTime), nil
}

// evaluateNewest evaluates only the candidates that can be the newest, and
// returns them. A Portfile turns a tag's captured version into the port's
// version in the captures' own order, as stripping a prefix or swapping
// separators does, so the newest capture is the newest version. Discovery
// from a livecheck or a listing already relies on that, and evaluates only
// the one it selects. Here the newest captures are evaluated, and so are
// the next newest, which checks that order at the one place it decides the
// answer: a tie between the two, or the next newest ahead, and every
// candidate is evaluated, as a Portfile that orders its versions otherwise
// needs. A port with hundreds of tags is evaluated twice, not hundreds of
// times, and a candidate that can't be the newest is never evaluated.
func (s *Service) evaluateNewest(ctx context.Context, expression string, candidates []macports.VersionCandidate, tags []string) ([]macports.VersionCandidate, []string, error) {
	remaining := make([]int, len(candidates))
	for i := range remaining {
		remaining[i] = i
	}
	var groups [][]int
	for len(groups) < 2 && len(remaining) > 0 {
		group, err := s.newestCaptures(ctx, expression, candidates, remaining)
		if err != nil {
			return nil, nil, err
		}
		groups = append(groups, group)
		remaining = slices.DeleteFunc(remaining, func(index int) bool { return slices.Contains(group, index) })
	}
	chosen := slices.Concat(groups...)
	if err := s.evaluateCandidates(ctx, candidates, tags, unevaluated(candidates, chosen)); err != nil {
		return nil, nil, err
	}
	if len(groups) == 2 {
		evaluated := subset(candidates, chosen)
		newest, err := s.Versions.SelectVersion(ctx, "0", expression, evaluated)
		if err != nil {
			return nil, nil, err
		}
		for _, i := range newest.Indices {
			if i < 0 || i >= len(chosen) {
				return nil, nil, fmt.Errorf("upstream: invalid version selection")
			}
			if !slices.Contains(groups[0], chosen[i]) {
				// The versions don't follow the captures here: every
				// candidate decides.
				all := slices.Concat(chosen, remaining)
				if err := s.evaluateCandidates(ctx, candidates, tags, unevaluated(candidates, all)); err != nil {
					return nil, nil, err
				}
				chosen = all
				break
			}
		}
	}
	return subset(candidates, chosen), subset(tags, chosen), nil
}

// newestCaptures are the candidates among remaining whose captured versions
// tie for newest, by MacPorts' vercmp.
func (s *Service) newestCaptures(ctx context.Context, expression string, candidates []macports.VersionCandidate, remaining []int) ([]int, error) {
	captures := make([]macports.VersionCandidate, len(remaining))
	for i, index := range remaining {
		captures[i] = macports.VersionCandidate{Version: candidates[index].CaptureVersion, MatchText: candidates[index].MatchText, CaptureVersion: candidates[index].CaptureVersion}
	}
	selection, err := s.Versions.SelectVersion(ctx, "0", expression, captures)
	if err != nil {
		return nil, err
	}
	if len(selection.Indices) == 0 {
		return nil, fmt.Errorf("upstream: no newest capture among %d candidates", len(remaining))
	}
	var group []int
	for _, i := range selection.Indices {
		if i < 0 || i >= len(remaining) {
			return nil, fmt.Errorf("upstream: invalid version selection")
		}
		group = append(group, remaining[i])
	}
	return group, nil
}

// unevaluated are the indices among chosen whose version isn't known yet.
func unevaluated(candidates []macports.VersionCandidate, chosen []int) []int {
	return slices.DeleteFunc(slices.Clone(chosen), func(index int) bool { return candidates[index].Version != "" })
}

// subset is the items at the indices, in their order.
func subset[T any](items []T, indices []int) []T {
	picked := make([]T, 0, len(indices))
	for _, index := range indices {
		picked = append(picked, items[index])
	}
	return picked
}

// evaluateCandidates fills the evaluated version of every pending candidate,
// in one batch when the bound probe supports it. MacPorts still evaluates each
// candidate; batching only shares the interpreter.
func (s *Service) evaluateCandidates(ctx context.Context, candidates []macports.VersionCandidate, tags []string, pending []int) error {
	if len(pending) == 0 {
		return nil
	}
	if s.EvaluateVersions != nil && len(pending) > 1 {
		values := make([]string, len(pending))
		for i, index := range pending {
			values[i] = candidates[index].CaptureVersion
		}
		versions, err := s.EvaluateVersions(ctx, values)
		if err != nil {
			return fmt.Errorf("upstream: cannot evaluate candidate versions: %w", err)
		}
		if len(versions) != len(values) {
			return fmt.Errorf("upstream: incomplete candidate evaluation")
		}
		for i, index := range pending {
			candidates[index].Version = versions[i]
		}
		return nil
	}
	for _, index := range pending {
		version, err := s.EvaluateVersion(ctx, candidates[index].CaptureVersion)
		if err != nil {
			return fmt.Errorf("upstream: cannot evaluate %s: %w", tags[index], err)
		}
		candidates[index].Version = version
	}
	return nil
}
