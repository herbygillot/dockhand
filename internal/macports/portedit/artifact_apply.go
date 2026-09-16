package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/text"
	"io"
	"os"
	"strconv"
	"strings"
)

func (s *Service) applyObservedArchives(ctx context.Context, request Request, input *sourceInput, plan archivePlan) (Result, error) {
	result := plan.result
	updates := map[text.Span]string{}
	var downloads []Download
	for _, item := range plan.observed.downloads {
		progress.Report(ctx, "Refreshing %s", item.artifact.Name)
		var download Download
		var err error
		for _, address := range item.artifact.URLs {
			var file *os.File
			var output io.Writer
			if s.archiveDirectory != "" {
				file, err = os.CreateTemp(s.archiveDirectory, "source-*")
				if err != nil {
					return result, err
				}
				output = file
			}
			download, err = s.downloadArchive(ctx, item.info, archiveSource{Name: item.artifact.Name, URL: address}, output)
			if file != nil {
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
				download.path = file.Name()
				if err != nil {
					_ = os.Remove(file.Name())
				}
			}
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
		}
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
		final, err := s.observeContents(ctx, request, input, contents, macports.ObservationRequest{Platform: frame.profile}, false)
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
		finalFidelity := checksumFidelity(frame.after, final.Snapshot, input.target.Name, input.files.Root, input.files.Root, checksums)
		if len(finalFidelity.UnexpectedChanges) > 0 {
			return result, fmt.Errorf("%w: final context %+v: %v", ErrFidelity, frame.profile, finalFidelity.UnexpectedChanges)
		}
	}
	// The final native snapshot is kept last: workflow validates the stored Git tree
	// against native metadata, never a modeled platform.
	edit, final, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return result, err
	}
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
	fidelity := checksumFidelity(native.after, final, input.target.Name, input.files.Root, root, strings.Join(wanted, " "))
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	result.Files = []portfile.Edit{edit}
	result.Downloads = downloads
	result.Fidelity = append(result.Fidelity, fidelity)
	result.Commits = []CommitIntent{{Subject: input.target.Name + ": update to " + request.Release.Version, Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return result, nil
}
