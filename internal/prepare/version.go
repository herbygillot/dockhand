package prepare

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
)

func (s *Service) prepareVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	release := request.Release
	if release == nil || release.Requested != request.Version {
		return Result{}, fmt.Errorf("prepare: a matching resolved release is required")
	}
	if request.Version == "" && release.CurrentVersion != input.info.Version {
		return Result{}, fmt.Errorf("prepare: automatic selection does not match the input version")
	}
	if release.NoUpdate && request.Version != "" {
		return Result{}, fmt.Errorf("prepare: explicit selection cannot imply no update")
	}
	if input.target.Subport != "" {
		return Result{}, fmt.Errorf("%w: version bumps currently select the primary port", ErrUnsupported)
	}
	if err := s.Upstream.Check(ctx, input.info, *release); err != nil {
		return Result{}, err
	}
	if release.NoUpdate {
		return Result{Base: request.Source, Target: input.target, Release: release, PreparedTree: request.Source.Tree}, nil
	}
	contents, err := versionEdits(input.data, input.info.Version, release.Version, input.info.Revision)
	if err != nil {
		return Result{}, err
	}
	if _, err := checksumWords(input.data, input.info.Options["checksums"]); err != nil {
		return Result{}, err
	}
	_, oldURL, err := downloadSource(input.info)
	if err != nil {
		return Result{}, err
	}
	_, _, versioned, versionRoot, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return Result{}, err
	}
	fidelity := versionFidelity(input.before, versioned, input.target.Name, input.files.Root, versionRoot, *release, input.info.Options["checksums"])
	result := Result{Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	info := versioned.Ports[input.target.Name]
	_, newURL, err := downloadSource(info)
	if err != nil {
		return result, err
	}
	if newURL == oldURL {
		return result, fmt.Errorf("%w: version edit did not change the download source", ErrUnsupported)
	}
	download, err := s.download(ctx, info)
	if err != nil {
		return result, err
	}
	contents, checksums, err := replaceChecksums(contents, info.Options["checksums"], download)
	if err != nil {
		return result, err
	}
	edit, tree, after, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return result, err
	}
	final := versionFidelity(input.before, after, input.target.Name, input.files.Root, root, *release, checksums)
	result.Files, result.Downloads, result.Fidelity = []git.FileEdit{edit}, []Download{download}, append(result.Fidelity, final)
	if len(final.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, final.UnexpectedChanges)
	}
	if err := s.Upstream.Check(ctx, input.info, *release); err != nil {
		return result, err
	}
	result.PreparedTree = record.ObjectID(tree)
	result.Commits = []CommitIntent{{Subject: input.target.Name + ": update to " + release.Version, Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return result, nil
}

func versionFidelity(before, after macports.Snapshot, selected, beforeRoot, afterRoot string, release record.Release, checksums string) Fidelity {
	result := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".version -> " + release.Version, selected + ".revision -> 0", selected + ".distfiles and checksums"}, UnexpectedChanges: []string{}}
	names := map[string]bool{}
	for name := range before.Ports {
		names[name] = true
	}
	for name := range after.Ports {
		names[name] = true
	}
	for name := range names {
		original, was := before.Ports[name]
		next, is := after.Ports[name]
		if !was || !is {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+": port set changed")
			continue
		}
		old := comparablePort(original, beforeRoot)
		next = comparablePort(next, afterRoot)
		if name == selected {
			old.Version, old.Revision = release.Version, 0
			for _, key := range []string{"version", "github.version", "go.version", "git.branch", "distname", "distfiles", "master_sites", "worksrcdir", "livecheck.version", "github.master_sites"} {
				if value, ok := old.Options[key]; ok {
					old.Options[key] = strings.ReplaceAll(value, original.Version, release.Version)
				}
			}
			if next.Options["git.branch"] != release.Tag {
				result.UnexpectedChanges = append(result.UnexpectedChanges, name+".git.branch differs from selected tag")
			}
			actual, errs := syntax.ListValues(next.Options["checksums"])
			expected, expectedErrs := syntax.ListValues(checksums)
			if len(errs) > 0 || len(expectedErrs) > 0 || !slices.Equal(actual, expected) {
				result.UnexpectedChanges = append(result.UnexpectedChanges, name+".checksums differ from intended values")
			}
			old.Options["checksums"] = next.Options["checksums"]
		}
		if old.Revision != next.Revision {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".revision changed unexpectedly")
		}
		result.UnexpectedChanges = append(result.UnexpectedChanges, comparePortMetadata(name, old, next)...)
	}
	slices.Sort(result.UnexpectedChanges)
	return result
}
