package portedit

import (
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/record"
)

func releaseScope(before, after macports.Snapshot, selected string, authorized bool) (*record.ReleaseScope, error) {
	scope := &record.ReleaseScope{}
	oldRoot, nextRoot := before.Ports[selected], after.Ports[selected]
	if len(before.Ports) != len(after.Ports) {
		return nil, fmt.Errorf("%w: release changes the port set", ErrFidelity)
	}
	for name, old := range before.Ports {
		next, ok := after.Ports[name]
		if !ok {
			return nil, fmt.Errorf("%w: sibling disappeared", ErrFidelity)
		}
		target := before.Target
		target.Name = name
		target.Subport = ""
		if name != before.Target.Name {
			target.Subport = name
		}
		member := record.ReleaseMember{Target: target, Before: macports.ReleaseState(old), After: macports.ReleaseState(next), NeedsXcode: next.Options["use_xcode"] == "yes" || next.Options["use_xcode"] == "true" || next.Options["use_xcode"] == "1", MetadataOnly: next.Options["dockhand.metadata_only"] == "1"}
		if old.Version == next.Version {
			scope.Protected = append(scope.Protected, member)
			continue
		}
		if name != selected && !authorized {
			return nil, fmt.Errorf("%w: shared release also changes %s; inspect with assess --shared-release --version and authorize with bump --shared-release", ErrFidelity, name)
		}
		if old.Version != oldRoot.Version || next.Version != nextRoot.Version {
			return nil, fmt.Errorf("%w: %s belongs to an independent release", ErrFidelity, name)
		}
		// A shared input must describe the same source, not just coincident versions.
		if !member.MetadataOnly {
			for _, key := range []string{"git.branch", "distfiles", "master_sites", "checksums"} {
				if old.Options[key] != oldRoot.Options[key] || next.Options[key] != nextRoot.Options[key] {
					return nil, fmt.Errorf("%w: %s has independent %s; shared-source preparation is required", ErrFidelity, name, key)
				}
			}
		}
		scope.Affected = append(scope.Affected, member)
	}
	slices.SortFunc(scope.Affected, func(a, b record.ReleaseMember) int { return record.CompareTargets(a.Target, b.Target) })
	slices.SortFunc(scope.Protected, func(a, b record.ReleaseMember) int { return record.CompareTargets(a.Target, b.Target) })
	return scope, nil
}

func scopedVersionFidelity(shared bool, before, after macports.Snapshot, selected, root string, release record.Release, checksums string) Fidelity {
	if !shared {
		if _, err := releaseScope(before, after, selected, false); err != nil {
			return Fidelity{Before: before, After: after, UnexpectedChanges: []string{err.Error()}}
		}
		return versionFidelity(before, after, selected, root, release, checksums)
	}
	scope, err := releaseScope(before, after, selected, true)
	if err != nil {
		return Fidelity{Before: before, After: after, UnexpectedChanges: []string{err.Error()}}
	}
	normalized := before
	normalized.Ports = maps.Clone(before.Ports)
	var problems []string
	for _, member := range scope.Affected {
		name := member.Target.Name
		oneBefore, oneAfter := before, after
		oneBefore.Ports = map[string]macports.PortInfo{name: before.Ports[name]}
		oneAfter.Ports = map[string]macports.PortInfo{name: after.Ports[name]}
		ownRelease := release
		if member.MetadataOnly {
			ownRelease.Tag = ""
		}
		f := versionFidelity(oneBefore, oneAfter, name, root, ownRelease, after.Ports[name].Options["checksums"])
		problems = append(problems, f.UnexpectedChanges...)
		normalized.Ports[name] = after.Ports[name]
	}
	if err := CheckEquivalent(normalized, after, root, root); err != nil {
		problems = append(problems, err.Error())
	}
	return Fidelity{Before: before, After: after, UnexpectedChanges: problems}
}

func scopedChecksumFidelity(scope *record.ReleaseScope, before, after macports.Snapshot, selected, root, checksums string) Fidelity {
	if scope == nil {
		return checksumFidelity(before, after, selected, root, checksums)
	}
	expected := before
	expected.Ports = maps.Clone(before.Ports)
	for _, member := range scope.Affected {
		name := member.Target.Name
		info := before.Ports[name]
		if info.Version != member.After.Version || info.Options["checksums"] != before.Ports[selected].Options["checksums"] {
			continue
		}
		info.Options = maps.Clone(info.Options)
		info.Options["checksums"] = checksums
		expected.Ports[name] = info
	}
	result := Fidelity{Before: before, After: after}
	if err := CheckEquivalent(expected, after, root, root); err != nil {
		result.UnexpectedChanges = append(result.UnexpectedChanges, err.Error())
	}
	return result
}

func (s *Service) checkSharedArchiveOwners(input *sourceInput, contents []byte, observed macports.Observation, selected distfiles.Binding) error {
	if input.scope == nil {
		return nil
	}
	for _, member := range input.scope.Affected {
		if member.MetadataOnly || member.Target.Name == input.target.Name {
			continue
		}
		next := *input
		next.target = member.Target
		binding, err := s.bindArchives(&next, contents, observed)
		if err != nil {
			return err
		}
		if len(binding.Artifacts) != len(selected.Artifacts) {
			return fmt.Errorf("%w: %s has an independent archive plan", ErrFidelity, member.Target.Name)
		}
		for i, item := range binding.Artifacts {
			other := selected.Artifacts[i]
			if item.Group.ID() != other.Group.ID() || item.Name != other.Name || !slices.Equal(item.URLs, other.URLs) {
				return fmt.Errorf("%w: %s has independently owned source/checksum declarations", ErrFidelity, member.Target.Name)
			}
		}
	}
	return nil
}
