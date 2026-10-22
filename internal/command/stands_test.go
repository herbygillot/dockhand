package command

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
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
	deps compose.Dependencies
	host *host
	env  map[string]string
	dir  string
}

// standingOf is the test's stand-ins, made on first use and dropped with
// the test: a subtest's start as a copy of its parent's.
func standingOf(t testing.TB) *standing {
	t.Helper()
	if found, ok := standIns.Load(t.Name()); ok {
		return found.(*standing)
	}
	made := &standing{deps: compose.Dependencies{GitHubCLI: signedIn{}}, host: testHost(), env: map[string]string{}}
	if parent, ok := parentStanding(t.Name()); ok {
		host := *parent.host
		made = &standing{deps: parent.deps, host: &host, env: maps.Clone(parent.env), dir: parent.dir}
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

// setenv sets a variable in the environment the test's command lines run
// with, as t.Setenv would in the process's, which a parallel test can't.
func setenv(t testing.TB, name, value string) {
	t.Helper()
	standingOf(t).env[name] = value
}

// getenv is a variable as the test's command lines see it.
func getenv(t testing.TB, name string) string {
	t.Helper()
	found := standsFor(t)
	if value, ok := found.env[name]; ok {
		return value
	}
	return os.Getenv(name)
}

// chdir is the directory the test's command lines run in, as t.Chdir
// would make the process's.
func chdir(t testing.TB, dir string) {
	t.Helper()
	standingOf(t).dir = dir
}

// invocationFor is a command line in the test: the process's environment
// with the test's own over it, and the test's directory, else the
// process's.
func invocationFor(t testing.TB, args []string) invocation {
	t.Helper()
	found := standsFor(t)
	env := os.Environ()
	for name, value := range found.env {
		env = append(env, name+"="+value)
	}
	dir := found.dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			t.Fatal(err)
		}
	}
	return invocation{args: args, env: env, dir: dir}
}

// stands are the test's stand-ins for what compose makes: the GitHub CLI
// isn't installed, so a test doesn't depend on the Mac running it.
func stands(t testing.TB) *compose.Dependencies {
	t.Helper()
	return &standingOf(t).deps
}

// hosted is the test's host (testHost), which it changes as it needs.
func hosted(t testing.TB) *host {
	t.Helper()
	return standingOf(t).host
}

// testHost is a host that keeps a test out of the Mac running it: it finds
// no Tart, though every test's Mac takes it, but where it says it's an
// Intel Mac's (onIntel); its login is no one's, in its keeping or read
// afresh by serve's identity check, unless the test gives it one
// (withAuth); and it starts no cleanup, the executable being the test
// binary, which isn't dockhand.
func testHost() *host {
	h := machineHost()
	h.lookTart = func(string) (string, error) { return "", exec.ErrNotFound }
	h.tartSupported = func() bool { return true }
	h.identity = func(*github.Runtime) func(context.Context) (string, error) { return nil }
	h.authStore = memoryStore{}
	h.authAPI = func(github.Signin) *github.Client {
		return &github.Client{Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) { return github.Token{}, github.ErrNoCredentials })}
	}
	h.startCleanup = func(engine.Options) error { return nil }
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

// runFor runs a command line in the test, with its stand-ins.
func runFor(t testing.TB, ctx context.Context, args []string, streams term.Streams) error {
	t.Helper()
	found := standsFor(t)
	return run(ctx, invocationFor(t, args), streams, found.deps, found.host)
}

// testSettings are a command's settings, with the test's stand-ins.
func testSettings(t testing.TB) *settings {
	t.Helper()
	found := standsFor(t)
	line := invocationFor(t, nil)
	return &settings{deps: found.deps, host: found.host, env: line.env, dir: line.dir}
}

// testPlaces are the test's home and working directory, as a command run
// in it reads them.
func testPlaces(t testing.TB) config.Places {
	t.Helper()
	return testSettings(t).places()
}
