package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// sourceInput is one editing session: the workspace, the evaluated baseline,
// the selected and owning targets, and the original Portfile contents. Fields
// after data are filled in as preparation learns more about the source.
type sourceInput struct {
	scope        *record.ReleaseScope
	versionInput record.ReleaseInput
	files        *workspace
	tree         macports.Tree
	// session is the one native interpreter the input's evaluations share:
	// the baseline, every candidate, and the final edit. Modeled
	// observations never use it; each starts its own interpreter, since
	// observation setup changes the interpreter it runs in.
	session              macports.Batch
	before               macports.Snapshot
	primary, target      record.Target
	info                 macports.PortInfo
	data                 []byte
	platformOperands     []string
	baselineObservations map[observationKey]macports.Observation
}

// load binds the request's selection to a disposable workspace. A stub
// selection, a port that builds nothing while its versioned subports carry
// its release, is redirected to the newest subport as a shared release,
// and the request is updated so the commit keeps the stub's name.
func (s *Service) load(ctx context.Context, request *Request) (_ *sourceInput, err error) {
	if s == nil || s.Ports == nil || request.Root == "" {
		return nil, fmt.Errorf("portedit: a disposable source workspace and MacPorts reader are required")
	}
	files := &workspace{root: request.Root, source: request.Source}

	tree, err := macports.NewTree(request.Source, files.root, request.Platform)
	if err != nil {
		return nil, err
	}
	input := &sourceInput{files: files, tree: tree}
	defer func() {
		if err != nil {
			err = errors.Join(err, input.Close())
		}
	}()
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
	bound, err := tree.Select(targets[0])
	if err != nil {
		return nil, err
	}
	before, err := input.native(ctx, s.Ports).Evaluate(ctx, bound)
	if err != nil {
		return nil, err
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
	data, err := os.ReadFile(files.path(selected.Portfile))
	if err != nil {
		return nil, err
	}
	input.before, input.primary, input.target, input.info, input.data = before, targets[0], selected, info, data
	return input, nil
}

// native is the reader for the input's native evaluations: one interpreter
// session bound to the workspace tree, opened on first use and shared by
// the baseline, every candidate, and the final edit, so a preparation does
// not start an interpreter per evaluation. When a session cannot be opened
// the evaluator itself serves, an interpreter per evaluation.
func (i *sourceInput) native(ctx context.Context, ports macports.Evaluator) snapshotEvaluator {
	if i.session != nil {
		return i.session
	}
	session, err := ports.OpenBatch(ctx, i.tree)
	if err != nil {
		progress.DebugReport(ctx, "evaluating without a shared session: %v", err)
		return ports
	}
	i.session = session
	return session
}

// Close ends the input's shared session, if one was opened.
func (i *sourceInput) Close() error {
	if i == nil || i.session == nil {
		return nil
	}
	session := i.session
	i.session = nil
	return session.Close()
}

// workspace is the exclusively owned, disposable source snapshot an editing
// session probes: never a user checkout. It owns the paths under its root and
// the cycle that writes candidate contents over a file, evaluates, and restores
// the original, so callers never touch the snapshot directly.
type workspace struct {
	root   string
	source record.Source
}

func (w *workspace) path(relative string) string {
	return filepath.Join(w.root, filepath.FromSlash(relative))
}

// withContents runs fn with contents written over the named file, then
// restores the original even when fn fails.
func (w *workspace) withContents(relative string, contents []byte, fn func() error) (err error) {
	path := w.path(relative)
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, contents, 0600); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.WriteFile(path, original, 0600)) }()
	return fn()
}

// portfile is the selected target's Portfile inside the workspace.
func (i *sourceInput) portfile() string { return i.files.path(i.target.Portfile) }

// portdir is the selected target's port directory inside the workspace.
func (i *sourceInput) portdir() string { return filepath.Dir(i.portfile()) }

// context binds the workspace to the owning Portfile, or to the selected
// subport alone for counterfactual probes.
func (i *sourceInput) context(platform record.Platform, selectedOnly bool) (macports.Context, error) {
	target := i.primary
	if selectedOnly {
		target = i.target
	}
	return macports.NewContext(i.files.source, i.files.root, target, platform)
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
	err := input.files.withContents(input.target.Portfile, contents, func() error {
		bound, err := input.context(input.before.Platform, selectedOnly)
		if err != nil {
			return err
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
		return err
	})
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
