package portedit

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// ProbeSource selects an exclusively owned disposable source workspace.
// Probing restores the Portfile after each evaluation; it does not fetch archives.
type ProbeSource struct {
	record.EditIntent
	Source    record.Source
	Root      string
	Selection macports.Selection
	Platform  record.Platform
}

// VersionProbe binds source metadata and candidate evaluation to one workspace.
// Callers must keep the workspace alive and use the probe sequentially.
type VersionProbe struct {
	editor   *Service
	request  Request
	input    *sourceInput
	carriers versionInputs
}

func (s *Service) Probe(ctx context.Context, source ProbeSource) (*VersionProbe, error) {
	request := Request{EditIntent: source.EditIntent, Source: source.Source, Root: source.Root, Selection: source.Selection, Platform: source.Platform}
	input, err := s.load(ctx, &request)
	if err != nil {
		return nil, err
	}
	return &VersionProbe{editor: s, request: request, input: input}, nil
}

// Stub names the port the selection was made under when that port was a stub
// and the probe redirected to the subport carrying its release, and is empty
// otherwise. A caller holding an indexed name can tell a redirection from a
// port that evaluated as something else.
func (p *VersionProbe) Stub() string { return p.request.Stub }

func (p *VersionProbe) Port() macports.PortInfo {
	info := p.input.info
	info.Options = maps.Clone(info.Options)
	info.OptionErrors = maps.Clone(info.OptionErrors)
	info.Dependencies = slices.Clone(info.Dependencies)
	return info
}

func (p *VersionProbe) prepare(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.carriers != nil {
		return nil
	}
	progress.DebugReport(ctx, "Probing editable version inputs for %s", p.input.target.Name)
	carriers, err := p.editor.versionCarriers(ctx, p.request, p.input)
	if err != nil {
		return err
	}
	p.carriers = carriers
	return nil
}

// EvaluateVersion reports the calculated port version without requiring that the
// candidate is suitable for an automatic edit. Editing fidelity is checked separately.
func (p *VersionProbe) EvaluateVersion(ctx context.Context, value string) (string, error) {
	if err := p.prepare(ctx); err != nil {
		return "", err
	}
	_, snapshot, err := p.editor.evaluateVersion(ctx, p.input.native(ctx, p.editor.Ports), p.request, p.input, p.carriers, value, false)
	if err != nil {
		return "", err
	}
	return snapshot.Ports[p.input.target.Name].Version, nil
}

// Close ends the probe's interpreter session. Callers close a probe when
// they are done with it, as they close the workspace it was bound to.
func (p *VersionProbe) Close() error {
	if p == nil {
		return nil
	}
	return p.input.Close()
}

// EvaluateVersions reports the calculated port version for each source version
// in order, through the probe's shared interpreter session; the evaluation
// itself is unchanged. An error names the value that failed.
func (p *VersionProbe) EvaluateVersions(ctx context.Context, values []string) ([]string, error) {
	if err := p.prepare(ctx); err != nil {
		return nil, err
	}
	reader := p.input.native(ctx, p.editor.Ports)
	results := make([]string, len(values))
	for i, value := range values {
		_, snapshot, err := p.editor.evaluateVersion(ctx, reader, p.request, p.input, p.carriers, value, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", value, err)
		}
		results[i] = snapshot.Ports[p.input.target.Name].Version
	}
	return results, nil
}

// CheckRelease applies the stricter edit-fidelity checks after release selection.
func (p *VersionProbe) CheckRelease(ctx context.Context, release record.Release) error {
	if release.NoUpdate {
		return nil
	}
	if err := p.prepare(ctx); err != nil {
		return err
	}
	spec, err := portsource.Interpret(p.input.info, portsource.Edit)
	if err != nil {
		return err
	}
	raw, ok := spec.Pattern.Version(release.Tag)
	if spec.Forge == "" {
		raw, ok = release.SourceSpelling(), release.Forge == ""
	}
	if !ok {
		return fmt.Errorf("%w: selected tag no longer matches source convention", ErrFidelity)
	}
	_, snapshot, err := p.editor.probeVersion(ctx, p.request, p.input, p.carriers, raw)
	if err != nil {
		return err
	}
	if snapshot.Ports[p.input.target.Name].Version != release.Version {
		return fmt.Errorf("%w: selected version changed during evaluation", ErrFidelity)
	}
	return nil
}
