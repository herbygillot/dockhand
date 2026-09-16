package survey

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// Selection chooses explicit ports, exact maintainer/category filters, or all ports.
type Selection struct {
	All         bool
	Ports       []string
	Maintainers []string
	Categories  []string
}

// Validate rejects mixed selector modes and malformed filter values.
func (s Selection) Validate() error {
	grouped := s.All || len(s.Maintainers)+len(s.Categories) > 0
	if grouped && len(s.Ports) > 0 {
		return fmt.Errorf("choose explicit ports, maintainer/category filters, or --all; do not mix selector modes")
	}
	if grouped {
		return (portindex.Filter{All: s.All, Maintainers: s.Maintainers, Categories: s.Categories}).Validate()
	}
	if len(s.Ports) == 0 {
		return fmt.Errorf("select ports with arguments or a selection flag")
	}
	for _, port := range s.Ports {
		if strings.TrimSpace(port) == "" {
			return fmt.Errorf("port must not be empty")
		}
	}
	return nil
}

// Port retains an indexed name when selection came from metadata.
type Port struct {
	Label     string
	Name      string
	Selection macports.Selection
}

// Workspace owns a temporary tree until Close. Callers may probe edits only here.
type Workspace struct {
	Source   record.Source
	Root     string
	Ports    []Port
	Problems []portindex.SelectionProblem
	files    *git.Snapshot
}

func (w *Workspace) Close() error { return w.files.Close() }

// Open freezes HEAD, then selects explicit ports or stages and queries its index.
func Open(ctx context.Context, repo *git.Repository, platform record.Platform, index portindex.Config, client *http.Client, selection Selection) (_ *Workspace, err error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	commit, err := repo.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	trees, err := repo.CommitTrees(ctx, []string{commit})
	if err != nil {
		return nil, err
	}
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(trees[commit])}
	files, err := repo.Materialize(ctx, string(source.Tree))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, files.Close())
		}
	}()
	ports, problems, err := selectPorts(ctx, repo, source, platform, index, client, files.Root, selection)
	if err != nil {
		return nil, err
	}
	return &Workspace{Source: source, Root: files.Root, Ports: ports, Problems: problems, files: files}, nil
}

func selectPorts(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, config portindex.Config, client *http.Client, root string, selection Selection) ([]Port, []portindex.SelectionProblem, error) {
	var selected []Port
	var problems []portindex.SelectionProblem
	seen := map[string]bool{}
	for _, selector := range selection.Ports {
		if !seen[selector] {
			selected = append(selected, Port{Label: selector, Selection: macports.Selection{Selector: selector}})
			seen[selector] = true
		}
	}
	if len(selection.Ports) == 0 {
		if err := portindex.Stage(ctx, repo, source, platform, config, root, client); err != nil {
			return nil, nil, err
		}
		index, err := portindex.Open(root)
		if err != nil {
			return nil, nil, err
		}
		matches, err := index.Select(ctx, portindex.Filter{All: selection.All, Maintainers: selection.Maintainers, Categories: selection.Categories})
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range matches.Entries {
			selected = append(selected, Port{Label: entry.Name, Name: entry.Name, Selection: macports.Selection{Selector: entry.Portdir + "/Portfile"}})
		}
		problems = matches.Problems
		progress.Report(ctx, "Selected %d indexed ports; %d selection coverage problems", len(selected), len(matches.Problems))
	}
	return selected, problems, nil
}
