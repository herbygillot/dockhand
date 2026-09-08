// dockhand is a command-line tool for MacPorts maintainers.
// From upstream release to submitted port.
package main

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/herbygillot/dockhand/internal/cli"
)

// Version is a var, not a const: the linker's -X can only overwrite a
// variable, and it fails silently on anything else.
//
// The placeholder is a PLACEHOLDER and not a version. Every pull request
// dockhand opens signs off with this string, so that "a published
// sentence found to be wrong can be traced to the build that wrote it"
// (publish.Env.Version) — and a body reading "opened by dockhand
// 0.0.0-dev" traces to nothing at all. That is what the field measured:
// `make build` stamped the Makefile's own default, and every pull
// request the exercise opened carried it upstream.
var Version = placeholder

// placeholder is the version of a build nobody named. It is written down
// once so that the one rule about it — it never reaches a pull request
// body when the toolchain knows the commit — is checkable.
const placeholder = "0.0.0-dev"

func main() {
	os.Exit(cli.Execute(stamped(Version)))
}

// stamped is the version with the go toolchain's own VCS answer folded
// in when the linker gave none.
//
// The Makefile derives a version from `git describe`, so a `make build`
// binary arrives here already named. A `go build ./cmd/dockhand` — which
// is what a contributor types, and what `go install` does — passes no
// -ldflags at all, and the placeholder would go out on a pull request.
// Go stamps vcs.revision and vcs.modified into the build info of any
// binary built inside a repository, so the answer is already in the
// binary and nothing was reading it.
//
// It APPENDS rather than replaces when a real version was linked in: a
// tagged build that is one dirty commit ahead of its tag is both facts,
// and a reader tracing a wrong sentence needs the commit more than the
// tag.
func stamped(v string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return fold(v, rev, dirty)
}

// fold is stamped's whole decision, separated from the reading so it can
// be tested: a `go test` binary carries no vcs stamp of its own, so a
// test that called stamped would skip on the one machine that matters.
func fold(v, rev string, dirty bool) string {
	if rev == "" {
		return v
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	// A linked version that already names this commit is not repeated.
	if strings.Contains(v, rev) {
		return v
	}
	if v == "" || v == placeholder {
		return rev
	}
	return v + " (" + rev + ")"
}
