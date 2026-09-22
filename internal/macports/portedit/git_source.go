package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"regexp"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/progress"
)

// A port fetched with git has no archive to download and no checksums to
// refresh: the build clones git.url at git.branch. A version bump is the
// version edit alone, and what stands in for the checksum match is that the
// evaluated git.branch lands on the resolved tag, or on the resolved commit
// when the Portfile pins a literal one. Modeled contexts are not observed;
// there is no archive plan for them to cover.

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// obsoleteIn reports a port that evaluates as an obsolete follower in the
// observed context: replaced_by names its successor and it fetches nothing.
func obsoleteIn(info macports.PortInfo) bool {
	return info.Options["replaced_by"] != "" && info.OptionErrors["replaced_by"] == ""
}

// gitFetched reports whether MacPorts fetches the port by cloning.
func gitFetched(info macports.PortInfo) bool {
	return info.Options["fetch.type"] == "git" && info.OptionErrors["fetch.type"] == ""
}

// checkGitSource refuses the shapes the git path does not handle: an archive
// beside the clone, or a generated dependency block, which would need the
// clone on the host.
func checkGitSource(info macports.PortInfo) error {
	if info.OptionErrors["git.url"] != "" || info.OptionErrors["git.branch"] != "" || info.Options["git.url"] == "" {
		return fmt.Errorf("%w: git source without an evaluable git.url", ErrUnsupported)
	}
	if info.Options["checksums"] != "" {
		return fmt.Errorf("%w: a git-fetched port with checksums mixes an archive into the clone; prepare this port manually", ErrUnsupported)
	}
	for _, key := range []string{"go.vendors", "cargo.crates", "cargo.crates_github"} {
		if info.Options[key] != "" {
			return fmt.Errorf("%w: %s regeneration needs the source on the host, which a git fetch does not provide", ErrUnsupported, key)
		}
	}
	return nil
}

// planGitVersion completes the version plan for a git-fetched port from the
// versioned candidate: no downloads, a literal pinned commit moved to the
// resolved one, and fidelity on git.branch in place of checksums.
func (s *Service) planGitVersion(ctx context.Context, request Request, input *sourceInput, contents []byte, versioned macports.Snapshot) (archivePlan, error) {
	release := request.Release
	if err := checkGitSource(input.info); err != nil {
		return archivePlan{}, err
	}
	next := versioned.Ports[input.target.Name]
	if !gitFetched(next) {
		return archivePlan{}, fmt.Errorf("%w: the version edit changed the fetch type", ErrFidelity)
	}
	if err := checkGitSource(next); err != nil {
		return archivePlan{}, err
	}
	branch := release.Tag
	if old := input.info.Options["git.branch"]; commitPattern.MatchString(old) {
		if release.Commit == "" {
			return archivePlan{}, fmt.Errorf("%w: git.branch pins a commit and the resolved release names none", ErrUnsupported)
		}
		rewritten, err := portfile.RewriteLiteralDeclaration(contents, "git.branch", old, release.Commit)
		if err != nil {
			return archivePlan{}, err
		}
		evaluated, err := s.evaluateEdit(ctx, input, rewritten)
		if err != nil {
			return archivePlan{}, err
		}
		contents, versioned, branch = rewritten, evaluated.after, release.Commit
		progress.Report(ctx, "The Portfile's git.branch pins a commit; moving it to %s", release.Commit)
	}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return archivePlan{}, err
	}
	report := fidelity.GitVersion(request.SharedRelease, family, versioned, input.target.Name, *release, branch)
	result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release, Coverage: []ContextCoverage{{Fetch: next.Fetch, Platform: input.before.Platform}}}
	result.report(report)
	if len(report.UnexpectedChanges) > 0 {
		return archivePlan{result: result}, fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	progress.VerboseReport(ctx, "%s is fetched with git; the build clones %s at %s", input.target.Name, next.Options["git.url"], next.Options["git.branch"])
	return archivePlan{result: result, contents: contents, versioned: versioned, viaGit: true, branch: branch, subject: "update to " + release.Version}, nil
}

// applyGitVersion evaluates the final candidate once more and commits it;
// there is nothing to download.
func (s *Service) applyGitVersion(ctx context.Context, request Request, input *sourceInput, plan archivePlan) (Result, error) {
	result := plan.result
	evaluated, err := s.evaluateEdit(ctx, input, plan.contents)
	if err != nil {
		return result, err
	}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return result, err
	}
	report := fidelity.GitVersion(request.SharedRelease, family, evaluated.after, input.target.Name, *request.Release, plan.branch)
	if err := result.commitEdit(input, request, evaluated.edit, report, plan.subject); err != nil {
		return result, err
	}
	if input.scope != nil {
		result.Scope, err = macports.RebindReleaseScope(input.scope, evaluated.after)
		if err != nil {
			return result, err
		}
	}
	if patched(evaluated.after.Ports[input.target.Name]) {
		progress.Report(ctx, "%s declares patches; a git fetch is not extracted here, so they are checked by the build", input.target.Name)
	}
	return result, nil
}
