package command

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/compose"
)

// standIns are each test's stand-ins for what compose makes, by the test's
// name, where a package variable each held them for every test at once.
var standIns sync.Map

// stands are the test's stand-ins, made on first use and dropped with the
// test: no Tart is found, and the GitHub CLI isn't installed, so a test
// doesn't depend on the Mac running it.
func stands(t testing.TB) *compose.Dependencies {
	t.Helper()
	if found, ok := standIns.Load(t.Name()); ok {
		return found.(*compose.Dependencies)
	}
	made := &compose.Dependencies{LookTart: func(string) (string, error) { return "", exec.ErrNotFound }, GitHubCLI: signedIn{}}
	actual, loaded := standIns.LoadOrStore(t.Name(), made)
	if !loaded {
		t.Cleanup(func() { standIns.Delete(t.Name()) })
	}
	return actual.(*compose.Dependencies)
}

// standsFor are the stand-ins a command run in the test is given: its
// own, else those of the test it's a subtest of.
func standsFor(t testing.TB) compose.Dependencies {
	t.Helper()
	for name := t.Name(); ; {
		if found, ok := standIns.Load(name); ok {
			return *found.(*compose.Dependencies)
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return *stands(t)
		}
		name = name[:i]
	}
}

// runFor runs a command line in the test, with its stand-ins.
func runFor(t testing.TB, ctx context.Context, args []string, streams Streams) error {
	t.Helper()
	return run(ctx, args, streams, standsFor(t))
}
