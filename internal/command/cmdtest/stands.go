package cmdtest

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// standIns are each test's stand-ins for what compose makes and for the
// host, by the test's name, where package variables each held them for
// every test at once.
var standIns sync.Map

// standing is one test's stand-ins: compose's, the host, and the
// environment and directory a command line in it runs with, where it
// sets them (setenv, chdir), the process's otherwise.
type standing struct {
	Deps compose.Dependencies
	Host *host.Host
	Env  map[string]string
	Dir  string
}

// standingOf is the test's stand-ins, made on first use and dropped with
// the test: a subtest's start as a copy of its parent's.
func standingOf(t testing.TB) *standing {
	t.Helper()
	if found, ok := standIns.Load(t.Name()); ok {
		return found.(*standing)
	}
	made := &standing{Deps: compose.Dependencies{GitHubCLI: SignedIn{}}, Host: testHost(), Env: map[string]string{}}
	if parent, ok := parentStanding(t.Name()); ok {
		mac := *parent.Host
		made = &standing{Deps: parent.Deps, Host: &mac, Env: maps.Clone(parent.Env), Dir: parent.Dir}
	}
	actual, loaded := standIns.LoadOrStore(t.Name(), made)
	if !loaded {
		t.Cleanup(func() { standIns.Delete(t.Name()) })
	}
	return actual.(*standing)
}

// parentStanding is the stand-ins of the nearest test name is a subtest
// of.
func parentStanding(name string) (*standing, bool) {
	for {
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return nil, false
		}
		name = name[:i]
		if found, ok := standIns.Load(name); ok {
			return found.(*standing), true
		}
	}
}

// Setenv sets a variable in the environment the test's command lines run
// with, as t.Setenv would in the process's, which a parallel test can't.
func Setenv(t testing.TB, name, value string) {
	t.Helper()
	standingOf(t).Env[name] = value
}

// Getenv is a variable as the test's command lines see it.
func Getenv(t testing.TB, name string) string {
	t.Helper()
	found := standsFor(t)
	if value, ok := found.Env[name]; ok {
		return value
	}
	return os.Getenv(name)
}

// Chdir is the directory the test's command lines run in, as t.Chdir
// would make the process's.
func Chdir(t testing.TB, dir string) {
	t.Helper()
	standingOf(t).Dir = dir
}

// invocationFor is a command line in the test: the process's environment
// with the test's own over it, and the test's directory, else the
// process's.
func invocationFor(t testing.TB, args []string) settings.Invocation {
	t.Helper()
	found := standsFor(t)
	env := os.Environ()
	for name, value := range found.Env {
		env = append(env, name+"="+value)
	}
	dir := found.Dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			t.Fatal(err)
		}
	}
	return settings.Invocation{Args: args, Env: env, Dir: dir}
}

// Stands are the test's stand-ins for what compose makes: the GitHub CLI
// isn't installed, so a test doesn't depend on the Mac running it.
func Stands(t testing.TB) *compose.Dependencies {
	t.Helper()
	return &standingOf(t).Deps
}

// Hosted is the test's host (testHost), which it changes as it needs.
func Hosted(t testing.TB) *host.Host {
	t.Helper()
	return standingOf(t).Host
}

// testHost is a host that keeps a test out of the Mac running it: it finds
// no Tart, though every test's Mac takes it, but where it says it's an
// Intel Mac's (onIntel); its login is no one's, in its keeping or read
// afresh by serve's identity check, unless the test gives it one
// (withAuth); and it starts no cleanup, the executable being the test
// binary, which isn't dockhand.
func testHost() *host.Host {
	h := host.MachineHost()
	h.LookTart = func(string) (string, error) { return "", exec.ErrNotFound }
	h.TartSupported = func() bool { return true }
	h.Identity = func(*github.Runtime) func(context.Context) (string, error) { return nil }
	h.AuthStore = memoryStore{}
	h.AuthAPI = func(github.Signin) *github.Client {
		return &github.Client{Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) { return github.Token{}, github.ErrNoCredentials })}
	}
	h.StartCleanup = func(engine.Options) error { return nil }
	h.StartDrain = func(engine.Options) (int, error) { return 0, errors.New("no drain starts in a test") }
	return h
}

// standsFor are the stand-ins a command run in the test is given: its
// own, else those of the test it's a subtest of.
func standsFor(t testing.TB) *standing {
	t.Helper()
	for name := t.Name(); ; {
		if found, ok := standIns.Load(name); ok {
			return found.(*standing)
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return standingOf(t)
		}
		name = name[:i]
	}
}

// RunFor runs a command line in the test, with its stand-ins.
func RunFor(t testing.TB, ctx context.Context, args []string, streams term.Streams) error {
	t.Helper()
	found := standsFor(t)
	return runner(ctx, invocationFor(t, args), streams, found.Deps, found.Host)
}

// TestSettings are a command's settings, with the test's stand-ins.
func TestSettings(t testing.TB) *settings.Settings {
	t.Helper()
	found := standsFor(t)
	line := invocationFor(t, nil)
	return &settings.Settings{Deps: found.Deps, Host: found.Host, Env: line.Env, Dir: line.Dir}
}

// TestPlaces are the test's home and working directory, as a command run
// in it reads them.
func TestPlaces(t testing.TB) config.Places {
	t.Helper()
	return TestSettings(t).Places()
}

// UsePorts gives a command run in the test reader as its port reader,
// with each capability the reader has, leaving any other given apart, as
// engine's usePorts does.
func UsePorts(d *compose.Dependencies, reader engine.PortReader) {
	d.Ports.Reader = reader
	if families, ok := reader.(engine.FamilyReader); ok {
		d.Ports.Families = families
	}
	if graphs, ok := reader.(engine.DependencyGraphReader); ok {
		d.Ports.Graphs = graphs
	}
	if planner, ok := reader.(engine.ArchivePlanner); ok {
		d.Ports.Archives = planner
	}
	if names, ok := reader.(engine.PortNamer); ok {
		d.Ports.Names = names
	}
	if dependents, ok := reader.(engine.DependentReader); ok {
		d.Ports.Dependents = dependents
	}
	if maintainers, ok := reader.(engine.MaintainerReader); ok {
		d.Ports.Maintainers = maintainers
	}
}

// UseDependents gives a command run in the test reader as what finds
// dependents, and as what names a directory's ports, which names none
// where the reader doesn't.
func UseDependents(d *compose.Dependencies, reader engine.DependentReader) {
	d.Ports.Dependents = reader
	d.Ports.Names = noNames{}
	if names, ok := reader.(engine.PortNamer); ok {
		d.Ports.Names = names
	}
}

// noNames names no directory's ports.
type noNames struct{}

func (noNames) PortsDefined(context.Context, model.Source, []string) (map[string][]string, error) {
	return nil, nil
}

// jqDependents stands in for the port index: two ports depend on jq.
// jqDependents are jq's dependents, as the index has them; where under,
// jo links it under its +jq variant too, as variantDependents finds.
type JqDependents struct{ Under bool }

func (d JqDependents) Dependents(context.Context, model.Source, []string) ([]treequery.Dependent, error) {
	jo := treequery.Dependent{Name: "jo", Directory: "textproc/jo", On: []string{"jq"}, Phases: []string{"build"}}
	if d.Under {
		jo.Phases, jo.Variants = []string{"library"}, []string{"jq"}
	}
	return []treequery.Dependent{jo, {Name: "yq", Directory: "textproc/yq", On: []string{"jq"}, Phases: []string{"library", "runtime"}}}, nil
}
