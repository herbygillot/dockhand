// Package doctor probes the machine for the tools dockhand's capabilities
// depend on, and reports which capabilities that implies. A missing tool
// is a fact about the machine, never a finding about any port — and the
// same probe runs before a batch begins, so absence surfaces at plan time
// rather than forty minutes in.
package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// provisioned is indirected for hermetic tests; the default asks the
// provisioner what bases exist.
var provisioned = func(ctx context.Context) ([]string, error) {
	rels, err := (provision.Tart{}).Provisioned(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rels))
	for _, r := range rels {
		names = append(names, r.Name)
	}
	return names, nil
}

// lookPath and runVersion are indirected for hermetic tests.
var (
	lookPath   = exec.LookPath
	runVersion = func(path string, args ...string) string {
		out, err := exec.Command(path, args...).Output()
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

// Probe examines the machine.
func Probe() Report {
	find := func(name string, fallback string) Tool {
		t := Tool{Name: name}
		path, err := lookPath(name)
		if err != nil && fallback != "" {
			if _, ferr := lookPath(fallback); ferr == nil {
				path, err = fallback, nil
			}
		}
		if err != nil {
			return t
		}
		t.Found, t.Path = true, path
		return t
	}

	portTclsh := find(macports.TclShellName, prefix.Prefix(macports.DefaultPrefix).PortTclsh())
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
	tclsh := find("tclsh", "")
	git := find("git", "")
	if git.Found {
		git.Version = strings.TrimPrefix(runVersion(git.Path, "--version"), "git version ")
		// The write path (D21) needs three porcelains, each with its
		// introducing release: notes is ancient (1.6.6; full subcommand
		// set 1.7.1, merge strategies 1.7.4), worktree needs 2.5, and
		// sparse-checkout needs 2.25 — the binding floor, subsuming the
		// others. A dependency declaration cannot express a version
		// floor, so the probe enforces it.
		if versionBelow(git.Version, 2, 25) {
			git.Note = "below the 2.25 floor required for git sparse-checkout"
		}
	}
	gh := find("gh", "")
	if gh.Found {
		gh.Version = runVersion(gh.Path, "--version")
	}
	curl := find("curl", "")
	tart := find("tart", "")
	var bases []string
	if tart.Found {
		if rels, err := provisioned(context.Background()); err == nil {
			bases = rels
		}
	}
	go2port := find("go2port", "")
	cargo2port := find("cargo2port", "")

	return Report{Tools: []Tool{portTclsh, tclsh, git, gh, curl, tart, go2port, cargo2port},
		VMBases: bases}
}

// String renders the report: each tool, then the capabilities the
// combination implies.
func (r Report) String() string {
	var b strings.Builder
	byName := map[string]Tool{}
	for _, t := range r.Tools {
		byName[t.Name] = t
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
	cap(byName[macports.TclShellName].Found, "evaluation", "no port-tclsh: install MacPorts")
	cap(byName["git"].Found && byName["git"].Note == "", "branch workflow", "git missing or below floor")
	cap(byName["gh"].Found, "GitHub integration", "no gh")
	cap(byName["curl"].Found, "non-http distfile fetch", "no curl: only http(s) sources reachable")
	switch {
	case !byName["tart"].Found:
		cap(false, "VM verification", "no tart")
	case len(r.VMBases) == 0:
		cap(false, "VM verification", "no base images: run `dockhand provision tart`")
	default:
		fmt.Fprintf(&b, "  %-24s available (%s)\n", "VM verification", strings.Join(r.VMBases, ", "))
	}
	cap(byName["go2port"].Found, "Go vendored blocks", "no go2port")
	cap(byName["cargo2port"].Found, "Rust vendored blocks", "no cargo2port")
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
