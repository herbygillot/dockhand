// Package doctor probes the machine for the tools dockhand's capabilities
// depend on, and reports which capabilities that implies. A missing tool
// is a fact about the machine, never a finding about any port — and the
// same probe runs before a batch begins, so absence surfaces at plan time
// rather than forty minutes in.
package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// provisioned is indirected for hermetic tests; the default asks the
// provisioner what bases exist.
var provisioned = func(ctx context.Context, tools *tool.Finder) ([]string, error) {
	rels, err := (provision.Tart{Tools: tools}).Provisioned(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rels))
	for _, r := range rels {
		names = append(names, r.Name)
	}
	return names, nil
}

// runVersion is indirected for hermetic tests; binary discovery goes
// through the run's tool.Finder — the SAME finder every component
// execs through, which is what keeps this report honest: doctor cannot
// say "available" about a tool the working code would fail to find,
// nor the reverse, because there is exactly one finder.
var (
	runVersion = func(path string, args ...string) string {
		out, _, err := tool.Output(context.Background(), path, tool.Opts{Args: args})
		if err != nil {
			return ""
		}
		if i := strings.IndexByte(string(out), '\n'); i >= 0 {
			out = out[:i]
		}
		return strings.TrimSpace(string(out))
	}
)

// Tool is one probe result.
type Tool struct {
	Name    string
	Path    string
	Version string
	Found   bool
	Note    string
}

// Report is the machine's capability picture.
type Report struct {
	Tools []Tool
	// VMBases are the provisioned verification bases, by release name.
	// The tart binary being present says nothing about whether any
	// environment exists; the bases are the capability.
	VMBases []string
}

// Probe examines the machine through the run's finder.
func Probe(tools *tool.Finder) Report {
	find := func(which tool.Tool, fallback string) Tool {
		t := Tool{Name: string(which)}
		path, err := tools.FindWith(which, fallback)
		if err != nil {
			return t
		}
		t.Found, t.Path = true, path
		return t
	}

	portTclsh := find(tool.PortTclsh, prefix.Prefix(macports.DefaultPrefix).PortTclsh())
	if portTclsh.Found {
		// The MacPorts version is not trivia: it selects the Tcl shims
		// dockhand speaks to this installation with.
		pfx := prefix.Prefix(filepath.Dir(filepath.Dir(portTclsh.Path)))
		if v, err := pfx.Version(context.Background()); err == nil {
			portTclsh.Version = v
			// An installation newer than any shim still works — selection
			// falls back rather than failing — but it is being driven by a
			// shim written for an older MacPorts, and the day that stops
			// working it should not be a surprise. Derived from the shims
			// themselves, so this notices without anyone remembering to
			// check.
			if newest, err := eval.NewestShim(); err == nil {
				portTclsh.Note = shimNote(v, newest)
			}
		} else {
			portTclsh.Note = "version undetermined; dockhand will use its newest shim"
		}
	}
	tclsh := find(tool.Tclsh, "")
	git := find(tool.Git, "")
	if git.Found {
		git.Version = strings.TrimPrefix(runVersion(git.Path, "--version"), "git version ")
		// The write path (D21) needs notes (ancient: full subcommand
		// set by 1.7.1) and worktree-aware plumbing — the notes lock
		// resolves --git-common-dir, introduced with worktrees in 2.5,
		// which is the binding floor. The old 2.25 floor cited
		// sparse-checkout, a relic of the abandoned worktree-based
		// design; the assessment caught the reason outliving it. A
		// dependency declaration cannot express a version floor, so the
		// probe enforces it.
		if versionBelow(git.Version, 2, 5) {
			git.Note = "below the 2.5 floor required for worktree-aware plumbing (--git-common-dir)"
		}
	}
	gh := find(tool.Gh, "")
	if gh.Found {
		gh.Version = runVersion(gh.Path, "--version")
	}
	curl := find(tool.Curl, "")
	tart := find(tool.Tart, "")
	var bases []string
	if tart.Found {
		if rels, err := provisioned(context.Background(), tools); err == nil {
			bases = rels
		}
	}
	go2port := find(tool.Go2Port, "")
	cargo2port := find(tool.Cargo2Port, "")

	return Report{Tools: []Tool{portTclsh, tclsh, git, gh, curl, tart, go2port, cargo2port},
		VMBases: bases}
}

// String renders the report: each tool, then the capabilities the
// combination implies.
func (r Report) String() string {
	var b strings.Builder
	byName := map[tool.Tool]Tool{}
	for _, t := range r.Tools {
		byName[tool.Tool(t.Name)] = t
		if t.Found {
			fmt.Fprintf(&b, "  %-12s %s", t.Name, t.Path)
			if t.Version != "" {
				fmt.Fprintf(&b, "  (%s)", t.Version)
			}
			if t.Note != "" {
				fmt.Fprintf(&b, "  ! %s", t.Note)
			}
		} else {
			fmt.Fprintf(&b, "  %-12s missing", t.Name)
		}
		b.WriteByte('\n')
	}
	b.WriteString("capabilities:\n")
	cap := func(ok bool, name, whyNot string) {
		if ok {
			fmt.Fprintf(&b, "  %-24s available\n", name)
		} else {
			fmt.Fprintf(&b, "  %-24s unavailable (%s)\n", name, whyNot)
		}
	}
	cap(byName[tool.PortTclsh].Found, "evaluation", "no port-tclsh: install MacPorts")
	cap(byName[tool.Git].Found && byName[tool.Git].Note == "", "branch workflow", "git missing or below floor")
	cap(byName[tool.Gh].Found, "GitHub integration", "no gh")
	cap(byName[tool.Curl].Found, "non-http distfile fetch", "no curl: only http(s) sources reachable")
	switch {
	case !byName[tool.Tart].Found:
		cap(false, "VM verification", "no tart")
	case len(r.VMBases) == 0:
		cap(false, "VM verification", "no base images: run `dockhand provision tart`")
	default:
		fmt.Fprintf(&b, "  %-24s available (%s)\n", "VM verification", strings.Join(r.VMBases, ", "))
	}
	cap(byName[tool.Go2Port].Found, "Go vendored blocks", "no go2port")
	cap(byName[tool.Cargo2Port].Found, "Rust vendored blocks", "no cargo2port")
	return b.String()
}

// shimNote says when an installation has outrun the shims. Selection
// falls back rather than failing, so this is not an error — but the
// installation is being driven by a shim written for an older MacPorts,
// and the day that stops working it should not come as a surprise.
//
// Derived from the shim set rather than compared against a constant, so
// it notices without anyone remembering to check.
func shimNote(installed, newestShim string) string {
	if macports.VerCmp(installed, newestShim) <= 0 {
		return ""
	}
	return fmt.Sprintf("newer than dockhand's newest shim (%s); driven by that one", newestShim)
}

// versionBelow reports whether a dotted version string is numerically
// below major.minor — a lexical compare would put 2.45 below 2.5. An
// unparseable version is not claimed to be below anything.
func versionBelow(v string, major, minor int) bool {
	var maj, min int
	if n, _ := fmt.Sscanf(v, "%d.%d", &maj, &min); n < 2 {
		return false
	}
	return maj < major || (maj == major && min < minor)
}
