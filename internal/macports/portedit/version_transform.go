package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/text"
	"path/filepath"
	"strconv"
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

func (s *Service) resetRevision(ctx context.Context, request Request, input *sourceInput, contents []byte) ([]byte, error) {
	if input.info.Revision == 0 {
		return contents, nil
	}
	if _, ok := s.Ports.(macports.Observer); !ok {
		return portfile.ResetRevision(contents, input.info.Revision)
	}
	observed, err := s.observeContents(ctx, request, input, contents, macports.ObservationRequest{Declarations: true}, true)
	if err != nil {
		return nil, fmt.Errorf("%w: candidate evaluation was inconclusive: %v", ErrProbeInconclusive, err)
	}
	info := observed.Snapshot.Ports[input.target.Name]
	events := observed.Ports[input.target.Name].Declarations
	for i := len(events) - 1; i >= 0; i-- {
		d := events[i]
		if d.Command != "revision" {
			continue
		}
		cmd, err := portfile.LocateDeclaration(contents, filepath.Join(input.files.Root, input.target.Portfile), d)
		if err != nil {
			return nil, err
		}
		if len(cmd.Words) != 2 {
			return nil, fmt.Errorf("%w: revision is not a literal", ErrUnsupported)
		}
		value, literal := cmd.Words[1].Literal(contents)
		if !literal || value != strconv.Itoa(info.Revision) {
			return nil, fmt.Errorf("%w: revision is calculated or overridden", ErrUnsupported)
		}
		return text.Apply(contents, []text.Edit{{Span: cmd.Words[1].Span, New: []byte("0")}})
	}
	return nil, fmt.Errorf("%w: nonzero revision has no editable declaration", ErrUnsupported)
}
