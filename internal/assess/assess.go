package assess

import (
	"runtime"
	"sync"

	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"net/http"
	"path"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Request selects local ports and optionally one exact upstream release.
type Request struct {
	SharedRelease bool
	Selection     survey.Selection
	Version       string
	Subport       string
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
			return fmt.Errorf("assess: --version requires exactly one explicit port")
		}
		return version.Validate(r.Version)
	}
	return nil
}

type Result struct {
	Source record.Source
	Ports  []Port
}

type Port struct {
	Selector string
	portedit.Assessment
}

// Service receives evaluation, source selection, and optional release integrations.
type Service struct {
	Repo            *git.Repository
	Ports           macports.NativeReader
	Upstream        *upstream.Service
	DependencyTools dependency.Tools
	Index           portindex.Config
	HTTP            *http.Client
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
	files, err := survey.Open(ctx, s.Repo, platform, s.Index, s.HTTP, request.Selection)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	result := Result{Source: files.Source, Ports: []Port{}}
	for _, problem := range files.Problems {
		result.Ports = append(result.Ports, Port{Selector: problem.Port, Assessment: portedit.Assessment{Outcome: portedit.Unknown, Findings: []portedit.Finding{{Check: "selection", Status: portedit.Unknown, Code: "index-coverage", Detail: problem.Detail}}}})
	}
	editor := &portedit.Service{Ports: s.Ports, DependencyTools: s.DependencyTools}
	// Ports are assessed concurrently, each with its own interpreters; the
	// snapshot and PortIndex they share are read-only. Results keep the
	// selection's order. The bound keeps a whole-tree run from starting more
	// MacPorts processes than the host can run at once.
	assessed := make([]Port, len(files.Ports))
	failures := make([]error, len(files.Ports))
	slots := make(chan struct{}, Concurrency)
	var wait sync.WaitGroup
	for i, selected := range files.Ports {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() { <-slots }()
			assessed[i], failures[i] = s.assessOne(ctx, editor, files, platform, request, selected)
		}()
	}
	wait.Wait()
	for _, err := range failures {
		if err != nil {
			return result, err
		}
	}
	result.Ports = append(result.Ports, assessed...)
	return result, ctx.Err()
}

// Concurrency bounds the ports assessed at once.
var Concurrency = min(8, max(2, runtime.NumCPU()))

// assessOne assesses one selected port: its probe, the optional release
// resolution, and the assessment itself.
func (s *Service) assessOne(ctx context.Context, editor *portedit.Service, files *survey.Workspace, platform record.Platform, request Request, selected survey.Port) (Port, error) {
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
		probe, problem := editor.Probe(ctx, portedit.ProbeSource{SharedRelease: request.SharedRelease, Source: files.Source, Root: files.Root, Selection: selected.Selection, Platform: platform})
		if problem == nil && selected.Name != "" && selected.Name != probe.Port().Name {
			problem = fmt.Errorf("%w: indexed subport %s; evaluation selected a different port %s", portedit.ErrUnsupported, selected.Name, probe.Port().Name)
		}
		if problem != nil {
			item.Findings = []portedit.Finding{portedit.Problem("evaluation", problem)}
			item.Summarize()
		} else {
			var release *record.Release
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
