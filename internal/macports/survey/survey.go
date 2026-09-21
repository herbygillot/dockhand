package survey

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	portselection "github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// Selection chooses explicit ports, exact maintainer/category filters, or all ports.
type Selection struct {
	All            bool
	Ports          []string
	Maintainers    []string
	Categories     []string
	NotMaintainers []string
}

// Validate rejects mixed selector modes and malformed filter values.
func (s Selection) Validate() error {
	grouped := s.All || len(s.Maintainers)+len(s.Categories)+len(s.NotMaintainers) > 0
	if grouped && len(s.Ports) > 0 {
		return fmt.Errorf("choose explicit ports, maintainer/category filters, or --all; do not mix selector modes")
	}
	if grouped {
		return (portindex.Filter{All: s.All, Maintainers: s.Maintainers, Categories: s.Categories, NotMaintainers: s.NotMaintainers}).Validate()
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
	// Portfile is the file the selection resolves to when the index knows
	// it, "category/port/Portfile"; ports that share one must not be
	// probed at the same time, since probing writes candidates into it.
	Portfile string
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
func Open(ctx context.Context, repo *git.Repository, platform record.Platform, index portindex.Config, selection Selection) (_ *Workspace, err error) {
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
	if err := macports.ValidatePortsTree(files.Root, repo.Root); err != nil {
		return nil, err
	}
	ports, problems, err := selectPorts(ctx, repo, source, platform, index, files.Root, selection)
	if err != nil {
		return nil, err
	}
	return &Workspace{Source: source, Root: files.Root, Ports: ports, Problems: problems, files: files}, nil
}

func selectPorts(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, config portindex.Config, root string, selection Selection) ([]Port, []portindex.SelectionProblem, error) {
	var selected []Port
	var problems []portindex.SelectionProblem
	seen := map[string]bool{}
	if len(selection.Ports) > 0 {
		// The index is staged for resolution later anyway; consulting it now
		// tells which explicit names share a Portfile.
		var index *portindex.Index
		if err := portindex.Stage(ctx, repo, source, platform, config, root); err != nil {
			progress.VerboseReport(ctx, "PortIndex unavailable for grouping explicit ports: %v", err)
		} else if index, err = portindex.Open(root); err != nil {
			progress.VerboseReport(ctx, "PortIndex unreadable for grouping explicit ports: %v", err)
		}
		for _, selector := range selection.Ports {
			if seen[selector] {
				continue
			}
			seen[selector] = true
			port := Port{Label: selector, Selection: macports.Selection{Selector: selector}}
			if strings.Contains(selector, "/") {
				port.Portfile = selector
			} else if index != nil {
				if entry, err := index.Lookup(selector); err == nil {
					port.Portfile = path.Join(entry.Portdir, "Portfile")
				}
			}
			selected = append(selected, port)
		}
	}
	if len(selection.Ports) == 0 {
		if err := portindex.Stage(ctx, repo, source, platform, config, root); err != nil {
			return nil, nil, err
		}
		index, err := portindex.Open(root)
		if err != nil {
			return nil, nil, err
		}
		matches, err := index.Select(ctx, portindex.Filter{All: selection.All, Maintainers: selection.Maintainers, Categories: selection.Categories, NotMaintainers: selection.NotMaintainers})
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range matches.Entries {
			selected = append(selected, Port{Label: entry.Name, Name: entry.Name, Selection: portselection.FromEntry(entry, nil), Portfile: path.Join(entry.Portdir, "Portfile")})
		}
		problems = matches.Problems
		progress.VerboseReport(ctx, "Selected %d indexed ports; %d selection coverage problems", len(selected), len(matches.Problems))
	}
	return selected, problems, nil
}
