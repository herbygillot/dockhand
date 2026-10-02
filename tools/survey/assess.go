package main

import (
	"runtime"
	"sync"
	"sync/atomic"

	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/model"
	"path"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	portsurvey "github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Request selects local ports and optionally one exact upstream release.
type Request struct {
	SharedRelease bool
	Selection     portsurvey.Selection
	Version       string
	Subport       string
	// Journal, when set, receives each port as it finishes and names the
	// ports a rerun leaves out.
	Journal *Journal
}

func (r Request) Validate() error {
	if err := r.Selection.Validate(); err != nil {
		return err
	}
	if r.Subport != "" && (len(r.Selection.Ports) != 1 || !macports.ValidName(r.Subport)) {
		return fmt.Errorf("assess: --subport requires one explicit Portfile and a valid subport name")
	}
	if r.Version != "" {
		if len(r.Selection.Ports) != 1 {
			return fmt.Errorf("assess: --at requires exactly one explicit port")
		}
		return version.Validate(r.Version)
	}
	return nil
}

type Result struct {
	Source model.Source
	Ports  []Port
	// Skipped counts the selected ports the journal already held.
	Skipped int `json:",omitempty"`
}

type Port struct {
	Selector string
	portedit.Assessment
}

// Service receives evaluation, source selection, and optional release integrations.
type Service struct {
	Repo            *git.Repository
	Ports           macports.NativeEvaluator
	Upstream        *upstream.Service
	DependencyTools depblock.Tools
	Index           portindex.Source
	// Workspaces hands out the tree; nil materializes one for this survey.
	Workspaces *workspace.Registry
}

func (s *Service) Assess(ctx context.Context, request Request) (_ Result, err error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.Repo == nil || s.Ports == nil || (request.Version != "" && s.Upstream == nil) {
		return Result{}, fmt.Errorf("assess: Git, MacPorts, and requested release integrations are required")
	}
	platform, err := s.Ports.NativePlatform(ctx)
	if err != nil {
		return Result{}, err
	}
	files, err := portsurvey.Open(ctx, s.Repo, s.Workspaces, platform, s.Index, request.Selection)
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
				assessed[i], failures[i] = s.assessOne(ctx, editor, files, projection, platform, request, files.Ports[i])
				if failures[i] == nil && journal != nil {
					failures[i] = journal.Record(assessed[i])
				}
				if n := finished.Add(1); journal != nil && n%1000 == 0 {
					progress.Report(ctx, "Assessed %d of %d ports", n, total)
				}
			}
		}()
	}
	wait.Wait()
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

// assessOne assesses one selected port: its probe, the optional release
// resolution, and the assessment itself.
func (s *Service) assessOne(ctx context.Context, editor *portedit.Service, files *portsurvey.Workspace, projection *workspace.Workspace, platform model.Platform, request Request, selected portsurvey.Port) (Port, error) {
	if err := ctx.Err(); err != nil {
		return Port{}, err
	}
	progress.VerboseReport(ctx, "Assessing %s", selected.Label)
	item := Port{Selector: selected.Label}
	{
		if request.Subport != "" {
			selected.Selection.Subport = request.Subport
		} else if selected.Name != "" && selected.Name != path.Base(path.Dir(selected.Selection.Selector)) {
			selected.Selection.Subport = selected.Name
		}
		probe, problem := editor.Probe(ctx, portedit.ProbeSource{SharedRelease: request.SharedRelease, Source: files.Source, Workspace: projection, Selection: selected.Selection, Platform: platform})
		if problem == nil {
			defer probe.Close()
		}
		if problem == nil && selected.Name != "" {
			problem = probe.Agrees(selected.Name)
		}
		if problem != nil {
			item.Findings = []portedit.Finding{portedit.Problem("evaluation", problem)}
			item.Summarize()
		} else {
			var release *model.Release
			var resolutionErr error
			if request.Version != "" {
				bound, bindErr := s.Upstream.Bind(probe)
				resolutionErr = bindErr
				if bindErr == nil {
					resolved, resolveErr := bound.Resolve(ctx, request.Version)
					resolutionErr = resolveErr
					if resolveErr == nil {
						release = &resolved
					}
				}
			}
			var err error
			item.Assessment, err = probe.Assess(ctx, release)
			if err != nil {
				return Port{}, err
			}
			if resolutionErr != nil {
				item.Findings = append(item.Findings, portedit.Problem("release", resolutionErr))
				item.Summarize()
			}
		}
	}
	return item, nil
}
