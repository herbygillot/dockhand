package prepare

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/macports"
)

func checkSnapshot(snapshot macports.Snapshot, bound macports.Context) error {
	platform := snapshot.Platform
	if snapshot.Source != bound.Source() || !reflect.DeepEqual(snapshot.Target, bound.Target()) || platform.OS == "" || platform.Version == "" || platform.Architecture == "" || (bound.Platform().OS != "" && platform != bound.Platform()) {
		return fmt.Errorf("%w: evaluation does not match its bound source, target, or platform", ErrFidelity)
	}
	if len(snapshot.Ports) == 0 {
		return fmt.Errorf("%w: evaluation contains no ports", ErrFidelity)
	}
	for name, port := range snapshot.Ports {
		if name != port.Name || port.Version == "" || port.Revision < 0 || port.Epoch < 0 {
			return fmt.Errorf("%w: invalid evaluated port %s", ErrFidelity, name)
		}
	}
	return nil
}

func revisionFidelity(before, after macports.Snapshot, selected, beforeRoot, afterRoot string) Fidelity {
	result := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".revision +1"}, UnexpectedChanges: []string{}}
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
		old = comparablePort(old, beforeRoot)
		next = comparablePort(next, afterRoot)
		if old.Name != next.Name {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".name changed")
		}
		if old.Version != next.Version {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".version changed")
		}
		if old.Epoch != next.Epoch {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".epoch changed")
		}
		if !reflect.DeepEqual(old.Dependencies, next.Dependencies) {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".dependencies changed")
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
				result.UnexpectedChanges = append(result.UnexpectedChanges, name+"."+key+" changed")
			}
		}
		if !maps.Equal(old.OptionErrors, next.OptionErrors) {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".option-errors changed")
		}
	}
	slices.Sort(result.UnexpectedChanges)
	return result
}

func comparablePort(port macports.PortInfo, root string) macports.PortInfo {
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
