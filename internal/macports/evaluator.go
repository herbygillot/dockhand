package macports

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
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/v2/internal/tcl/shell"
	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
)

var (
	ErrStartup  = errors.New("macports: evaluator startup failed")
	ErrPlatform = errors.New("macports: evaluation requires the native platform")
	ErrTarget   = errors.New("macports: target could not be resolved")
)

type Dependency struct {
	Port  string
	Phase string
	Spec  string
}

type PortInfo struct {
	Name         string
	Version      string
	Revision     int
	Epoch        int
	Options      map[string]string
	OptionErrors map[string]string
	Dependencies []Dependency
}

type Snapshot struct {
	Source     record.Source
	Target     record.Target
	Platform   record.Platform
	Ports      map[string]PortInfo
	ObservedAt time.Time
}

type Selection struct {
	Selector string
	Subport  string
	Variants map[string]bool
}

type Reader interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	Resolve(context.Context, Tree, Selection) ([]record.Target, error)
}

type Evaluator struct {
	Executable string
	Prefix     string
}

//go:embed evaluator.tcl
var evaluatorScript string

func (e *Evaluator) start(ctx context.Context, tree Tree) (*rpc.Session, record.Platform, error) {
	executable := e.Executable
	if executable == "" {
		if e.Prefix != "" {
			executable = filepath.Join(e.Prefix, "bin", "port-tclsh")
		} else {
			var err error
			executable, err = exec.LookPath("port-tclsh")
			if err != nil {
				return nil, record.Platform{}, fmt.Errorf("%w: %w", ErrStartup, err)
			}
		}
	}
	proc, err := shell.Start(ctx, executable, shell.WithDir(tree.root))
	if err != nil {
		return nil, record.Platform{}, fmt.Errorf("%w: %w", ErrStartup, err)
	}
	session, err := rpc.New(ctx, proc)
	if err != nil {
		return nil, record.Platform{}, fmt.Errorf("%w: %w", ErrStartup, err)
	}
	fail := func(err error) (*rpc.Session, record.Platform, error) {
		_ = session.Close()
		return nil, record.Platform{}, err
	}
	if _, err := session.Call(ctx, "eval", evaluatorScript); err != nil {
		return fail(fmt.Errorf("%w: %w", ErrStartup, err))
	}
	reply, err := session.Call(ctx, "initialize", tree.root)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", ErrStartup, err))
	}
	values, errs := syntax.ListValues(reply)
	if len(errs) != 0 || len(values) != 3 {
		return fail(fmt.Errorf("%w: invalid platform reply %q", ErrStartup, reply))
	}
	platform := record.Platform{OS: values[0], Version: values[1], Architecture: values[2]}
	if platform.OS == "" || platform.Version == "" || platform.Architecture == "" {
		return fail(fmt.Errorf("%w: incomplete native platform", ErrStartup))
	}
	if tree.platform != (record.Platform{}) && tree.platform != platform {
		return fail(fmt.Errorf("%w: requested %+v; native %+v", ErrPlatform, tree.platform, platform))
	}
	return session, platform, nil
}

func (e *Evaluator) NativePlatform(ctx context.Context) (record.Platform, error) {
	session, platform, err := e.start(ctx, Tree{})
	if err != nil {
		return record.Platform{}, err
	}
	return platform, session.Close()
}

func (e *Evaluator) Evaluate(ctx context.Context, source Context) (_ Snapshot, err error) {
	checked, err := source.Tree.Select(source.Target())
	if err != nil {
		return Snapshot{}, err
	}
	session, platform, err := e.start(ctx, checked.Tree)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	top, subs, err := evaluateOne(ctx, session, checked, checked.target.Subport)
	if err != nil {
		return Snapshot{}, err
	}
	if top.Name != checked.target.Name {
		return Snapshot{}, fmt.Errorf("%w: expected %s, evaluated %s", ErrTarget, checked.target.Name, top.Name)
	}
	ports := map[string]PortInfo{top.Name: top}
	if checked.target.Subport == "" {
		for _, sub := range subs {
			if sub == top.Name {
				continue
			}
			value, _, err := evaluateOne(ctx, session, checked, sub)
			if err != nil {
				return Snapshot{}, err
			}
			if value.Name != sub {
				return Snapshot{}, fmt.Errorf("%w: expected subport %s, evaluated %s", ErrTarget, sub, value.Name)
			}
			ports[sub] = value
		}
	}
	return Snapshot{Source: source.Source(), Target: source.Target(), Platform: platform, Ports: ports, ObservedAt: time.Now().UTC()}, nil
}

func evaluateOne(ctx context.Context, session *rpc.Session, source Context, subport string) (PortInfo, []string, error) {
	variants := source.target.Variants
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)
	args := []string{filepath.Dir(filepath.Join(source.root, source.target.Portfile)), subport}
	for _, name := range names {
		sign := "-"
		if variants[name] {
			sign = "+"
		}
		args = append(args, name, sign)
	}
	reply, err := session.Call(ctx, "metadata", args...)
	if err != nil {
		return PortInfo{}, nil, fmt.Errorf("macports: evaluating %s (%s): %w", source.target.Portfile, subport, err)
	}
	return decodeMetadata(reply)
}

func decodeMetadata(reply string) (PortInfo, []string, error) {
	raw, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return PortInfo{}, nil, fmt.Errorf("macports: invalid metadata: %v", errs)
	}
	values := make(map[string]string, len(raw))
	for key, value := range raw {
		values[key] = syntax.ListValue(value)
	}
	value := PortInfo{Name: values["name"], Version: values["version"], Options: values}
	failures, errs := syntax.DictValues(values["option_errors"])
	if len(errs) != 0 {
		return PortInfo{}, nil, fmt.Errorf("macports: invalid option errors: %v", errs)
	}
	value.OptionErrors = make(map[string]string, len(failures))
	for name, failure := range failures {
		value.OptionErrors[name] = syntax.ListValue(failure)
	}
	delete(values, "option_errors")
	if !token(value.Name) || value.Version == "" {
		return PortInfo{}, nil, fmt.Errorf("macports: metadata lacks name or version")
	}
	for key, destination := range map[string]*int{"revision": &value.Revision, "epoch": &value.Epoch} {
		number, err := strconv.Atoi(values[key])
		if err != nil || number < 0 {
			return PortInfo{}, nil, fmt.Errorf("macports: invalid %s %q", key, values[key])
		}
		*destination = number
	}
	subs, errs := syntax.ListValues(values["subports"])
	if len(errs) != 0 {
		return PortInfo{}, nil, fmt.Errorf("macports: invalid subport list: %v", errs)
	}
	for _, sub := range subs {
		if !token(sub) {
			return PortInfo{}, nil, fmt.Errorf("macports: invalid subport %q", sub)
		}
	}
	for _, phase := range []string{"fetch", "extract", "patch", "build", "lib", "run", "test"} {
		deps, errs := syntax.ListValues(values["depends_"+phase])
		if len(errs) != 0 {
			return PortInfo{}, nil, fmt.Errorf("macports: invalid %s dependencies: %v", phase, errs)
		}
		for _, spec := range deps {
			fields := strings.SplitN(spec, ":", 3)
			valid := len(fields) == 2 && fields[0] == "port" || len(fields) == 3 && (fields[0] == "path" || fields[0] == "bin" || fields[0] == "lib")
			if !valid || !token(fields[len(fields)-1]) {
				return PortInfo{}, nil, fmt.Errorf("macports: invalid dependency %q", spec)
			}
			value.Dependencies = append(value.Dependencies, Dependency{Port: fields[len(fields)-1], Phase: phase, Spec: spec})
		}
	}
	return value, subs, nil
}

func token(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\") && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
func validateVariants(variants map[string]bool) error {
	for name := range variants {
		if !token(name) || strings.HasPrefix(name, "+") || strings.HasPrefix(name, "-") {
			return fmt.Errorf("macports: invalid variant %q", name)
		}
	}
	return nil
}

var _ Reader = (*Evaluator)(nil)
