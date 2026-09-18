package portedit

import (
	"context"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/text"
)

func (s *Service) resetRevision(ctx context.Context, request Request, input *sourceInput, contents []byte) ([]byte, error) {
	if _, ok := s.Ports.(macports.Observer); !ok {
		if input.info.Revision == 0 {
			return contents, nil
		}
		return portfile.ResetRevision(contents, input.info.Revision)
	}
	profiles, err := s.contextProfiles(ctx, request, input, contents)
	if err != nil {
		return nil, err
	}
	edits := map[text.Span]text.Edit{}
	selectedVersion := ""
	for _, profile := range profiles {
		mode := macports.ObservationRequest{Platform: profile, Declarations: true}
		before, err := s.observeContents(ctx, input, input.data, mode, !request.SharedRelease)
		if err != nil {
			return nil, fmt.Errorf("%w: baseline revision evaluation was inconclusive: %w", errProbeInconclusive, err)
		}
		after, err := s.observeContents(ctx, input, contents, mode, !request.SharedRelease)
		if err != nil {
			return nil, fmt.Errorf("%w: candidate revision evaluation was inconclusive: %w", errProbeInconclusive, err)
		}
		if selectedVersion == "" {
			selectedVersion = after.Snapshot.Ports[input.target.Name].Version
		}
		for name, next := range after.Snapshot.Ports {
			old := before.Snapshot.Ports[name]
			if old.Version == next.Version {
				continue
			}
			if old.Version != input.info.Version || next.Version != selectedVersion {
				return nil, fmt.Errorf("%w: revision reset would affect independent release %s on %+v", ErrFidelity, name, profile)
			}
			if next.Revision == 0 {
				continue
			}
			edit, err := revisionReset(contents, input.portfile(), next, after.Ports[name].Declarations)
			if err != nil {
				return nil, err
			}
			edits[edit.Span] = edit
		}
	}
	replacements := make([]text.Edit, 0, len(edits))
	for _, edit := range edits {
		replacements = append(replacements, edit)
	}
	// Full context and sibling fidelity is checked after applying the complete
	// candidate, including shared revision declarations in protected releases.
	return text.Apply(contents, replacements)
}

func revisionReset(contents []byte, path string, info macports.PortInfo, events []macports.Declaration) (text.Edit, error) {
	for i := len(events) - 1; i >= 0; i-- {
		d := events[i]
		if d.Command != "revision" {
			continue
		}
		cmd, err := portfile.LocateDeclaration(contents, path, d)
		if err != nil {
			return text.Edit{}, err
		}
		if len(cmd.Words) != 2 {
			return text.Edit{}, fmt.Errorf("%w: revision is not a literal", ErrUnsupported)
		}
		value, literal := cmd.Words[1].Literal(contents)
		if literal && value == strconv.Itoa(info.Revision) {
			return text.Edit{Span: cmd.Words[1].Span, New: []byte("0")}, nil
		}
		// A calculated revision, as the qt family reads from its module
		// table, is owned by the one place the Portfile writes it as the
		// words "revision N": that literal is reset, as a checksum held in
		// a table is refreshed at its one literal.
		if !literal {
			if span, ok := portfile.UniqueLiteral(contents, "revision "+strconv.Itoa(info.Revision)); ok {
				return text.Edit{Span: span, New: []byte("revision 0")}, nil
			}
		}
		return text.Edit{}, fmt.Errorf("%w: revision is calculated or overridden", ErrUnsupported)
	}
	return text.Edit{}, fmt.Errorf("%w: nonzero revision has no editable declaration", ErrUnsupported)
}
