package main

import (
	"runtime"
	"sync"
	"sync/atomic"

	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/model"
	"os"
	"path"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	portsurvey "github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/progress"
)

// Request selects local ports.
type Request struct {
	Selection portsurvey.Selection
	// Journal, when set, receives each port as it finishes and names the
	// ports a rerun leaves out: the journal, or the record.
	Journal Recorder
}

func (r Request) Validate() error {
	return r.Selection.Validate()
}

type Result struct {
	Source model.Source
	Ports  []Port
	// Skipped counts the selected ports the journal already held.
	Skipped int `json:",omitempty"`
}

type Port struct {
	Tolerance []toleranceSummary `json:",omitempty"`
	Selector  string
	portedit.Assessment
	// portfile is the port's Portfile where the selection knew it, and
	// observed each context it was observed in, for the record.
	portfile string
	observed []observed
}

// Service receives evaluation and source selection.
type Service struct {
	Repo            *git.Repository
	Ports           macports.NativeEvaluator
	DependencyTools depblock.Tools
	Index           portindex.Source
	// Observe observes each port as committed in each context its
	// assessment observes it in, for the record; ObserveOnly does that
	// alone, without assessing it.
	Observe, ObserveOnly bool
	MeasureFollower      bool
}

func (s *Service) Assess(ctx context.Context, request Request) (_ Result, err error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.Repo == nil || s.Ports == nil {
		return Result{}, fmt.Errorf("assess: Git and MacPorts are required")
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	// A survey evaluates MacPorts' own tree at a revision (#162).
	ctx = workspace.MacPorts(ctx)
	platform, err := s.Ports.NativePlatform(ctx)
	if err != nil {
		return Result{}, err
	}
	files, err := portsurvey.Open(ctx, s.Repo, nil, platform, s.Index, request.Selection)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	projection := files.Projection
	result := Result{Source: files.Source, Ports: []Port{}}
	journal := request.Journal
	if journal != nil {
		if err := journal.Begin(files.Source); err != nil {
			return Result{}, err
		}
	}
	for _, problem := range files.Problems {
		port := Port{Selector: problem.Port, Assessment: portedit.Assessment{Outcome: portedit.Unknown, Findings: []portedit.Finding{{Check: "selection", Status: portedit.Unknown, Code: "index-coverage", Detail: problem.Detail}}}}
		if journal != nil {
			if journal.Has(port.Selector) {
				result.Skipped++
				continue
			}
			if err := journal.Record(port); err != nil {
				return result, err
			}
		}
		result.Ports = append(result.Ports, port)
	}
	editor := &portedit.Service{Ports: s.Ports, DependencyTools: s.DependencyTools}
	// Ports are assessed concurrently, each with its own interpreters, and
	// results keep the selection's order. The snapshot is shared, and an
	// assessment writes probe candidates into its own Portfile while it
	// runs, so ports that share a Portfile, subports of one port, are
	// assessed one after another; only distinct Portfiles overlap. The bound
	// keeps a whole-tree run from starting more MacPorts processes than the
	// host can run at once.
	assessed := make([]Port, len(files.Ports))
	failures := make([]error, len(files.Ports))
	skipped := make([]bool, len(files.Ports))
	slots := make(chan struct{}, Concurrency)
	var wait sync.WaitGroup
	byPortfile := map[string][]int{}
	var order []string
	for i, selected := range files.Ports {
		if journal != nil && journal.Has(selected.Label) {
			skipped[i] = true
			result.Skipped++
			continue
		}
		key := selected.Portfile
		if key == "" {
			key = selected.Selection.Selector
		}
		if _, seen := byPortfile[key]; !seen {
			order = append(order, key)
		}
		byPortfile[key] = append(byPortfile[key], i)
	}
	total := len(files.Ports) - result.Skipped
	progress.VerboseReport(ctx, "Assessing %d ports across %d Portfiles, %d at a time", total, len(order), Concurrency)
	if journal != nil && result.Skipped > 0 {
		progress.Report(ctx, "Continuing the journal: %d ports already assessed, %d to go", result.Skipped, total)
	}
	var finished atomic.Int64
	for _, key := range order {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() { <-slots }()
			for _, i := range byPortfile[key] {
				if ctx.Err() != nil {
					return
				}
				assessed[i], failures[i] = s.assessOne(ctx, editor, files, projection, platform, files.Ports[i])
				if failures[i] == nil && journal != nil {
					failures[i] = journal.Record(assessed[i])
				}
				if failures[i] != nil {
					cancel(failures[i])
					return
				}
				if n := finished.Add(1); journal != nil && n%1000 == 0 {
					progress.Report(ctx, "Assessed %d of %d ports", n, total)
				}
			}
		}()
	}
	wait.Wait()
	if cause := context.Cause(ctx); cause != nil {
		return result, cause
	}
	for _, err := range failures {
		if err != nil {
			return result, err
		}
	}
	for i := range assessed {
		if !skipped[i] {
			result.Ports = append(result.Ports, assessed[i])
		}
	}
	return result, ctx.Err()
}

// Concurrency bounds the ports assessed at once.
var Concurrency = min(8, max(2, runtime.NumCPU()))

// assessOne assesses one selected port: its probe and the assessment
// itself.
func (s *Service) assessOne(ctx context.Context, editor *portedit.Service, files *portsurvey.Workspace, projection *workspace.Workspace, platform model.Platform, selected portsurvey.Port) (Port, error) {
	if err := ctx.Err(); err != nil {
		return Port{}, err
	}
	progress.VerboseReport(ctx, "Assessing %s", selected.Label)
	item := Port{Selector: selected.Label, portfile: selected.Portfile}
	if selected.Name != "" && selected.Name != path.Base(path.Dir(selected.Selection.Selector)) {
		selected.Selection.Subport = selected.Name
	}
	probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: files.Source, Workspace: projection, Selection: selected.Selection, Platform: platform})
	if problem == nil {
		defer probe.Close()
	}
	if problem == nil && selected.Name != "" {
		problem = probe.Agrees(selected.Name)
	}
	if problem == nil && s.Observe {
		// Observed first, so the assessment's own observations of the
		// committed Portfile are these.
		var measurementErr error
		err := probe.Observations(ctx, func(profile model.Platform, observation macports.Observation, observedErr error) {
			target := observation.Snapshot.Target
			if target.Name == "" {
				target = model.Target{Name: probe.Port().Name, Portfile: selected.Portfile, Subport: selected.Selection.Subport, Variants: selected.Selection.Variants}
			}
			name := target.Name
			root := observation.Snapshot.Root
			if root == "" {
				root = projection.Root()
			}
			modeled := observation.Modeled
			if observedErr != nil && profile != platform {
				modeled = true
			}
			entry := observed{Platform: profile, Modeled: modeled, Root: root, Info: observation.Snapshot.Ports[name], Observation: observation.Ports[name], Err: observedErr}
			if s.MeasureFollower && measurementErr == nil {
				contents, readErr := os.ReadFile(filepath.Join(root, target.Portfile))
				if readErr != nil {
					measurementErr = readErr
					return
				}
				entry.Capture, measurementErr = captureTolerance(ctx, target, profile, modeled, observation.Unanswered, entry.Observation, contents, root)
				if entry.Capture != nil && observedErr != nil {
					entry.Capture.Pair.Complete = false
					entry.Capture.Pair.Parity = false
					entry.Capture.Error = observedErr.Error()
					entry.Capture.ErrorCode = portedit.Problem("evaluation", observedErr).Code
				}
			}
			if measurementErr != nil {
				measurementErr = fmt.Errorf("%s on %s %s: %w", name, profile.Version, profile.Architecture, measurementErr)
			}
			item.observed = append(item.observed, entry)
		})
		if measurementErr != nil {
			return item, measurementErr
		}
		if err != nil {
			if ctx.Err() != nil {
				return Port{}, ctx.Err()
			}
			item.observed = append(item.observed, observed{Platform: platform, Err: err})
		}
	}
	if problem != nil {
		item.Findings = []portedit.Finding{portedit.Problem("evaluation", problem)}
		item.Summarize()
		if s.Observe {
			item.observed = []observed{{Platform: platform, Err: problem}}
		}
		return item, nil
	}
	if s.ObserveOnly {
		return item, nil
	}
	var err error
	if item.Assessment, err = probe.Assess(ctx, nil); err != nil {
		return Port{}, err
	}
	return item, nil
}
