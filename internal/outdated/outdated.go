package outdated

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/survey"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Result retains the frozen source and independent observations for selected ports.
type Result struct {
	Source model.Source
	Ports  []Port
}

// Port associates a selector with its upstream observation or an unknown result.
type Port struct {
	Selector string
	upstream.Result
	// With is the subport whose check stands for this one, which shares
	// its Portfile's release (WithSiblings); empty for one checked itself.
	With string
	// portfile is the Portfile it was selected from, and unsupported that
	// discovery doesn't take its source convention.
	portfile    string
	unsupported bool
}

// Service observes committed ports using caller-supplied integrations and cache
// configuration. It reads no record of dockhand's.
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
	// Progress, when set, hears how many of the ports are looked up: once
	// before the first, and after each, in order, never two at once.
	Progress func(done, total int)
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
	// selection's order. Each has its own probe and interpreters, and
	// evaluates candidate versions in overlays of the shared projection,
	// never in it, so subports of one Portfile overlap like any others. Most
	// of a port's time is spent waiting on its upstream, and GitHub's
	// requests are paced for the whole process (internal/github), however
	// many ports are looked up at once.
	observed := make([]Port, len(files.Ports))
	done := make([]bool, len(files.Ports))
	var progress sync.Mutex
	finished := 0
	report := func(port bool) {
		if s.Progress == nil {
			return
		}
		progress.Lock()
		defer progress.Unlock()
		if port {
			finished++
		}
		s.Progress(finished, len(files.Ports))
	}
	report(false)
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency())
	for i, selected := range files.Ports {
		group.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			var err error
			if observed[i], err = s.observeOne(gctx, editor, files, platform, selected); err != nil {
				return err
			}
			done[i] = true
			report(true)
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
	result.Ports = WithSiblings(result.Ports)
	return result, failed
}

// WithSiblings gives a subport whose source convention discovery doesn't
// take the result of a sibling it shares its Portfile's release with,
// checked, as dockhand edits subports that share the main version as one
// release: the python PortGroup turns its subports' livecheck off, so
// py310-cbor2 to py314-cbor2 read as problems beside py-cbor2, checked
// and outdated, 65 of the person's 1,079 ports (batch 30). Each names the
// sibling it's checked with. A subport that fetches nothing, as
// mysql8-server beside mysql8, moves with its sibling too, where one
// shares its release; its version is MacPorts' own only where none does.
func WithSiblings(ports []Port) []Port {
	checked := map[[2]string]Port{}
	for _, port := range ports {
		if port.portfile != "" && port.Assessment != upstream.Unknown && port.Assessment != upstream.OwnVersion && port.CurrentVersion != "" {
			key := [2]string{port.portfile, port.CurrentVersion}
			if _, ok := checked[key]; !ok {
				checked[key] = port
			}
		}
	}
	for i, port := range ports {
		sibling, ok := checked[[2]string{port.portfile, port.CurrentVersion}]
		if !port.unsupported && port.Assessment != upstream.OwnVersion || port.CurrentVersion == "" || !ok {
			continue
		}
		ports[i].Result, ports[i].With = sibling.Result, sibling.Selector
	}
	return ports
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
func (s *Service) observeOne(ctx context.Context, editor *portedit.Service, files *survey.Workspace, platform model.Platform, selected survey.Port) (Port, error) {
	item := Port{Selector: selected.Label, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC()}}
	probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: files.Source, Workspace: files.Projection, Selection: selected.Selection, Platform: platform})
	if problem == nil && selected.Name != "" {
		problem = probe.Agrees(selected.Name)
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
		item.unsupported = errors.Is(problem, upstream.ErrAutomaticUnsupported)
	}
	item.portfile = selected.Portfile
	return item, probe.Close()
}
