package prepare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func (s *Service) prepareArchiveVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
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
	oldSources, err := downloadSources(input.info, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile)))
	if err != nil {
		return Result{}, err
	}
	oldGroups, err := checksumGroups(input.data, input.info.Options["checksums"])
	if err != nil {
		return Result{}, err
	}
	_, _, versioned, versionRoot, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return Result{}, err
	}
	info := versioned.Ports[input.target.Name]
	checked := info
	checked.Options = maps.Clone(info.Options)
	checked.Options["filespath"] = input.info.Options["filespath"]
	sources, err := downloadSources(checked, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile)))
	if err != nil {
		return Result{}, err
	}
	groups, err := checksumGroups(contents, info.Options["checksums"])
	if err != nil {
		return Result{}, err
	}
	if len(groups) != len(oldGroups) || len(sources) != len(oldSources) {
		return Result{}, fmt.Errorf("%w: source/checksum group count changed", ErrUnsupported)
	}
	changed := false
	for i, source := range sources {
		if source.URL != oldSources[i].URL {
			changed = true
		}
	}
	if !changed {
		return Result{}, fmt.Errorf("%w: version edit did not change the download source", ErrUnsupported)
	}
	fidelity := versionFidelity(input.before, versioned, input.target.Name, input.files.Root, versionRoot, *release, info.Options["checksums"])
	result := Result{Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	// Check source associations before starting downloads, including unchanged auxiliary archives.
	placeholders := make([]Download, len(sources))
	for i, source := range sources {
		placeholders[i] = Download{Name: source.Name}
	}
	if _, _, err = replaceChecksums(contents, info.Options["checksums"], placeholders...); err != nil {
		return result, err
	}
	downloads := make([]Download, 0, len(sources))
	for _, source := range sources {
		var output io.Writer
		var file *os.File
		if s.archiveDirectory != "" {
			file, err = os.CreateTemp(s.archiveDirectory, "source-*")
			if err != nil {
				return result, err
			}
			output = file
		}
		download, err := s.downloadArchive(ctx, info, source, output)
		if file != nil {
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			download.path = file.Name()
		}
		if err != nil {
			return result, err
		}
		downloads = append(downloads, download)
	}
	contents, checksums, err := replaceChecksums(contents, info.Options["checksums"], downloads...)
	if err != nil {
		return result, err
	}
	edit, tree, after, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return result, err
	}
	final := versionFidelity(input.before, after, input.target.Name, input.files.Root, root, *release, checksums)
	result.Files, result.Downloads, result.Fidelity = []git.FileEdit{edit}, downloads, append(result.Fidelity, final)
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
			for _, key := range []string{"version", "github.version", "gitlab.version", "go.version", "git.branch", "distname", "distfiles", "master_sites", "worksrcdir", "livecheck.version", "github.master_sites", "gitlab.master_sites"} {
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
