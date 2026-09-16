package dependents

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// Reason preserves the indexed relationship that selected a direct dependent.
type Reason struct {
	Root   string
	Fields []string
}

// Candidate is one target configuration to consider for isolated verification.
// A failed evaluation remains visible through Problem and a nil Evaluation.
type Candidate struct {
	Target     record.Target
	Root       bool
	Reasons    []Reason
	Evaluation *macports.Snapshot
	Problem    string
	// IndexedClosure is an estimate from default-variant PortIndex metadata,
	// not the dependency resolution that will actually occur in a build guest.
	IndexedClosure portindex.Closure
}

// Coverage includes explicit roots and their direct build, library, and runtime
// dependents. It does not expand reverse dependencies transitively. Unread fields
// anywhere in the reverse index may conceal additional dependents.
type Coverage struct {
	Source   record.Source
	Platform record.Platform
	Targets  []Candidate
	Unread   []portindex.Unread
}

type Service struct {
	Repo  *git.Repository
	Ports macports.Reader
	Index portindex.Config
	HTTP  *http.Client
}

// Discover stages an index and evaluates candidates against the same immutable
// source, outside workflow transactions. Dependent variants use MacPorts defaults;
// explicit root variants remain confined to their corresponding root questions.
func (s *Service) Discover(ctx context.Context, source record.Source, platform record.Platform, roots []record.Target) (_ Coverage, err error) {
	if err := ctx.Err(); err != nil {
		return Coverage{}, err
	}
	if s == nil || s.Repo == nil || s.Ports == nil || len(roots) == 0 {
		return Coverage{}, fmt.Errorf("dependents: Git, MacPorts, and at least one root are required")
	}
	files, err := s.Repo.Materialize(ctx, string(source.Tree))
	if err != nil {
		return Coverage{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	tree, err := macports.NewTree(source, files.Root, platform)
	if err != nil {
		return Coverage{}, err
	}
	for _, root := range roots {
		if _, err := tree.Select(root); err != nil {
			return Coverage{}, err
		}
	}
	if err := portindex.Stage(ctx, s.Repo, source, platform, s.Index, files.Root, s.HTTP); err != nil {
		return Coverage{}, err
	}
	index, err := portindex.Open(files.Root)
	if err != nil {
		return Coverage{}, err
	}
	return discover(ctx, s.Ports, tree, index, roots)
}

func discover(ctx context.Context, ports macports.Reader, tree macports.Tree, index *portindex.Index, roots []record.Target) (Coverage, error) {
	reverse, err := index.ReverseDependencies()
	if err != nil {
		return Coverage{}, err
	}
	result := Coverage{Source: tree.Source(), Platform: tree.Platform(), Unread: reverse.Unread}
	roots = slices.Clone(roots)
	slices.SortFunc(roots, record.CompareTargets)
	roots = slices.CompactFunc(roots, func(a, b record.Target) bool { return record.CompareTargets(a, b) == 0 })
	rootNames := map[string]string{}
	for _, root := range roots {
		name := strings.ToLower(root.Name)
		if previous, ok := rootNames[name]; ok && previous != root.Portfile {
			return Coverage{}, fmt.Errorf("dependents: root %s selects multiple Portfiles", root.Name)
		}
		rootNames[name] = root.Portfile
		root.Variants = maps.Clone(root.Variants)
		result.Targets = append(result.Targets, Candidate{Target: root, Root: true})
	}
	// One dependent can cover several roots. Its reasons are merged without
	// adding another default-variant question for an explicitly selected root.
	selected := map[string]int{}
	names := make([]string, 0, len(rootNames))
	for name := range rootNames {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, dependent := range reverse.ByPort[name] {
			key := strings.ToLower(dependent.Name)
			reason := Reason{Root: name, Fields: slices.Clone(dependent.Fields)}
			if _, ok := rootNames[key]; ok {
				for i := range result.Targets {
					if result.Targets[i].Root && strings.EqualFold(result.Targets[i].Target.Name, key) {
						result.Targets[i].Reasons = append(result.Targets[i].Reasons, reason)
					}
				}
				continue
			}
			i, ok := selected[key]
			if !ok {
				i = len(result.Targets)
				selected[key] = i
				// Appending without path cleaning lets Tree.Select reject malformed
				// index paths instead of interpreting them as a different Portfile.
				target := record.Target{Name: dependent.Name, Portfile: dependent.Portdir + "/Portfile", Subport: dependent.Name}
				result.Targets = append(result.Targets, Candidate{Target: target})
			}
			result.Targets[i].Reasons = append(result.Targets[i].Reasons, reason)
		}
	}
	slices.SortFunc(result.Targets, func(a, b Candidate) int { return record.CompareTargets(a.Target, b.Target) })
	for i := range result.Targets {
		if err := ctx.Err(); err != nil {
			return Coverage{}, err
		}
		candidate := &result.Targets[i]
		candidate.IndexedClosure, err = index.DependencyClosure([]string{candidate.Target.Name})
		if err != nil {
			return Coverage{}, err
		}
		entry, lookupErr := index.Lookup(candidate.Target.Name)
		if lookupErr != nil && !errors.Is(lookupErr, portindex.ErrNotIndexed) {
			return Coverage{}, lookupErr
		}
		if lookupErr != nil {
			candidate.Problem = lookupErr.Error()
			continue
		}
		if entry.Portdir != path.Dir(candidate.Target.Portfile) || entry.Name != candidate.Target.Name {
			candidate.Problem = "dependents: indexed identity does not match the selected target"
			continue
		}
		bound, bindErr := tree.Select(candidate.Target)
		if bindErr != nil {
			candidate.Problem = bindErr.Error()
			continue
		}
		evaluation, evalErr := ports.Evaluate(ctx, bound)
		if err := ctx.Err(); err != nil {
			return Coverage{}, err
		}
		if evalErr != nil {
			candidate.Problem = evalErr.Error()
			continue
		}
		info, exists := evaluation.Ports[candidate.Target.Name]
		if evaluation.Source != tree.Source() || evaluation.Platform != tree.Platform() || record.CompareTargets(evaluation.Target, candidate.Target) != 0 || !exists || info.Name != candidate.Target.Name {
			candidate.Problem = "dependents: evaluation does not match the selected source, platform, and target"
			continue
		}
		candidate.Evaluation = &evaluation
	}
	return result, nil
}
