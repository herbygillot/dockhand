package portedit

import (
	"context"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"strings"
)

// separatorMapping establishes a reversible spelling transformation with a
// counterfactual. It is a candidate generator, not an interpretation of Tcl.
func separatorMapping(literal, source, probe, observed string) (string, string, bool) {
	for _, from := range []string{".", "_", "-"} {
		if !strings.Contains(literal, from) {
			continue
		}
		for _, to := range []string{".", "_", "-"} {
			if from != to && strings.ReplaceAll(literal, from, to) == source && strings.ReplaceAll(probe, from, to) == observed {
				return to, from, true
			}
		}
	}
	return "", "", false
}

func (s *Service) evaluateCandidate(ctx context.Context, request Request, input *sourceInput, contents []byte) (portfile.Edit, macports.Snapshot, string, error) {
	selected := *input
	selected.primary = input.target
	return s.evaluateEdit(ctx, request, &selected, contents)
}
