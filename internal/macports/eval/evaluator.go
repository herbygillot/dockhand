package eval

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

type Evaluator struct {
	Executable string
	Prefix     string
	// Model is the macOS an interpreter describes as its own when MacPorts
	// runs on a host that is not a Mac; zero selects DefaultModel. A Mac
	// always describes itself.
	Model record.Platform
}

// DefaultModel is the platform a host that is not a Mac models unless told
// otherwise: the current macOS on Apple silicon.
func DefaultModel() record.Platform {
	return record.Platform{OS: "darwin", Version: strconv.Itoa(macos.CurrentDarwin), Architecture: "arm64"}
}

//go:embed evaluator.tcl
var evaluatorScript string

//go:embed fetch_credentials.tcl
var fetchCredentialsScript string

func (e *Evaluator) start(ctx context.Context, tree macports.Tree) (*rpc.Session, macports.Runtime, error) {
	executable := e.Executable
	if executable == "" {
		if e.Prefix != "" {
			executable = filepath.Join(e.Prefix, "bin", macports.TclShell)
		} else {
			var err error
			executable, err = exec.LookPath(macports.TclShell)
			if err != nil {
				return nil, macports.Runtime{}, fmt.Errorf("%w: %w", macports.ErrStartup, err)
			}
		}
	}
	proc, err := shell.Start(ctx, executable, shell.WithDir(tree.Root()))
	if err != nil {
		return nil, macports.Runtime{}, fmt.Errorf("%w: %w", macports.ErrStartup, err)
	}
	session, err := rpc.New(ctx, proc)
	if err != nil {
		return nil, macports.Runtime{}, fmt.Errorf("%w: %w", macports.ErrStartup, err)
	}
	fail := func(err error) (*rpc.Session, macports.Runtime, error) {
		_ = session.Close()
		return nil, macports.Runtime{}, err
	}
	readOptions := "namespace eval ::dockhand {}\nset ::dockhand::read_options [list " + strings.Join(macports.ReadOptions, " ") + "]\n"
	if _, err := session.Call(ctx, "eval", readOptions+compatibilityScript+"\n"+fetchCredentialsScript+"\n"+platformScript+"\n"+observationScript+"\n"+evaluatorScript); err != nil {
		return fail(fmt.Errorf("%w: %w", macports.ErrStartup, err))
	}
	reply, err := session.Call(ctx, "initialize", tree.Root(), tree.Base())
	if err != nil {
		return fail(fmt.Errorf("%w: %w", macports.ErrStartup, err))
	}
	runtime, err := decodeRuntime(reply)
	if err != nil {
		return fail(err)
	}
	if runtime.Platform.OS != "darwin" {
		if runtime, err = e.model(ctx, session, runtime); err != nil {
			return fail(err)
		}
	}
	if tree.Platform() != (record.Platform{}) && tree.Platform() != runtime.Platform {
		return fail(fmt.Errorf("%w: MacPorts Base %s; requested %+v; native %+v", macports.ErrPlatform, runtime.BaseVersion, tree.Platform(), runtime.Platform))
	}
	return session, runtime, nil
}

// model makes a session on a host that is not a Mac describe a macOS for the
// rest of its life, with the variables an index generated on such a host is
// given, and reads the platform back from the interpreter rather than
// assuming the override took.
func (e *Evaluator) model(ctx context.Context, session *rpc.Session, runtime macports.Runtime) (macports.Runtime, error) {
	model := e.Model
	if model == (record.Platform{}) {
		model = DefaultModel()
	}
	overrides, err := macports.ModelVariables(model)
	if err != nil {
		return runtime, fmt.Errorf("%w: %w", macports.ErrPlatform, err)
	}
	reply, err := session.Call(ctx, "model_platform", overrides, macports.CommandLineTools)
	if err != nil {
		return runtime, fmt.Errorf("%w: %w", macports.ErrStartup, err)
	}
	described, err := decodePlatform(reply)
	if err != nil {
		return runtime, err
	}
	if described != model {
		return runtime, fmt.Errorf("%w: MacPorts Base %s on %+v describes %+v, not the modeled %+v", macports.ErrPlatform, runtime.BaseVersion, runtime.Platform, described, model)
	}
	runtime.Host, runtime.Platform = runtime.Platform, model
	return runtime, nil
}

func (e *Evaluator) NativePlatform(ctx context.Context) (record.Platform, error) {
	runtime, err := e.Inspect(ctx)
	return runtime.Platform, err
}

func (e *Evaluator) Evaluate(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	observation, err := e.evaluate(ctx, source, nil, false)
	return observation.Snapshot, err
}

// EvaluateSelected omits sibling metadata for probes. Full edit validation
// must use Evaluate so unrelated subport changes remain visible.
func (e *Evaluator) EvaluateSelected(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	observation, err := e.evaluate(ctx, source, nil, true)
	return observation.Snapshot, err
}

func (e *Evaluator) evaluate(ctx context.Context, source macports.Context, request *macports.ObservationRequest, selectedOnly bool) (_ macports.Observation, err error) {
	checked, err := source.Tree.Select(source.Target())
	if err != nil {
		return macports.Observation{}, err
	}
	session, runtime, err := e.start(ctx, checked.Tree)
	if err != nil {
		return macports.Observation{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	return evaluateIn(ctx, session, runtime, checked, source, request, selectedOnly)
}

// Session keeps one MacPorts interpreter for repeated evaluations of the same
// tree, such as probing many candidate versions of one Portfile. Each
// evaluation still opens the port afresh, so rewritten contents are observed.
type Session struct {
	tree    macports.Tree
	session *rpc.Session
	runtime macports.Runtime
}

// Open starts an interpreter bound to the tree for Session evaluations.
func (e *Evaluator) Open(ctx context.Context, tree macports.Tree) (*Session, error) {
	session, runtime, err := e.start(ctx, tree)
	if err != nil {
		return nil, err
	}
	return &Session{tree: tree, session: session, runtime: runtime}, nil
}

// OpenBatch satisfies macports.BatchReader.
func (e *Evaluator) OpenBatch(ctx context.Context, tree macports.Tree) (macports.Batch, error) {
	return e.Open(ctx, tree)
}

func (s *Session) Close() error { return s.session.Close() }

func (s *Session) Evaluate(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	observation, err := s.evaluate(ctx, source, false)
	return observation.Snapshot, err
}

func (s *Session) EvaluateSelected(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	observation, err := s.evaluate(ctx, source, true)
	return observation.Snapshot, err
}

func (s *Session) evaluate(ctx context.Context, source macports.Context, selectedOnly bool) (macports.Observation, error) {
	if s == nil || s.session == nil {
		return macports.Observation{}, fmt.Errorf("%w: session is closed", macports.ErrStartup)
	}
	checked, err := source.Tree.Select(source.Target())
	if err != nil {
		return macports.Observation{}, err
	}
	// An overlay of the session's root evaluates in the session: MacPorts
	// resolves _resources from the port directory upward and loads
	// PortGroups per worker interpreter, so the overlay's files are what
	// it reads, and the session's sources setting names the base the
	// overlay is a copy of.
	if checked.Tree.Base() != s.tree.Base() {
		return macports.Observation{}, fmt.Errorf("%w: session is bound to %s, not %s", macports.ErrTarget, s.tree.Base(), checked.Tree.Base())
	}
	return evaluateIn(ctx, s.session, s.runtime, checked, source, nil, selectedOnly)
}

func evaluateIn(ctx context.Context, session *rpc.Session, runtime macports.Runtime, checked, source macports.Context, request *macports.ObservationRequest, selectedOnly bool) (macports.Observation, error) {
	if request != nil {
		for _, operand := range request.Operands {
			if !operandName.MatchString(operand) {
				return macports.Observation{}, fmt.Errorf("macports: invalid observation operand %q", operand)
			}
		}
		overrides := ""
		if request.Platform != (record.Platform{}) && request.Platform != runtime.Platform {
			var err error
			if overrides, err = macports.PlatformVariables(request.Platform); err != nil {
				return macports.Observation{}, err
			}
		}
		if _, err := session.Call(ctx, "observation_setup", overrides, strconv.FormatBool(request.Declarations), strings.Join(request.Operands, " ")); err != nil {
			return macports.Observation{}, err
		}
	}
	observations := map[string]macports.PortObservation{}
	top, subs, err := evaluateOne(ctx, session, checked, checked.Target().Subport)
	if err != nil {
		return macports.Observation{}, err
	}
	if top.Name != checked.Target().Name {
		return macports.Observation{}, fmt.Errorf("%w: expected %s, evaluated %s", macports.ErrTarget, checked.Target().Name, top.Name)
	}
	if request != nil {
		observations[top.Name], err = decodeObservation(top.Options["dockhand.observation"])
		if err != nil {
			return macports.Observation{}, err
		}
	}
	delete(top.Options, "dockhand.observation")
	ports := map[string]macports.PortInfo{top.Name: top}
	if !selectedOnly && checked.Target().Subport == "" {
		for _, sub := range subs {
			if sub == top.Name {
				continue
			}
			value, _, err := evaluateOne(ctx, session, checked, sub)
			if err != nil {
				return macports.Observation{}, err
			}
			if value.Name != sub {
				return macports.Observation{}, fmt.Errorf("%w: expected subport %s, evaluated %s", macports.ErrTarget, sub, value.Name)
			}
			if request != nil {
				observations[sub], err = decodeObservation(value.Options["dockhand.observation"])
				if err != nil {
					return macports.Observation{}, err
				}
			}
			delete(value.Options, "dockhand.observation")
			ports[sub] = value
		}
	}
	snapshot := macports.Snapshot{Source: source.Source(), Target: source.Target(), Platform: runtime.Platform, Runtime: runtime, Ports: ports, Root: source.Root(), ObservedAt: time.Now().UTC()}
	other := request != nil && request.Platform != (record.Platform{}) && request.Platform != runtime.Platform
	if other {
		snapshot.Platform = request.Platform
	}
	// A runtime that models its own platform models every context.
	return macports.Observation{Snapshot: snapshot, Modeled: other || runtime.Modeled(), Ports: observations}, nil
}

func evaluateOne(ctx context.Context, session *rpc.Session, source macports.Context, subport string) (macports.PortInfo, []string, error) {
	variants := source.Target().Variants
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)
	args := []string{filepath.Dir(filepath.Join(source.Root(), source.Target().Portfile)), subport}
	for _, name := range names {
		sign := "-"
		if variants[name] {
			sign = "+"
		}
		args = append(args, name, sign)
	}
	reply, err := session.Call(ctx, "metadata", args...)
	if err != nil {
		return macports.PortInfo{}, nil, fmt.Errorf("macports: evaluating %s (%s): %w", source.Target().Portfile, subport, err)
	}
	return decodeMetadata(reply)
}

func decodeMetadata(reply string) (macports.PortInfo, []string, error) {
	values, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid metadata: %v", errs)
	}
	value := macports.PortInfo{Name: values["name"], Version: values["version"], Options: values}
	failures, errs := syntax.DictValues(values["option_errors"])
	if len(errs) != 0 {
		return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid option errors: %v", errs)
	}
	value.OptionErrors = make(map[string]string, len(failures))
	for name, failure := range failures {
		value.OptionErrors[name] = failure
	}
	delete(values, "option_errors")
	if details, ok := values["fetch_details"]; ok {
		fields, errs := syntax.ListValues(details)
		if len(errs) != 0 || len(fields) < 3 || len(fields) > 4 {
			return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid fetch metadata")
		}
		values["fetch.archive_compatible"] = "0"
		var origins []hookOrigin
		if len(fields) == 4 {
			origins = parseOrigins(fields[3])
		}
		assessment := assessFetch(value, fields[0], fields[1], fields[2], origins)
		value.Fetch = &assessment
		if assessment.Kind != "custom" {
			values["fetch.archive_compatible"] = "1"
		} else {
			value.OptionErrors["fetch.archive_compatible"] = fmt.Sprintf("MacPorts Base %s: %s; prepare this port manually", values["dockhand.base_version"], assessment.Problem)
		}
		delete(values, "fetch_details")
	}
	if !macports.ValidName(value.Name) || value.Version == "" {
		return macports.PortInfo{}, nil, fmt.Errorf("macports: metadata lacks name or version")
	}
	for key, destination := range map[string]*int{"revision": &value.Revision, "epoch": &value.Epoch} {
		number, err := strconv.Atoi(values[key])
		if err != nil || number < 0 {
			return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid %s %q", key, values[key])
		}
		*destination = number
	}
	subs, errs := syntax.ListValues(values["subports"])
	if len(errs) != 0 {
		return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid subport list: %v", errs)
	}
	for _, sub := range subs {
		if !macports.ValidName(sub) {
			return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid subport %q", sub)
		}
	}
	for _, phase := range []string{"fetch", "extract", "patch", "build", "lib", "run", "test"} {
		deps, errs := syntax.ListValues(values["depends_"+phase])
		if len(errs) != 0 {
			return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid %s dependencies: %v", phase, errs)
		}
		for _, spec := range deps {
			fields := strings.SplitN(spec, ":", 3)
			valid := len(fields) == 2 && fields[0] == "port" || len(fields) == 3 && (fields[0] == "path" || fields[0] == "bin" || fields[0] == "lib")
			if !valid || !macports.ValidName(fields[len(fields)-1]) {
				return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid dependency %q", spec)
			}
			value.Dependencies = append(value.Dependencies, macports.Dependency{Port: fields[len(fields)-1], Phase: phase, Spec: spec})
		}
	}
	return value, subs, nil
}

var _ macports.Reader = (*Evaluator)(nil)
