package upstream

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
)

// fullCommit is a commit as GitHub names it whole.
var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pinned reports a port that pins its source to a commit and tracks a
// branch for newer ones (livecheck.type git), as wezterm pins
// github.setup: the commit its source names, and the spec it's edited by.
func pinned(port macports.PortInfo) (portsource.Spec, bool) {
	discovery, err := portsource.Interpret(port, portsource.Discovery)
	if err != nil || discovery.Catalog != portsource.GitHead {
		return portsource.Spec{}, false
	}
	spec, err := portsource.Interpret(port, portsource.Edit)
	if err != nil || spec.Forge == "" || !fullCommit.MatchString(spec.SourceVersion) {
		return portsource.Spec{}, false
	}
	spec.Livecheck = discovery.Livecheck
	return spec, true
}

// resolvePin is the release that moves a commit pin: the branch's head,
// or the commit named, written into the source, with the version the
// port's scheme gives it, where its version reads as the pinned commit's
// date and hash (pinnedVersion), else the version named, with the head.
// wezterm's update refused the version and the commit alike, and was
// bumped by hand (the roadmap's v0.5.0 part 3, step 2).
func (s *Service) resolvePin(ctx context.Context, port macports.PortInfo, spec portsource.Spec, requested string) (model.Release, error) {
	_, repository, err := s.repository(port, false)
	if err != nil {
		return model.Release{}, err
	}
	commit, named := "", ""
	switch {
	case fullCommit.MatchString(requested):
		commit = requested
		if commits, ok := repository.(forge.CommitRepository); ok {
			found, err := commits.HasCommit(ctx, commit)
			if err != nil {
				return model.Release{}, err
			}
			if !found {
				return model.Release{}, fmt.Errorf("upstream: %s has no commit %s", repository.Name(), commit)
			}
		}
	case regexp.MustCompile(`^[0-9a-f]{7,39}$`).MatchString(requested):
		return model.Release{}, fmt.Errorf("upstream: name %s's commit whole, its 40 characters: %s is abbreviated", port.Name, requested)
	default:
		named = requested
		if commit, err = git.RemoteBranchCommit(ctx, s.Git, spec.Livecheck.URL, spec.Livecheck.Branch); err != nil {
			return model.Release{}, fmt.Errorf("upstream: reading %s's %s with git: %w", spec.Livecheck.URL, spec.Livecheck.Branch, err)
		}
	}
	if commit == spec.SourceVersion {
		return model.Release{}, fmt.Errorf("upstream: %s is already at %s, the commit it pins", port.Name, model.ObjectID(commit).Short())
	}
	version := named
	if version == "" {
		dated, ok := repository.(forge.DatedRepository)
		if !ok {
			return model.Release{}, fmt.Errorf("upstream: %s's forge can't say when a commit was made; name the version: dockhand update %s <version>", port.Name, port.Name)
		}
		was, err := dated.CommitTime(ctx, spec.SourceVersion)
		if err != nil {
			return model.Release{}, err
		}
		now, err := dated.CommitTime(ctx, commit)
		if err != nil {
			return model.Release{}, err
		}
		var ok2 bool
		if version, ok2 = pinnedVersion(port.Version, spec.SourceVersion, was, commit, now); !ok2 {
			return model.Release{}, fmt.Errorf("upstream: %s's version %s doesn't read as its commit's date and hash; name the version: dockhand update %s <version>", port.Name, port.Version, port.Name)
		}
	}
	if version == port.Version {
		return model.Release{}, fmt.Errorf("upstream: %s is already at version %s", port.Name, port.Version)
	}
	return model.Release{ReleaseSelection: model.ReleaseSelection{Requested: requested, CurrentVersion: port.Version}, Version: version, SourceVersion: commit, Pinned: true,
		Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: commit, Commit: commit, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}, nil
}

// checkPin checks a pinned release against the Portfile and the forge: a
// pinned port's, of its repository, naming a commit it has.
func (s *Service) checkPin(ctx context.Context, port macports.PortInfo, release model.Release) error {
	spec, ok := pinned(port)
	_, repository, err := s.repository(port, false)
	if err != nil {
		return err
	}
	if !ok || string(spec.Forge) != release.Forge || spec.Instance != release.Instance || repository.Name() != release.Repository || !fullCommit.MatchString(release.Commit) || release.Tag != release.Commit || release.SourceVersion != release.Commit {
		return fmt.Errorf("upstream: pinned release does not match the Portfile's commit pin")
	}
	if commits, ok := repository.(forge.CommitRepository); ok {
		found, err := commits.HasCommit(ctx, release.Commit)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s has no commit %s", ErrSourceChanged, repository.Name(), release.Commit)
		}
	}
	return nil
}

// dateLayouts are the ways a version spells its commit's date.
var dateLayouts = []string{"20060102", "2006.01.02", "2006-01-02", "2006.1.2"}

// pinnedVersion is the version a port's scheme gives a new commit, where
// its version as it is reads as its pinned commit's date and pieces of
// its hash with fixed text between: wezterm's 20261005-372548-295b0b25 is
// the commit's date, its hash's first six characters, and the eight after
// them. False where the version has neither, or the old commit's date
// isn't in it.
func pinnedVersion(version, oldCommit string, oldTime time.Time, newCommit string, newTime time.Time) (string, bool) {
	var out strings.Builder
	dated, hashed := false, false
	for i := 0; i < len(version); {
		if layout, ok := dateAt(version[i:], oldTime); ok {
			out.WriteString(newTime.UTC().Format(layout))
			i += len(oldTime.UTC().Format(layout))
			dated = true
			continue
		}
		// The longest run of the old hash that starts here, of at least
		// six characters, where it is.
		best, at := 0, 0
		for start := 0; start < len(oldCommit); start++ {
			n := 0
			for i+n < len(version) && start+n < len(oldCommit) && version[i+n] == oldCommit[start+n] {
				n++
			}
			if n > best {
				best, at = n, start
			}
		}
		if best >= 6 {
			out.WriteString(newCommit[at : at+best])
			i += best
			hashed = true
			continue
		}
		out.WriteByte(version[i])
		i++
	}
	return out.String(), dated && hashed
}

// dateAt reports the layout the date begins text with, where it does.
func dateAt(text string, date time.Time) (string, bool) {
	for _, layout := range dateLayouts {
		if spelled := date.UTC().Format(layout); strings.HasPrefix(text, spelled) {
			return layout, true
		}
	}
	return "", false
}
