package portedit

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func (s *Service) prepareArchiveVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	release := request.Release
	if release == nil || release.Requested != request.Version {
		return Result{}, fmt.Errorf("portedit: a matching resolved release is required")
	}
	if request.Version == "" && release.CurrentVersion != input.info.Version {
		return Result{}, fmt.Errorf("portedit: automatic selection does not match the input version")
	}
	if release.NoUpdate && request.Version != "" {
		return Result{}, fmt.Errorf("portedit: explicit selection cannot imply no update")
	}
	if input.target.Subport != "" {
		return Result{}, fmt.Errorf("%w: version bumps currently select the primary port", ErrUnsupported)
	}
	if err := s.Upstream.Check(ctx, input.info, *release); err != nil {
		return Result{}, err
	}
	if release.NoUpdate {
		return Result{Base: request.Source, Target: input.target, Release: release}, nil
	}
	spec, err := portsource.Interpret(input.info)
	if err != nil {
		return Result{}, err
	}
	sourceVersion, ok := spec.Pattern.Version(release.Tag)
	if !ok {
		return Result{}, fmt.Errorf("%w: selected tag does not match source convention", ErrFidelity)
	}
	carriers, err := s.versionCarriers(ctx, request, input)
	if err != nil {
		return Result{}, err
	}
	contents, versioned, err := s.probeVersion(ctx, request, input, carriers, sourceVersion)
	if err != nil {
		return Result{}, err
	}
	if versioned.Ports[input.target.Name].Version != release.Version {
		return Result{}, fmt.Errorf("%w: evaluated version differs from resolved release", ErrFidelity)
	}
	versionRoot := input.files.Root

	oldSources, err := downloadSources(input.info, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile)))
	if err != nil {
		return Result{}, err
	}
	oldGroups, err := portfile.ChecksumCount(input.data, input.info.Options["checksums"])
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
	groups, err := portfile.ChecksumCount(contents, info.Options["checksums"])
	if err != nil {
		return Result{}, err
	}
	if groups != oldGroups || len(sources) != len(oldSources) {
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
	placeholders := make([]portfile.Checksum, len(sources))
	for i, source := range sources {
		placeholders[i] = portfile.Checksum{Name: source.Name}
	}
	if _, _, err = portfile.ReplaceChecksums(contents, info.Options["checksums"], placeholders...); err != nil {
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
	contents, checksums, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], checksumValues(downloads)...)
	if err != nil {
		return result, err
	}
	edit, after, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return result, err
	}
	final := checksumFidelity(versioned, after, input.target.Name, versionRoot, root, checksums)
	result.Files, result.Downloads, result.Fidelity = []portfile.Edit{edit}, downloads, append(result.Fidelity, final)
	if len(final.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, final.UnexpectedChanges)
	}
	if err := s.Upstream.Check(ctx, input.info, *release); err != nil {
		return result, err
	}
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
			for _, key := range []string{"fetch.has_credentials", "version", "github.version", "gitlab.version", "go.version", "git.branch", "distname", "distfiles", "master_sites", "worksrcdir", "livecheck.version", "github.master_sites", "gitlab.master_sites"} {
				if value, ok := next.Options[key]; ok {
					old.Options[key] = value
				} else {
					delete(old.Options, key)
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

func checksumFidelity(before, after macports.Snapshot, selected, beforeRoot, afterRoot, checksums string) Fidelity {
	expected := before
	expected.Ports = maps.Clone(before.Ports)
	info := expected.Ports[selected]
	info.Options = maps.Clone(info.Options)
	info.Options["checksums"] = checksums
	expected.Ports[selected] = info
	result := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".checksums"}}
	normalized := after
	normalized.Ports = maps.Clone(after.Ports)
	next := normalized.Ports[selected]
	next.Options = maps.Clone(next.Options)
	values, errs := syntax.ListValues(next.Options["checksums"])
	wanted, wantedErrs := syntax.ListValues(checksums)
	if len(errs) > 0 || len(wantedErrs) > 0 || !slices.Equal(values, wanted) {
		result.UnexpectedChanges = append(result.UnexpectedChanges, selected+".checksums differ from intended values")
	}
	next.Options["checksums"] = checksums
	normalized.Ports[selected] = next
	if err := CheckEquivalent(expected, normalized, beforeRoot, afterRoot); err != nil {
		result.UnexpectedChanges = append(result.UnexpectedChanges, err.Error())
	}
	return result
}

func checksumValues(downloads []Download) []portfile.Checksum {
	values := make([]portfile.Checksum, len(downloads))
	for i, d := range downloads {
		values[i] = portfile.Checksum{Name: d.Name, SHA256: d.SHA256, RMD160: d.RMD160, Size: d.Size}
	}
	return values
}
