package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

type OutdatedResult struct {
	Source record.Source
	Ports  []OutdatedPort
}
type OutdatedPort struct {
	Selector string
	upstream.Result
}

// Outdated observes committed local source without accepting work or opening state.
func Outdated(ctx context.Context, config Config, selection OutdatedSelection) (_ OutdatedResult, err error) {
	if err := selection.Validate(); err != nil {
		return OutdatedResult{}, err
	}
	repo, err := git.Open(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return OutdatedResult{}, err
	}
	commit, err := repo.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return OutdatedResult{}, err
	}
	trees, err := repo.CommitTrees(ctx, []string{commit})
	if err != nil {
		return OutdatedResult{}, err
	}
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(trees[commit])}
	files, err := repo.Materialize(ctx, string(source.Tree))
	if err != nil {
		return OutdatedResult{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	platform, err := ports.NativePlatform(ctx)
	if err != nil {
		return OutdatedResult{}, err
	}
	discovery := releaseDiscovery(ports, newGitHubClient(config.GitHub), http.DefaultClient)
	editor := &portedit.Service{Ports: ports}
	result := OutdatedResult{Source: source}
	selected, problems, err := selectOutdated(ctx, config, repo, source, platform, files.Root, selection)
	if err != nil {
		return result, err
	}
	result.Ports = append(result.Ports, problems...)
	for _, selected := range selected {
		selector := selected.label
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item := OutdatedPort{Selector: selector, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC()}}
		probe, problem := editor.Probe(ctx, portedit.ProbeSource{Source: source, Root: files.Root, Selection: selected.selection, Platform: platform})
		if problem == nil && selected.name != "" && probe.Port().Name != selected.name {
			problem = fmt.Errorf("indexed subport %s: upstream version probing currently supports the primary port %s", selected.name, probe.Port().Name)
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
