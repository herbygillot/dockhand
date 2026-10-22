package portedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/pypi"
	"github.com/herbygillot/dockhand/internal/textedit"
)

func (s *Service) applyObservedArchives(ctx context.Context, request Request, input *sourceInput, plan archivePlan, store *distfetch.Store) (Result, error) {
	result := plan.result
	updates := map[textedit.Span]checksumUpdate{}
	var downloads []distfetch.Download
	for _, item := range plan.observed.downloads {
		progress.VerboseReport(ctx, "Refreshing %s", item.artifact.Name)
		download, err := store.FetchFirst(ctx, item.info, item.artifact.Name, item.artifact.URLs)
		if err != nil {
			return result, wheelsOnly(ctx, s.PyPI, item.artifact.Name, item.artifact.URLs, item.info.Version, err)
		}
		if subdir, differs := store.SharedMirrorCopy(ctx, item.info, download); differs && !slices.ContainsFunc(result.MirrorConflicts, func(c MirrorConflict) bool { return c.Name == download.Name }) {
			result.MirrorConflicts = append(result.MirrorConflicts, MirrorConflict{Name: download.Name, Subdir: subdir})
		}
		group := item.artifact.Group
		if differs(group, download) && !slices.Contains(result.Differing, item.artifact.Name) {
			result.Differing = append(result.Differing, item.artifact.Name)
		}
		if group.Legacy() && !request.KeepOldChecksums && !group.Traced() {
			edit, values := portfile.RewriteChecksumGroup(plan.contents, group.Pairs, download.Checksum)
			if _, ok := updates[edit.Span]; !ok {
				progress.Report(ctx, "Modernizing %s checksums: %s -> %s", item.artifact.Name, strings.Join(group.Kinds, " "), strings.Join(portfile.ModernChecksumKinds, " "))
			}
			if err := recordChecksumUpdate(updates, edit.Span, checksumUpdate{text: string(edit.New), values: values}); err != nil {
				return result, err
			}
		} else {
			for kind, token := range group.Values {
				value, _ := download.Value(kind)
				if err := recordChecksumUpdate(updates, token.Span, checksumUpdate{text: value, values: []string{value}}); err != nil {
					return result, err
				}
			}
		}
		downloads = append(downloads, download)
	}
	var edits []textedit.Edit
	for span, update := range updates {
		edits = append(edits, textedit.Edit{Span: span, New: []byte(update.text)})
	}
	contents, err := textedit.Apply(plan.contents, edits)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	if bytes.Equal(contents, input.data) {
		// Every archive still matches its declared checksums; there is nothing to commit.
		return result, nil
	}
	finals, err := observeFinal(ctx, input, plan.observed.contexts, contents)
	if err != nil {
		return result, err
	}
	// Each context is checked once, every member that writes its own
	// checksums there predicted with them.
	type contextKey struct {
		profile model.Platform
		variant string
	}
	wanted := map[contextKey]map[string]string{}
	for _, frame := range plan.observed.contexts {
		key := contextKey{frame.profile, frame.variant}
		if wanted[key] == nil {
			wanted[key] = map[string]string{}
		}
		wanted[key][frame.port] = strings.Join(wantedChecksums(frame.binding, updates), " ")
	}
	checked := map[contextKey]bool{}
	for i, frame := range plan.observed.contexts {
		key := contextKey{frame.profile, frame.variant}
		if checked[key] {
			continue
		}
		checked[key] = true
		finalFidelity := scopedChecksums(input, frame.after, finals[i].Snapshot, wanted[key])
		if len(finalFidelity.UnexpectedChanges) > 0 {
			return result, fmt.Errorf("%w: final context %+v: %s", ErrFidelity, frame.profile, strings.Join(finalFidelity.UnexpectedChanges, "; "))
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
		if frame.profile == input.before.Platform && frame.variant == "" {
			native = frame
			break
		}
	}
	report := scopedChecksums(input, native.after, final, wanted[contextKey{native.profile, native.variant}])
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

// scopedChecksums is the checksums step's prediction, observed: each
// member's own checksums where it writes some, the target's alone where
// the update has no release scope.
func scopedChecksums(input *sourceInput, before, after macports.Snapshot, wanted map[string]string) fidelity.Report {
	if input.scope == nil {
		return fidelity.Checksums(before, after, input.target.Name, wanted[input.target.Name])
	}
	return fidelity.ScopedChecksumsOf(input.scope, before, after, wanted)
}

// MirrorConflict is an archive MacPorts' mirror already holds another file
// of, under a dist_subdir other ports share (distfetch.SharedMirrorCopy).
type MirrorConflict struct {
	Name   string `json:"name"`
	Subdir string `json:"subdir"`
}

// differs reports a download whose checksums aren't what its group
// declares.
func differs(group distfiles.Group, download distfetch.Download) bool {
	for kind, token := range group.Values {
		if value, ok := download.Value(kind); !ok || value != token.Value {
			return true
		}
	}
	return false
}

// checksumUpdate is the text one span becomes and the evaluated words it
// then produces: one value for an edited checksum, six for a rewritten
// legacy group.
type checksumUpdate struct {
	text   string
	values []string
}

func recordChecksumUpdate(updates map[textedit.Span]checksumUpdate, span textedit.Span, update checksumUpdate) error {
	if previous, ok := updates[span]; ok && previous.text != update.text {
		return fmt.Errorf("%w: contexts require different bytes for one checksum declaration", ErrFidelity)
	}
	updates[span] = update
	return nil
}

// wantedChecksums is the checksums option a context evaluates to once the
// updates apply: each group's name, then its rewritten words or its edited
// values in written order.
func wantedChecksums(binding distfiles.Binding, updates map[textedit.Span]checksumUpdate) []string {
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

// observeFinal observes the edited contents in each context the plan
// observed, a profile's as the input's, and a variant's with its own
// session, in the contexts' order.
func observeFinal(ctx context.Context, input *sourceInput, contexts []archiveContext, contents []byte) ([]macports.Observation, error) {
	finals := make([]macports.Observation, len(contexts))
	var profiles []model.Platform
	var at []int
	for i, frame := range contexts {
		if frame.variant == "" {
			profiles, at = append(profiles, frame.profile), append(at, i)
			continue
		}
		observed, err := frame.session.Observe(ctx, contents, []model.Platform{frame.profile}, false, false)
		if err != nil {
			return nil, fmt.Errorf("+%s: %w", frame.variant, err)
		}
		finals[i] = observed[0]
	}
	observed, err := input.observe.Observe(ctx, contents, profiles, false, false)
	if err != nil {
		return nil, err
	}
	for k, i := range at {
		finals[i] = observed[k]
	}
	return finals, nil
}

// wheelsOnly says, of a source archive PyPI doesn't serve, where PyPI
// publishes only wheels for the release, and never will a source archive:
// py-flatbuffers 25.12.19's update said its sdist wasn't "published at
// that location yet", which it replaces. What PyPI publishes is its JSON
// API's to say; where it can't, or the release has a source archive, err
// stands as it is.
func wheelsOnly(ctx context.Context, index pypi.Client, name string, urls []string, version string, err error) error {
	var status *fetch.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		return err
	}
	for _, address := range urls {
		project, ok := pypi.SourceProject(address)
		if !ok {
			continue
		}
		files, lookErr := index.Files(ctx, project, version)
		if lookErr != nil || len(files) == 0 || slices.ContainsFunc(files, func(f pypi.File) bool { return f.PackageType == "sdist" }) {
			return err
		}
		return fmt.Errorf("archives: downloading %s: %w; PyPI has only wheels for %s %s. Update the Portfile to use another source archive, such as one from the project's release page", name, status, project, version)
	}
	return err
}
