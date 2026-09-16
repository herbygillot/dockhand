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

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

type Evaluator struct {
	Executable string
	Prefix     string
}

//go:embed evaluator.tcl
var evaluatorScript string

//go:embed fetch_credentials.tcl
var fetchCredentialsScript string

func (e *Evaluator) start(ctx context.Context, tree macports.Tree) (*rpc.Session, macports.Runtime, error) {
	executable := e.Executable
	if executable == "" {
		if e.Prefix != "" {
			executable = filepath.Join(e.Prefix, "bin", "port-tclsh")
		} else {
			var err error
			executable, err = exec.LookPath("port-tclsh")
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
	if _, err := session.Call(ctx, "eval", compatibilityScript+"\n"+fetchCredentialsScript+"\n"+observationScript+"\n"+evaluatorScript); err != nil {
		return fail(fmt.Errorf("%w: %w", macports.ErrStartup, err))
	}
	reply, err := session.Call(ctx, "initialize", tree.Root())
	if err != nil {
		return fail(fmt.Errorf("%w: %w", macports.ErrStartup, err))
	}
	runtime, err := decodeRuntime(reply)
	if err != nil {
		return fail(err)
	}
	if tree.Platform() != (record.Platform{}) && tree.Platform() != runtime.Platform {
		return fail(fmt.Errorf("%w: MacPorts Base %s; requested %+v; native %+v", macports.ErrPlatform, runtime.BaseVersion, tree.Platform(), runtime.Platform))
	}
	return session, runtime, nil
}

func (e *Evaluator) NativePlatform(ctx context.Context) (record.Platform, error) {
	runtime, err := e.Inspect(ctx)
	return runtime.Platform, err
}

func (e *Evaluator) Evaluate(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	observation, err := e.evaluate(ctx, source, nil)
	return observation.Snapshot, err
}

func (e *Evaluator) evaluate(ctx context.Context, source macports.Context, request *macports.ObservationRequest) (_ macports.Observation, err error) {
	checked, err := source.Tree.Select(source.Target())
	if err != nil {
		return macports.Observation{}, err
	}
	session, runtime, err := e.start(ctx, checked.Tree)
	if err != nil {
		return macports.Observation{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	if request != nil {
		platform := ""
		if request.Platform != (record.Platform{}) && request.Platform != runtime.Platform {
			platform = request.Platform.OS + " " + request.Platform.Version + " " + request.Platform.Architecture
		}
		if _, err := session.Call(ctx, "observation_setup", platform, strconv.FormatBool(request.Declarations)); err != nil {
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
	if checked.Target().Subport == "" {
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
	snapshot := macports.Snapshot{Source: source.Source(), Target: source.Target(), Platform: runtime.Platform, Runtime: runtime, Ports: ports, ObservedAt: time.Now().UTC()}
	modeled := request != nil && request.Platform != (record.Platform{}) && request.Platform != runtime.Platform
	if modeled {
		snapshot.Platform = request.Platform
	}
	return macports.Observation{Snapshot: snapshot, Modeled: modeled, Ports: observations}, nil
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
		if len(errs) != 0 || len(fields) != 3 {
			return macports.PortInfo{}, nil, fmt.Errorf("macports: invalid fetch metadata")
		}
		values["fetch.archive_compatible"] = "0"
		if archiveFetchCompatible(value, fields[0], fields[1], fields[2]) {
			values["fetch.archive_compatible"] = "1"
		} else {
			value.OptionErrors["fetch.archive_compatible"] = fmt.Sprintf("MacPorts Base %s: fetch procedure or hooks are not recognized for automatic archive preparation; prepare this port manually", values["dockhand.base_version"])
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
