package command

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
)

// standIns are each test's stand-ins for what compose makes and for the
// host, by the test's name, where package variables each held them for
// every test at once.
var standIns sync.Map

// standing is one test's stand-ins.
type standing struct {
	deps compose.Dependencies
	host *host
}

// standingOf is the test's stand-ins, made on first use and dropped with
// the test.
func standingOf(t testing.TB) *standing {
	t.Helper()
	if found, ok := standIns.Load(t.Name()); ok {
		return found.(*standing)
	}
	made := &standing{deps: compose.Dependencies{GitHubCLI: signedIn{}}, host: testHost()}
	actual, loaded := standIns.LoadOrStore(t.Name(), made)
	if !loaded {
		t.Cleanup(func() { standIns.Delete(t.Name()) })
	}
	return actual.(*standing)
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
func runFor(t testing.TB, ctx context.Context, args []string, streams Streams) error {
	t.Helper()
	found := standsFor(t)
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return run(ctx, invocation{args: args, env: os.Environ(), dir: dir}, streams, found.deps, found.host)
}

// testSettings are a command's settings, with the test's stand-ins.
func testSettings(t testing.TB) *settings {
	t.Helper()
	found := standsFor(t)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return &settings{deps: found.deps, host: found.host, env: os.Environ(), dir: dir}
}

// testPlaces are the test's home and working directory, as a command run
// in it reads them.
func testPlaces(t testing.TB) config.Places {
	t.Helper()
	return testSettings(t).places()
}
