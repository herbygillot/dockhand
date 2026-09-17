package portedit

import (
	"bytes"
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/text"
)

func (s *Service) applyObservedArchives(ctx context.Context, request Request, input *sourceInput, plan archivePlan, archives *archiveStore) (Result, error) {
	result := plan.result
	updates := map[text.Span]checksumUpdate{}
	var downloads []Download
	for _, item := range plan.observed.downloads {
		progress.VerboseReport(ctx, "Refreshing %s", item.artifact.Name)
		download, err := archives.fetchFirst(ctx, item.info, item.artifact.Name, item.artifact.URLs)
		if err != nil {
			return result, err
		}
		group := item.artifact.Group
		if group.Legacy() && !request.KeepOldChecksums {
			edit, values := portfile.RewriteChecksumGroup(plan.contents, group.Pairs, checksumValues([]Download{download})[0])
			if _, ok := updates[edit.Span]; !ok {
				progress.Report(ctx, "Modernizing %s checksums: %s -> %s", item.artifact.Name, strings.Join(group.Kinds, " "), strings.Join(portfile.ModernChecksumKinds, " "))
			}
			if err := recordChecksumUpdate(updates, edit.Span, checksumUpdate{text: string(edit.New), values: values}); err != nil {
				return result, err
			}
		} else {
			values := map[string]string{"sha256": download.SHA256, "rmd160": download.RMD160, "size": strconv.FormatInt(download.Size, 10), "md5": download.MD5, "sha1": download.SHA1}
			for kind, token := range group.Values {
				if err := recordChecksumUpdate(updates, token.Span, checksumUpdate{text: values[kind], values: []string{values[kind]}}); err != nil {
					return result, err
				}
			}
		}
		downloads = append(downloads, download)
	}
	var edits []text.Edit
	for span, update := range updates {
		edits = append(edits, text.Edit{Span: span, New: []byte(update.text)})
	}
	contents, err := text.Apply(plan.contents, edits)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	if bytes.Equal(contents, input.data) {
		// Every archive still matches its declared checksums; there is nothing to commit.
		return result, nil
	}
	for _, frame := range plan.observed.contexts {
		final, err := s.observeContents(ctx, input, contents, macports.ObservationRequest{Platform: frame.profile}, false)
		if err != nil {
			return result, err
		}
		checksums := strings.Join(wantedChecksums(frame.binding, updates), " ")
		finalFidelity := fidelity.ScopedChecksums(input.scope, frame.after, final.Snapshot, input.target.Name, input.files.root, checksums)
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
	report := fidelity.ScopedChecksums(input.scope, native.after, final, input.target.Name, input.files.root, strings.Join(wantedChecksums(native.binding, updates), " "))
	if err := result.commitEdit(input, request, evaluated.edit, report, plan.subject); err != nil {
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

// checksumUpdate is the text one span becomes and the evaluated words it
// then produces: one value for an edited checksum, six for a rewritten
// legacy group.
type checksumUpdate struct {
	text   string
	values []string
}

func recordChecksumUpdate(updates map[text.Span]checksumUpdate, span text.Span, update checksumUpdate) error {
	if previous, ok := updates[span]; ok && previous.text != update.text {
		return fmt.Errorf("%w: contexts require different bytes for one checksum declaration", ErrFidelity)
	}
	updates[span] = update
	return nil
}

// wantedChecksums is the checksums option a context evaluates to once the
// updates apply: each group's name, then its rewritten words or its edited
// values in written order.
func wantedChecksums(binding distfiles.Binding, updates map[text.Span]checksumUpdate) []string {
	var wanted []string
	for _, group := range binding.Groups {
		if group.Name != "" {
			wanted = append(wanted, group.Name)
		}
		if update, ok := updates[group.Span()]; ok && group.Legacy() {
			wanted = append(wanted, update.values...)
			continue
		}
		for i, pair := range group.Pairs {
			value := group.Values[group.Kinds[i]].Value
			if update, ok := updates[pair.Value]; ok {
				value = update.text
			}
			wanted = append(wanted, group.Kinds[i], value)
		}
	}
	return wanted
}
