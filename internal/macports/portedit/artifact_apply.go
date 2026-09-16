package portedit

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/text"
)

func (s *Service) applyObservedArchives(ctx context.Context, request Request, input *sourceInput, plan archivePlan, archives *archiveStore) (Result, error) {
	result := plan.result
	updates := map[text.Span]string{}
	var downloads []Download
	for _, item := range plan.observed.downloads {
		progress.Report(ctx, "Refreshing %s", item.artifact.Name)
		download, err := archives.fetchFirst(ctx, item.info, item.artifact.Name, item.artifact.URLs)
		if err != nil {
			return result, err
		}
		values := map[string]string{"sha256": download.SHA256, "rmd160": download.RMD160, "size": strconv.FormatInt(download.Size, 10)}
		for kind, token := range item.artifact.Group.Values {
			value := values[kind]
			if previous, ok := updates[token.Span]; ok && previous != value {
				return result, fmt.Errorf("%w: contexts require different bytes for one checksum declaration", ErrFidelity)
			}
			updates[token.Span] = value
		}
		downloads = append(downloads, download)
	}
	var edits []text.Edit
	for span, value := range updates {
		edits = append(edits, text.Edit{Span: span, New: []byte(value)})
	}
	contents, err := text.Apply(plan.contents, edits)
	if err != nil {
		return result, err
	}
	for _, frame := range plan.observed.contexts {
		final, err := s.observeContents(ctx, input, contents, macports.ObservationRequest{Platform: frame.profile}, false)
		if err != nil {
			return result, err
		}
		var wanted []string
		for _, token := range frame.binding.Tokens {
			value := token.Value
			if next, ok := updates[token.Span]; ok {
				value = next
			}
			wanted = append(wanted, value)
		}
		checksums := strings.Join(wanted, " ")
		finalFidelity := scopedChecksumFidelity(input.scope, frame.after, final.Snapshot, input.target.Name, input.files.root, checksums)
		if len(finalFidelity.UnexpectedChanges) > 0 {
			return result, fmt.Errorf("%w: final context %+v: %v", ErrFidelity, frame.profile, finalFidelity.UnexpectedChanges)
		}
	}
	// The final native snapshot is kept last: workflow validates the stored Git tree
	// against native metadata, never a modeled platform.
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	final := evaluated.after
	var native archiveContext
	for _, frame := range plan.observed.contexts {
		if frame.profile == input.before.Platform {
			native = frame
			break
		}
	}
	var wanted []string
	for _, token := range native.binding.Tokens {
		value := token.Value
		if next, ok := updates[token.Span]; ok {
			value = next
		}
		wanted = append(wanted, value)
	}
	fidelity := scopedChecksumFidelity(input.scope, native.after, final, input.target.Name, input.files.root, strings.Join(wanted, " "))
	result.Downloads = downloads
	if err := result.commitEdit(input, request, evaluated.edit, fidelity, "update to "+request.Release.Version); err != nil {
		return result, err
	}
	if input.scope != nil {
		result.Scope, err = macports.RebindReleaseScope(input.scope, final)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
