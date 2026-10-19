package portedit

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/textedit"
)

func (s *Service) resetRevision(ctx context.Context, request Request, input *sourceInput, contents []byte) ([]byte, error) {
	profiles, err := input.observe.Profiles(ctx, contents)
	if err != nil {
		return nil, err
	}
	edits := map[textedit.Span]textedit.Edit{}
	selectedVersion := ""
	for _, profile := range profiles {
		mode := macports.ObservationRequest{Platform: profile, Declarations: true}
		before, err := input.observe.One(ctx, input.data, mode, !request.SharedRelease)
		if err != nil {
			return nil, fmt.Errorf("%w: baseline revision evaluation was inconclusive: %w", errProbeInconclusive, err)
		}
		after, err := input.observe.One(ctx, contents, mode, !request.SharedRelease)
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
			edit, err := revisionReset(contents, input.portfileIn(after.Snapshot.Root), next, after.Ports[name].Declarations)
			if err != nil {
				return nil, err
			}
			edits[edit.Span] = edit
		}
	}
	replacements := make([]textedit.Edit, 0, len(edits))
	for _, edit := range edits {
		replacements = append(replacements, edit)
	}
	// Full context and sibling fidelity is checked after applying the complete
	// candidate, including shared revision declarations in protected releases.
	return textedit.Apply(contents, replacements)
}

func revisionReset(contents []byte, path string, info macports.PortInfo, events []macports.Declaration) (textedit.Edit, error) {
	for i := len(events) - 1; i >= 0; i-- {
		d := events[i]
		if d.Command != "revision" {
			continue
		}
		cmd, err := portfile.LocateDeclaration(contents, path, d)
		if err != nil {
			return textedit.Edit{}, err
		}
		if len(cmd.Words) != 2 {
			return textedit.Edit{}, fmt.Errorf("%w: revision is not a literal", ErrUnsupported)
		}
		value, literal := cmd.Words[1].Literal(contents)
		if literal && value == strconv.Itoa(info.Revision) {
			return textedit.Edit{Span: cmd.Words[1].Span, New: []byte("0")}, nil
		}
		// A calculated revision, as the qt family reads from its module
		// table, is owned by the one place the Portfile writes it as the
		// words "revision N": that literal is reset, as a checksum held in
		// a table is refreshed at its one literal.
		if !literal {
			if span, ok := portfile.UniqueLiteral(contents, "revision "+strconv.Itoa(info.Revision)); ok {
				return textedit.Edit{Span: span, New: []byte("revision 0")}, nil
			}
		}
		return textedit.Edit{}, fmt.Errorf("%w: revision is calculated or overridden", ErrUnsupported)
	}
	return textedit.Edit{}, fmt.Errorf("%w: nonzero revision has no editable declaration", ErrUnsupported)
}

// bumpRevision raises the target's revision from current in contents:
// where the Portfile's text has one place to bump (portfile.BumpRevision),
// there; else where the probe finds it, the one literal of the revision's
// value that, raised, raises the target's evaluated revision and so sets
// it, as LyX's is set in each branch of an if, refused as "nonzero
// revision is set outside the supported scope" (the roadmap's v0.5.0 part
// 3, step 2). The evaluated edit is held to the raise, as any revbump is.
func (s *Service) bumpRevision(ctx context.Context, input *sourceInput, contents []byte, current int) ([]byte, error) {
	bumped, err := portfile.BumpRevision(contents, input.target.Subport, current)
	if err == nil || !errors.Is(err, portfile.ErrUnsupported) {
		return bumped, err
	}
	next := strconv.Itoa(current + 1)
	var found [][]byte
	for _, span := range portfile.RevisionCandidates(contents, current) {
		probe, applyErr := textedit.Apply(contents, []textedit.Edit{{Span: span, New: []byte(next)}})
		if applyErr != nil {
			continue
		}
		evaluated, evalErr := s.evaluateCandidate(ctx, input, probe)
		if evalErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if after, ok := evaluated.after.Ports[input.target.Name]; ok && after.Revision == current+1 {
			found = append(found, probe)
		}
	}
	if len(found) != 1 {
		return nil, err
	}
	return found[0], nil
}
