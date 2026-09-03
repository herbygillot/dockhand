package porttest

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// The live half of the package: the real evaluator, the real fetcher,
// and the handle over either. Four test packages had written these for
// themselves — in two argument orders, with the prefix derivation
// inlined in one of them — which is four chances to skip where the
// others require, and the reason a machine with a broken MacPorts could
// look green in one package and red in another. The four intent
// packages call these now.
//
// The prefix itself is testenv's, not this package's. eval, port,
// portfetch and session all want it and none can import a helper that
// lives under macports/port without a cycle, so it sits beside the tool
// discovery it is derived from and every package reaches the same one.
//
// They are gated, not scripted: what they exercise is what MacPorts
// actually evaluates, which is exactly what the scripted Oracle above
// must never be asked to stand in for.

// Evaluator starts a real evaluator against the discovered
// installation and closes it when the test ends.
func Evaluator(t *testing.T) *eval.Evaluator {
	t.Helper()
	ev, err := eval.Start(context.Background(), testenv.MacPortsPrefix(t))
	if err != nil {
		t.Fatalf("starting the evaluator: %v", err)
	}
	t.Cleanup(func() { _ = ev.Close() })
	return ev
}

// LiveHandle is Handle over an evaluator started for this test: the
// shorthand for a test that wants one context and does not need the
// evaluator by name.
func LiveHandle(t *testing.T, portdir string) port.Handle {
	t.Helper()
	return Handle(Evaluator(t), portdir)
}

// Fetcher starts a real portfetch session, staging under the system
// temporary directory, and closes it when the test ends.
func Fetcher(t *testing.T) *portfetch.Fetcher {
	t.Helper()
	f, err := portfetch.New(context.Background(), testenv.MacPortsPrefix(t), tempdir.Root{})
	if err != nil {
		t.Fatalf("starting the fetcher: %v", err)
	}
	t.Cleanup(f.Close)
	return f
}
