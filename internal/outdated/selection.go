package outdated

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Selection chooses explicit ports or exact maintainer/category filters.
type Selection struct {
	Ports       []string
	Maintainers []string
	Categories  []string
}

// Validate rejects mixed selector modes and malformed filter values.
func (s Selection) Validate() error {
	grouped := len(s.Maintainers)+len(s.Categories) > 0
	if grouped && len(s.Ports) > 0 {
		return fmt.Errorf("choose explicit ports or maintainer/category selectors, not both")
	}
	if grouped {
		return (portindex.Filter{Maintainers: s.Maintainers, Categories: s.Categories}).Validate()
	}
	if len(s.Ports) == 0 {
		return fmt.Errorf("select ports with arguments, --maintainer, or --category")
	}
	for _, port := range s.Ports {
		if strings.TrimSpace(port) == "" {
			return fmt.Errorf("port must not be empty")
		}
	}
	return nil
}

type selectedPort struct {
	label     string
	name      string
	selection macports.Selection
}

func (s *Service) selectPorts(ctx context.Context, source record.Source, platform record.Platform, root string, selection Selection) ([]selectedPort, []Port, error) {
	var selected []selectedPort
	var problems []Port
	seen := map[string]bool{}
	for _, selector := range selection.Ports {
		if !seen[selector] {
			selected = append(selected, selectedPort{label: selector, selection: macports.Selection{Selector: selector}})
			seen[selector] = true
		}
	}
	if len(selection.Ports) == 0 {
		if err := portindex.Stage(ctx, s.Repo, source, platform, s.Index, root, s.HTTP); err != nil {
			return nil, nil, err
		}
		index, err := portindex.Open(root)
		if err != nil {
			return nil, nil, err
		}
		matches, err := index.Select(ctx, portindex.Filter{Maintainers: selection.Maintainers, Categories: selection.Categories})
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range matches.Entries {
			selected = append(selected, selectedPort{label: entry.Name, name: entry.Name, selection: macports.Selection{Selector: entry.Portdir + "/Portfile"}})
		}
		for _, problem := range matches.Problems {
			problems = append(problems, Port{Selector: problem.Port, Result: upstream.Result{Assessment: upstream.Unknown, ObservedAt: time.Now().UTC(), Detail: problem.Detail}})
		}
		progress.Report(ctx, "Selected %d indexed ports; %d selection coverage problems", len(selected), len(matches.Problems))
	}
	return selected, problems, nil
}
