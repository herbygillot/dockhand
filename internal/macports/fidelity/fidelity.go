// Package fidelity compares MacPorts evaluations of a Portfile before and after
// an edit and reports whether only the intended metadata changed. It knows
// nothing about editing, downloads, or workspaces: callers supply the
// evaluated snapshots, the selected port, and what they expected to change.
package fidelity

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// ErrMismatch means an evaluation does not match the intended change.
var ErrMismatch = errors.New("fidelity: evaluation does not match the intended change")

// Report is what an evaluated edit was expected to change and what it changed
// beyond that. Any unexpected change refuses the edit.
type Report struct {
	Before            macports.Snapshot
	After             macports.Snapshot
	ExpectedChanges   []string
	UnexpectedChanges []string
}

// CheckSnapshot requires an evaluation to describe its bound source, target,
// and platform completely.
func CheckSnapshot(snapshot macports.Snapshot, bound macports.Context) error {
	platform := snapshot.Platform
	if snapshot.Source != bound.Source() || !reflect.DeepEqual(snapshot.Target, bound.Target()) || platform.OS == "" || platform.Version == "" || platform.Architecture == "" || (bound.Platform().OS != "" && platform != bound.Platform()) {
		return fmt.Errorf("%w: evaluation does not match its bound source, target, or platform", ErrMismatch)
	}
	if len(snapshot.Ports) == 0 {
		return fmt.Errorf("%w: evaluation contains no ports", ErrMismatch)
	}
	for name, port := range snapshot.Ports {
		if name != port.Name || port.Version == "" || port.Revision < 0 || port.Epoch < 0 {
			return fmt.Errorf("%w: invalid evaluated port %s", ErrMismatch, name)
		}
	}
	return nil
}

// Revision expects only the selected port's revision to advance by one.
func Revision(before, after macports.Snapshot, selected, root string) Report {
	result := Report{Before: before, After: after, ExpectedChanges: []string{selected + ".revision +1"}, UnexpectedChanges: []string{}}
	names := map[string]bool{}
	for name := range before.Ports {
		names[name] = true
	}
	for name := range after.Ports {
		names[name] = true
	}
	for name := range names {
		old, was := before.Ports[name]
		next, is := after.Ports[name]
		if !was || !is {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+": port set changed")
			continue
		}
		wanted := old.Revision
		if name == selected {
			wanted++
		}
		if next.Revision != wanted {
			result.UnexpectedChanges = append(result.UnexpectedChanges, fmt.Sprintf("%s.revision: expected %d, got %d", name, wanted, next.Revision))
		}
		old = ComparablePort(old, root)
		next = ComparablePort(next, root)
		result.UnexpectedChanges = append(result.UnexpectedChanges, Compare(name, old, next)...)
	}
	slices.Sort(result.UnexpectedChanges)
	return result
}

// ComparablePort normalizes a port for comparison: the revision is compared
// separately, and workspace roots are replaced so relocated snapshots compare equal.
func ComparablePort(port macports.PortInfo, root string) macports.PortInfo {
	port.Options = maps.Clone(port.Options)
	delete(port.Options, "revision")
	for key, value := range port.Options {
		port.Options[key] = strings.ReplaceAll(value, root, "<source>")
	}
	port.OptionErrors = maps.Clone(port.OptionErrors)
	for key, value := range port.OptionErrors {
		port.OptionErrors[key] = strings.ReplaceAll(value, root, "<source>")
	}
	return port
}

// Compare lists the metadata differences between two normalized ports.
func Compare(name string, old, next macports.PortInfo) []string {
	var differences []string
	if old.Name != next.Name {
		differences = append(differences, name+".name changed")
	}
	if old.Version != next.Version {
		differences = append(differences, name+".version changed")
	}
	if old.Epoch != next.Epoch {
		differences = append(differences, name+".epoch changed")
	}
	if !reflect.DeepEqual(old.Dependencies, next.Dependencies) {
		differences = append(differences, name+".dependencies changed")
	}
	keys := map[string]bool{}
	for key := range old.Options {
		keys[key] = true
	}
	for key := range next.Options {
		keys[key] = true
	}
	for key := range keys {
		a, aok := old.Options[key]
		b, bok := next.Options[key]
		if aok != bok || a != b {
			differences = append(differences, name+"."+key+" changed")
		}
	}
	if !maps.Equal(old.OptionErrors, next.OptionErrors) {
		differences = append(differences, name+".option-errors changed")
	}
	return differences
}

// Equivalent requires two evaluations of the same context to agree on every
// port and every compared field.
func Equivalent(expected, actual macports.Snapshot, expectedRoot, actualRoot string) error {
	if expected.Platform != actual.Platform || !reflect.DeepEqual(expected.Target, actual.Target) {
		return fmt.Errorf("%w: candidate evaluation changed evaluation context", ErrMismatch)
	}
	if len(expected.Ports) != len(actual.Ports) {
		return fmt.Errorf("%w: candidate evaluation changed port set", ErrMismatch)
	}
	for name, old := range expected.Ports {
		next, ok := actual.Ports[name]
		if !ok || old.Revision != next.Revision {
			return fmt.Errorf("%w: candidate evaluation changed %s", ErrMismatch, name)
		}
		if changes := Compare(name, ComparablePort(old, expectedRoot), ComparablePort(next, actualRoot)); len(changes) > 0 {
			return fmt.Errorf("%w: candidate evaluation: %v", ErrMismatch, changes)
		}
	}
	return nil
}

// Version expects the selected port to move to the release, its revision to
// reset, and its source declarations and checksums to change, and nothing else.
func version(before, after macports.Snapshot, selected, root string, release record.Release, checksums string) Report {
	result := Report{Before: before, After: after, ExpectedChanges: []string{selected + ".version -> " + release.Version, selected + ".revision -> 0", selected + ".distfiles and checksums"}, UnexpectedChanges: []string{}}
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
		old := ComparablePort(original, root)
		next = ComparablePort(next, root)
		if name == selected {
			old.Version, old.Revision = release.Version, 0
			for _, key := range []string{"fetch.has_credentials", "version", "github.version", "gitlab.version", "go.version", "git.branch", "distname", "dist_subdir", "distfiles", "extract.only", "master_sites", "worksrcdir", "livecheck.version", "github.master_sites", "gitlab.master_sites"} {
				if value, ok := next.Options[key]; ok {
					old.Options[key] = value
				} else {
					delete(old.Options, key)
				}
			}

			if release.Tag != "" && next.Options["git.branch"] != release.Tag {
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
		result.UnexpectedChanges = append(result.UnexpectedChanges, Compare(name, old, next)...)
	}
	slices.Sort(result.UnexpectedChanges)
	return result
}

// Checksums expects only the selected port's checksums to change.
func Checksums(before, after macports.Snapshot, selected, root, checksums string) Report {
	expected := before
	expected.Ports = maps.Clone(before.Ports)
	info := expected.Ports[selected]
	info.Options = maps.Clone(info.Options)
	info.Options["checksums"] = checksums
	expected.Ports[selected] = info
	result := Report{Before: before, After: after, ExpectedChanges: []string{selected + ".checksums"}}
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
	if err := Equivalent(expected, normalized, root, root); err != nil {
		result.UnexpectedChanges = append(result.UnexpectedChanges, err.Error())
	}
	return result
}

// ReleaseScope partitions sibling ports into those a release changes and those
// it must protect, refusing independent releases and unauthorized siblings.
func ReleaseScope(before, after macports.Snapshot, selected string, authorized bool) (*record.ReleaseScope, error) {
	scope := &record.ReleaseScope{}
	oldRoot, nextRoot := before.Ports[selected], after.Ports[selected]
	if len(before.Ports) != len(after.Ports) {
		return nil, fmt.Errorf("%w: release changes the port set", ErrMismatch)
	}
	for name, old := range before.Ports {
		next, ok := after.Ports[name]
		if !ok {
			return nil, fmt.Errorf("%w: sibling disappeared", ErrMismatch)
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
		follower := followsObsolete(name, selected, old, next, oldRoot, nextRoot)
		if name != selected && !authorized && !follower {
			return nil, fmt.Errorf("%w: shared release also changes %s; inspect with assess --shared-release --version and authorize with bump --shared-release", ErrMismatch, name)
		}
		if old.Version != oldRoot.Version || next.Version != nextRoot.Version {
			return nil, fmt.Errorf("%w: %s belongs to an independent release", ErrMismatch, name)
		}
		// A shared input must describe the same source, not just coincident versions.
		if !member.MetadataOnly {
			for _, key := range []string{"git.branch", "distfiles", "master_sites", "checksums"} {
				if old.Options[key] != oldRoot.Options[key] || next.Options[key] != nextRoot.Options[key] {
					return nil, fmt.Errorf("%w: %s has independent %s; shared-source preparation is required", ErrMismatch, name, key)
				}
			}
		}
		scope.Affected = append(scope.Affected, member)
	}
	slices.SortFunc(scope.Affected, func(a, b record.ReleaseMember) int { return record.CompareTargets(a.Target, b.Target) })
	slices.SortFunc(scope.Protected, func(a, b record.ReleaseMember) int { return record.CompareTargets(a.Target, b.Target) })
	return scope, nil
}

// followsObsolete reports whether name is an obsolete follower of the selected
// port: replaced by it, carrying its version before and after, and building
// nothing. Such a sibling moves with the selected port without shared-release
// authorization, since it has no source of its own to get wrong.
func followsObsolete(name, selected string, old, next, oldRoot, nextRoot macports.PortInfo) bool {
	return name != selected && next.Options["replaced_by"] == selected && next.Options["dockhand.metadata_only"] == "1" &&
		old.Version == oldRoot.Version && next.Version == nextRoot.Version
}

// ScopedVersion applies Version to every affected member of a release and
// Equivalent to the rest. Without shared authorization the affected members
// are the selected port and its obsolete followers.
func ScopedVersion(shared bool, before, after macports.Snapshot, selected, root string, release record.Release, checksums string) Report {
	scope, err := ReleaseScope(before, after, selected, shared)
	if err != nil {
		return Report{Before: before, After: after, UnexpectedChanges: []string{err.Error()}}
	}
	if len(scope.Affected) == 1 && scope.Affected[0].Target.Name == selected {
		return version(before, after, selected, root, release, checksums)
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
		f := version(oneBefore, oneAfter, name, root, ownRelease, after.Ports[name].Options["checksums"])
		problems = append(problems, f.UnexpectedChanges...)
		normalized.Ports[name] = after.Ports[name]
	}
	if err := Equivalent(normalized, after, root, root); err != nil {
		problems = append(problems, err.Error())
	}
	return Report{Before: before, After: after, UnexpectedChanges: problems}
}

// ScopedChecksums applies Checksums across the affected members of a scope.
func ScopedChecksums(scope *record.ReleaseScope, before, after macports.Snapshot, selected, root, checksums string) Report {
	if scope == nil {
		return Checksums(before, after, selected, root, checksums)
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
	result := Report{Before: before, After: after}
	if err := Equivalent(expected, after, root, root); err != nil {
		result.UnexpectedChanges = append(result.UnexpectedChanges, err.Error())
	}
	return result
}
