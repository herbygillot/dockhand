package outdated

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Result retains the frozen source and independent observations for selected ports.
type Result struct {
	Source record.Source
	Ports  []Port
}

// Port associates a selector with its upstream observation or an unknown result.
type Port struct {
	Selector string
	upstream.Result
}

// Service observes committed ports using caller-supplied integrations and cache
// configuration. It does not open workflow state or accept jobs.
type Service struct {
	Repo     *git.Repository
	Ports    *macports.Evaluator
	Upstream *upstream.Service
	Index    portindex.Config
	HTTP     *http.Client
}

// Observe captures local HEAD and assesses every selected port independently.
// Dirty checkout edits are excluded and temporary source workspaces are released.
func (s *Service) Observe(ctx context.Context, selection Selection) (_ Result, err error) {
	if err := selection.Validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.Repo == nil || s.Ports == nil || s.Upstream == nil {
		return Result{}, fmt.Errorf("outdated: Git, MacPorts, and upstream discovery are required")
	}
	ports := s.Ports
	platform, err := ports.NativePlatform(ctx)
	if err != nil {
		return Result{}, err
	}
	files, err := survey.Open(ctx, s.Repo, platform, s.Index, s.HTTP, selection)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	source := files.Source
	discovery := s.Upstream
	editor := &portedit.Service{Ports: ports}
	result := Result{Source: source}
	for _, problem := range files.Problems {
		result.Ports = append(result.Ports, Port{Selector: problem.Port, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC(), Detail: problem.Detail}})
	}
	for _, selected := range files.Ports {
		selector := selected.Label
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item := Port{Selector: selector, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC()}}
		probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: source, Root: files.Root, Selection: selected.Selection, Platform: platform})
		if problem == nil && selected.Name != "" && probe.Port().Name != selected.Name {
			problem = fmt.Errorf("indexed subport %s: upstream version probing currently supports the primary port %s", selected.Name, probe.Port().Name)
		}
		if problem == nil {
			var bound *upstream.Discovery
			bound, problem = discovery.Bind(probe)
			if problem == nil {
				item.Result, problem = bound.Discover(ctx)
			}
		}
		if problem != nil {
			item.Assessment = upstream.Unknown
			item.Detail = problem.Error()
		}
		result.Ports = append(result.Ports, item)
	}
	return result, ctx.Err()
}
