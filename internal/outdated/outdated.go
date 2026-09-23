package outdated

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
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

// Headline is the plain wording of a port's assessment; the code itself
// stays in JSON.
func (p Port) Headline() string {
	return strings.ReplaceAll(string(p.Assessment), "-", " ")
}

// Incomplete reports whether any port's observation is unknown, so a caller
// reads the verdict from the result rather than from the catalog's
// constants.
func (r Result) Incomplete() bool {
	for _, port := range r.Ports {
		if port.Assessment == upstream.Unknown {
			return true
		}
	}
	return false
}

// Hidden counts the ports an out-of-date report leaves unlisted.
type Hidden struct {
	Current int
	Unknown int
}

// OutOfDate is the result as outdated reports it unless asked for every
// port: only the ports with an update available. Current ports and ports
// that could not be checked are counted rather than listed, so a check
// that failed is never silent.
func (r Result) OutOfDate() (Result, Hidden) {
	report := Result{Source: r.Source, Ports: []Port{}}
	var hidden Hidden
	for _, port := range r.Ports {
		switch port.Assessment {
		case upstream.UpdateAvailable:
			report.Ports = append(report.Ports, port)
		case upstream.Current:
			hidden.Current++
		default:
			hidden.Unknown++
		}
	}
	return report, hidden
}

// Note is the line that says what an out-of-date report left out, or
// nothing when it left nothing out.
func (h Hidden) Note() string {
	var parts []string
	if h.Current > 0 {
		parts = append(parts, fmt.Sprintf("%d current", h.Current))
	}
	if h.Unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be checked", h.Unknown))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Not listed: " + strings.Join(parts, ", ") + "; --all lists them."
}

// Service observes committed ports using caller-supplied integrations and cache
// configuration. It does not open workflow state or accept jobs.
type Service struct {
	Repo     *git.Repository
	Ports    macports.NativeEvaluator
	Upstream *upstream.Service
	Index    portindex.Source
	// Workspaces hands out the tree; nil materializes one for this survey.
	Workspaces *workspace.Registry
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
	files, err := survey.Open(ctx, s.Repo, s.Workspaces, platform, s.Index, selection)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	projection := files.Projection
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
		probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: source, Workspace: projection, Selection: selected.Selection, Platform: platform})
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
		if err := probe.Close(); err != nil {
			return result, err
		}
		result.Ports = append(result.Ports, item)
	}
	return result, ctx.Err()
}
