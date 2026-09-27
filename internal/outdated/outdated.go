package outdated

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

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
	// Commit is the revision surveyed; HEAD when empty.
	Commit string
	// Concurrency is how many ports are looked up at once; Concurrency
	// when zero.
	Concurrency int
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
	revision := s.Commit
	if revision == "" {
		revision = "HEAD"
	}
	files, err := survey.OpenAt(ctx, s.Repo, revision, s.Workspaces, platform, s.Index, selection)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	editor := &portedit.Service{Ports: ports}
	result := Result{Source: files.Source}
	for _, problem := range files.Problems {
		result.Ports = append(result.Ports, Port{Selector: problem.Port, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC(), Detail: problem.Detail}})
	}
	// Ports are looked up concurrently, and the results keep the
	// selection's order. Probing a candidate version writes it into the
	// port's own Portfile in the shared projection, so ports that share a
	// Portfile, subports of one port, go one after another; only distinct
	// Portfiles overlap. Most of a port's time is spent waiting on its
	// upstream, and GitHub's requests are paced for the whole process
	// (internal/github), however many ports are looked up at once.
	var order []string
	byPortfile := map[string][]int{}
	for i, selected := range files.Ports {
		key := selected.Portfile
		if key == "" {
			key = selected.Selection.Selector
		}
		if _, seen := byPortfile[key]; !seen {
			order = append(order, key)
		}
		byPortfile[key] = append(byPortfile[key], i)
	}
	observed := make([]Port, len(files.Ports))
	done := make([]bool, len(files.Ports))
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency())
	for _, key := range order {
		group.Go(func() error {
			for _, i := range byPortfile[key] {
				if err := gctx.Err(); err != nil {
					return err
				}
				var err error
				if observed[i], err = s.observeOne(gctx, editor, files, platform, files.Ports[i]); err != nil {
					return err
				}
				done[i] = true
			}
			return nil
		})
	}
	// Interrupted, it reports the ports it finished, as they stand.
	failed := group.Wait()
	for i, port := range observed {
		if done[i] {
			result.Ports = append(result.Ports, port)
		}
	}
	if failed == nil {
		failed = ctx.Err()
	}
	return result, failed
}

// Concurrency is how many ports Observe looks up at once when the service
// doesn't say: each takes MacPorts processes of its own while it is
// evaluated, and a few requests upstream.
var Concurrency = min(8, max(2, runtime.NumCPU()))

func (s *Service) concurrency() int {
	if s.Concurrency > 0 {
		return s.Concurrency
	}
	return Concurrency
}

// observeOne looks up one selected port's newest release. A problem with
// the port is its result; only releasing its probe can fail the survey.
func (s *Service) observeOne(ctx context.Context, editor *portedit.Service, files *survey.Workspace, platform record.Platform, selected survey.Port) (Port, error) {
	item := Port{Selector: selected.Label, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC()}}
	probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: files.Source, Workspace: files.Projection, Selection: selected.Selection, Platform: platform})
	if problem == nil && selected.Name != "" && probe.Port().Name != selected.Name {
		problem = fmt.Errorf("indexed subport %s: upstream version probing currently supports the primary port %s", selected.Name, probe.Port().Name)
	}
	if problem == nil {
		var bound *upstream.Discovery
		bound, problem = s.Upstream.Bind(probe)
		if problem == nil {
			item.Result, problem = bound.Discover(ctx)
		}
	}
	if problem != nil {
		item.Assessment = upstream.Unknown
		item.Detail = problem.Error()
	}
	return item, probe.Close()
}
