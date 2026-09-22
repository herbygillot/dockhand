package portedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// session is one editing session: the workspace and its bound tree, the
// native interpreter every evaluation of the session shares, and the
// candidate overlays it made, closed together. Every baseline of one
// preparation, the Portfile as loaded and any derived from it, shares one
// session, and only the input that opened it closes it.
type session struct {
	ws   *workspace.Workspace
	tree macports.Tree
	// batch is the one native interpreter the session's evaluations share:
	// the baseline, every candidate, and the final edit, opened on first
	// use. Modeled observations never use it; each starts its own
	// interpreter, since observation setup changes the interpreter it
	// runs in.
	batch macports.Batch
	// loaded is the target's Portfile as the workspace holds it on disk,
	// the only contents that evaluate there directly.
	loaded []byte
	// overlays are the candidate projections the session made; they live
	// as long as the session does, since evaluated paths such as filespath
	// name them. byContents finds the overlay already made for the same
	// contents, which a candidate evaluated and then observed asks for
	// twice.
	overlays   []*workspace.Workspace
	byContents map[[32]byte]*workspace.Workspace
}

// sourceInput is one baseline of an editing session: the evaluated
// snapshot, the selected and owning targets, the Portfile contents the
// edit starts from, and what preparation learns about the source. A
// derived baseline, the Portfile stripped of its generated declarations
// or another member of a shared release, is a value of its own that shares
// the session.
type sourceInput struct {
	*session
	scope        *record.ReleaseScope
	versionInput record.ReleaseInput
	before       macports.Snapshot
	// family is the baseline across the owning Portfile's every subport,
	// known at load for a main-port selection and evaluated on demand for a
	// subport's, by familySnapshot.
	family          *macports.Snapshot
	primary, target record.Target
	info            macports.PortInfo
	// data is the baseline Portfile contents, which a derived baseline
	// replaces with a stripped form.
	data []byte
	// observe runs the baseline's modeled observations; its projections
	// are the session's overlays.
	observe *observe.Session
}

// derive is the baseline of the same session with other contents: the
// Portfile stripped of its generated declarations, evaluated as given. The
// family follows the derived baseline, since it is what the regenerated
// declarations are compared against, and the release scope and version
// input found so far carry over.
func (i *sourceInput) derive(data []byte, before macports.Snapshot) *sourceInput {
	derived := &sourceInput{session: i.session, scope: i.scope, versionInput: i.versionInput, before: before, primary: i.primary, target: i.target, info: before.Ports[i.target.Name], data: data, observe: i.observe.WithBaseline(data)}
	derived.family = &derived.before
	return derived
}

// forMember is the same baseline seen from another member of the shared
// release, whose own declarations the archive checks bind.
func (i *sourceInput) forMember(target record.Target) *sourceInput {
	member := *i
	member.target = target
	return &member
}

// load binds the request's selection to a disposable workspace. A stub
// selection, a port that builds nothing while its versioned subports carry
// its release, is redirected to the newest subport as a shared release,
// and the request is updated so the commit keeps the stub's name.
func (s *Service) load(ctx context.Context, request *Request) (_ *sourceInput, err error) {
	if s == nil || s.Ports == nil || request.Workspace == nil {
		return nil, fmt.Errorf("portedit: a source workspace and MacPorts reader are required")
	}
	ws := request.Workspace
	tree, err := ws.Tree(request.Platform)
	if err != nil {
		return nil, err
	}
	input := &sourceInput{session: &session{ws: ws, tree: tree}}
	defer func() {
		if err != nil {
			err = errors.Join(err, input.Close())
		}
	}()
	// A selector that names the port directory is resolved on disk, so a
	// sparse workspace brings that directory first; a bare name resolves
	// through the index, which widens the workspace itself when it must
	// generate one.
	if directory := selectedDirectory(request.Selection.Selector); directory != "" {
		if err := ws.EnsurePort(ctx, record.Target{Portfile: directory + "/Portfile"}); err != nil {
			return nil, err
		}
	}
	targets, err := s.Ports.Resolve(ctx, tree, request.Selection)
	if err != nil {
		return nil, err
	}
	if len(targets) != 1 {
		return nil, fmt.Errorf("%w: select one port", ErrUnsupported)
	}
	selected := targets[0]
	if selected.Subport != "" {
		targets, err = s.Ports.Resolve(ctx, tree, macports.Selection{Selector: selected.Portfile, Variants: selected.Variants})
		if err != nil {
			return nil, err
		}
		if len(targets) != 1 || targets[0].Subport != "" {
			return nil, fmt.Errorf("%w: select one owning Portfile", ErrUnsupported)
		}
	}
	// The owning Portfile's directory and _resources are what the
	// evaluations read; a sparse workspace brings them now.
	if err := ws.EnsurePort(ctx, targets[0]); err != nil {
		return nil, err
	}
	primary, err := tree.Select(targets[0])
	if err != nil {
		return nil, err
	}
	// The baseline is the selected port's own evaluation. A main-port
	// selection evaluates its whole Portfile, since stub detection reads the
	// siblings and an edit there is what they share; a subport's siblings
	// are evaluated on demand by familySnapshot, for the fidelity checks
	// that must see them, and never for an assessment that edits nothing.
	bound := primary
	var before macports.Snapshot
	if selected.Subport == "" {
		before, err = input.native(ctx, s.Ports).Evaluate(ctx, bound)
		if err != nil {
			return nil, err
		}
	} else {
		if bound, err = tree.Select(selected); err != nil {
			return nil, err
		}
		before, err = input.native(ctx, s.Ports).EvaluateSelected(ctx, bound)
		if err != nil {
			return nil, err
		}
		if request.Stub != "" {
			// The recorded stub is the main port: its entry is checked
			// below and lends its livecheck to the carrier.
			owner, err := input.native(ctx, s.Ports).EvaluateSelected(ctx, primary)
			if err != nil {
				return nil, err
			}
			maps.Copy(before.Ports, owner.Ports)
		}
	}
	if err := fidelity.CheckSnapshot(before, bound); err != nil {
		return nil, err
	}
	var stub string
	if request.Stub != "" {
		// The selection was resolved when the job was bound: the target is
		// the carrying subport and the stub's name is recorded. Honor it.
		if _, ok := before.Ports[request.Stub]; !ok || selected.Name == request.Stub || selected.Subport == "" {
			return nil, fmt.Errorf("%w: recorded stub %s does not match the selected subport %s", ErrUnsupported, request.Stub, selected.Name)
		}
		stub = request.Stub
		request.SharedRelease = true
	} else if carrier, name := macports.ResolveStub(before, selected); name != "" {
		progress.Report(ctx, "%s is a stub; editing %s and its sibling subports as one release", name, carrier.Name)
		stub = name
		request.Stub, request.SharedRelease = name, true
		selected = carrier
	}
	if selected.Subport == "" {
		// A main port's release is the Portfile's release: a subport that
		// shares the main port's version moves with it by construction, the
		// way atuin-server moves with atuin, and the person reviews the whole
		// edit. Only a named subport needs --shared-release to move its
		// siblings.
		request.SharedRelease = true
	}
	info, ok := before.Ports[selected.Name]
	if !ok {
		return nil, fmt.Errorf("%w: subport %s was not evaluated", ErrUnsupported, selected.Name)
	}
	if stub != "" {
		// The stub owns the livecheck; MacPorts disables it on the subports
		// that share the stub's version. Discovery borrows it for the subport
		// that carries the edit, since the release is one and the same.
		info = withLivecheckOf(info, before.Ports[stub])
	}
	data, err := os.ReadFile(filepath.Join(ws.Root(), filepath.FromSlash(selected.Portfile)))
	if err != nil {
		return nil, err
	}
	input.before, input.primary, input.target, input.info, input.data, input.loaded = before, targets[0], selected, info, data, data
	input.observe = &observe.Session{Ports: s.Ports, Primary: targets[0], Target: selected, Native: before.Platform, Baseline: data, Project: input.projection}
	if selected.Subport == "" {
		input.family = &input.before
	}
	return input, nil
}

// familySnapshot is the baseline across the owning Portfile's every subport,
// which the fidelity checks compare an edit against so a sibling that moved
// unexpectedly is seen. A main-port selection evaluated it at load; a
// subport's is evaluated on first demand through the shared session and
// kept, so an assessment that never edits never pays for it.
func (i *sourceInput) familySnapshot(ctx context.Context, ports macports.Evaluator) (macports.Snapshot, error) {
	if i.family != nil {
		return *i.family, nil
	}
	bound, err := i.context(i.before.Platform, false)
	if err != nil {
		return macports.Snapshot{}, err
	}
	snapshot, err := i.native(ctx, ports).Evaluate(ctx, bound)
	if err != nil {
		return macports.Snapshot{}, err
	}
	if err := fidelity.CheckSnapshot(snapshot, bound); err != nil {
		return macports.Snapshot{}, err
	}
	i.family = &snapshot
	return snapshot, nil
}

// native is the reader for the session's native evaluations: one
// interpreter bound to the workspace tree, opened on first use and shared
// by the baseline, every candidate, and the final edit, so a preparation
// does not start an interpreter per evaluation. When an interpreter cannot
// be opened the evaluator itself serves, an interpreter per evaluation.
func (s *session) native(ctx context.Context, ports macports.Evaluator) snapshotEvaluator {
	if s.batch != nil {
		return s.batch
	}
	batch, err := ports.OpenBatch(ctx, s.tree)
	if err != nil {
		progress.DebugReport(ctx, "Evaluating without a shared session: %v", err)
		return ports
	}
	s.batch = batch
	return batch
}

// Close ends the session's interpreter, if one was opened, and removes the
// candidate overlays it made. The workspace itself is the caller's.
func (s *session) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.batch != nil {
		batch := s.batch
		s.batch = nil
		err = batch.Close()
	}
	overlays := s.overlays
	s.overlays, s.byContents = nil, nil
	for _, overlay := range overlays {
		err = errors.Join(err, overlay.Close())
	}
	return err
}

// Close ends the session the input opened. A derived baseline shares its
// session and is not closed on its own.
func (i *sourceInput) Close() error {
	if i == nil {
		return nil
	}
	return i.session.Close()
}

// portfile is the selected target's Portfile inside the workspace.
func (i *sourceInput) portfile() string {
	return filepath.Join(i.ws.Root(), filepath.FromSlash(i.target.Portfile))
}

// portdir is the selected target's port directory inside the workspace.
func (i *sourceInput) portdir() string { return filepath.Dir(i.portfile()) }

// portdirIn is the target's port directory inside the projection an
// evaluation ran in, which is what its evaluated paths such as filespath
// name.
func (i *sourceInput) portdirIn(root string) string { return filepath.Dir(i.portfileIn(root)) }

// portfileIn is the target's Portfile inside the projection an evaluation
// ran in, which is what its declarations' source locations name.
func (i *sourceInput) portfileIn(root string) string {
	if root == "" {
		return i.portfile()
	}
	return filepath.Join(root, filepath.FromSlash(i.target.Portfile))
}

// context binds the workspace to the owning Portfile, or to the selected
// subport alone for counterfactual probes.
func (i *sourceInput) context(platform record.Platform, selectedOnly bool) (macports.Context, error) {
	return i.contextIn(i.ws, platform, selectedOnly)
}

// contextIn binds a projection, the workspace or one of its overlays.
func (i *sourceInput) contextIn(ws *workspace.Workspace, platform record.Platform, selectedOnly bool) (macports.Context, error) {
	target := i.primary
	if selectedOnly {
		target = i.target
	}
	return ws.Context(target, platform)
}

// projection is the workspace itself for the contents it holds on disk,
// and an overlay with the contents written over the target's Portfile
// otherwise. Overlays are kept until the session closes.
func (i *sourceInput) projection(ctx context.Context, contents []byte) (*workspace.Workspace, error) {
	return i.session.overlay(ctx, i.target.Portfile, contents)
}

// overlay is the workspace for the contents it holds, and otherwise the
// overlay already made for the same contents or a new one.
func (s *session) overlay(ctx context.Context, path string, contents []byte) (*workspace.Workspace, error) {
	if bytes.Equal(contents, s.loaded) {
		return s.ws, nil
	}
	key := sha256.Sum256(contents)
	if overlay, ok := s.byContents[key]; ok {
		return overlay, nil
	}
	overlay, err := s.ws.Overlay(ctx, []git.FileEdit{{Path: path, After: contents}})
	if err != nil {
		return nil, err
	}
	if s.byContents == nil {
		s.byContents = map[[32]byte]*workspace.Workspace{}
	}
	s.byContents[key] = overlay
	s.overlays = append(s.overlays, overlay)
	return overlay, nil
}

// evaluation is one candidate's edit and its evaluated snapshot.
type evaluation struct {
	edit  portfile.Edit
	after macports.Snapshot
}

func (s *Service) evaluateEdit(ctx context.Context, input *sourceInput, contents []byte) (evaluation, error) {
	return s.evaluateContents(ctx, input.native(ctx, s.Ports), input, contents, false)
}

// evaluateCandidate omits sibling metadata for probes; full edit validation
// uses evaluateEdit so unrelated subport changes remain visible.
func (s *Service) evaluateCandidate(ctx context.Context, input *sourceInput, contents []byte) (evaluation, error) {
	return s.evaluateContents(ctx, input.native(ctx, s.Ports), input, contents, true)
}

// snapshotEvaluator is the reader used for one evaluation: the service's
// evaluator, or a session bound to the workspace tree.
type snapshotEvaluator interface {
	Evaluate(context.Context, macports.Context) (macports.Snapshot, error)
	EvaluateSelected(context.Context, macports.Context) (macports.Snapshot, error)
}

func (s *Service) evaluateContents(ctx context.Context, reader snapshotEvaluator, input *sourceInput, contents []byte, selectedOnly bool) (evaluation, error) {
	result := evaluation{edit: portfile.Edit{Path: input.target.Portfile, After: contents}}
	projection, err := input.projection(ctx, contents)
	if err != nil {
		return result, err
	}
	bound, err := input.contextIn(projection, input.before.Platform, selectedOnly)
	if err != nil {
		return result, err
	}
	var after macports.Snapshot
	if selectedOnly {
		after, err = reader.EvaluateSelected(ctx, bound)
	} else {
		after, err = reader.Evaluate(ctx, bound)
	}
	if err == nil {
		err = fidelity.CheckSnapshot(after, bound)
	}
	// Probe snapshots describe uncommitted contents, not the immutable base tree.
	after.Source = record.Source{}
	result.after = after
	return result, err
}

// withLivecheckOf returns port with owner's livecheck declarations in place
// of its own.
func withLivecheckOf(port, owner macports.PortInfo) macports.PortInfo {
	// A member that checks upstream itself keeps its own livecheck: the
	// ruby PortGroup's stub returns before declaring one and its subports
	// each declare the RubyGems check, the reverse of the python shape.
	if own := port.Options["livecheck.type"]; (own == "regex" || own == "regexm") && port.Options["livecheck.regex"] != "" && port.OptionErrors["livecheck.regex"] == "" {
		return port
	}
	port.Options = maps.Clone(port.Options)
	if port.Options == nil {
		port.Options = map[string]string{}
	}
	port.OptionErrors = maps.Clone(port.OptionErrors)
	for key, value := range owner.Options {
		if strings.HasPrefix(key, "livecheck.") || strings.HasPrefix(key, "dockhand.livecheck_") {
			port.Options[key] = value
			delete(port.OptionErrors, key)
		}
	}
	for key, value := range owner.OptionErrors {
		if strings.HasPrefix(key, "livecheck.") {
			if port.OptionErrors == nil {
				port.OptionErrors = map[string]string{}
			}
			port.OptionErrors[key] = value
		}
	}
	return port
}

// selectedDirectory is the category/port directory a selector names, for a
// category/port or category/port/Portfile selector, and empty for a name.
func selectedDirectory(selector string) string {
	parts := strings.Split(selector, "/")
	switch {
	case len(parts) == 3 && parts[2] == "Portfile", len(parts) == 2:
		if parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[0], ".") || parts[0] == ".." || parts[1] == ".." {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return ""
}
