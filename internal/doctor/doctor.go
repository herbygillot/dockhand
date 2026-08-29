// Package doctor probes the machine for the tools dockhand's capabilities
// depend on, and reports which capabilities that implies. A missing tool
// is a fact about the machine, never a finding about any port — and the
// same probe runs before a batch begins, so absence surfaces at plan time
// rather than forty minutes in.
package doctor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
)

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

	portTclsh := find("port-tclsh", prefix.Prefix(macports.DefaultPrefix).PortTclsh())
	tclsh := find("tclsh", "")
	git := find("git", "")
	if git.Found {
		git.Version = strings.TrimPrefix(runVersion(git.Path, "--version"), "git version ")
		// git worktree needs 2.5+; a dependency declaration cannot express
		// a version floor, so the probe enforces it.
		if versionBelow(git.Version, 2, 5) {
			git.Note = "below the 2.5 floor required for git worktree"
		}
	}
	gh := find("gh", "")
	if gh.Found {
		gh.Version = runVersion(gh.Path, "--version")
	}
	tart := find("tart", "")
	go2port := find("go2port", "")
	cargo2port := find("cargo2port", "")

	return Report{Tools: []Tool{portTclsh, tclsh, git, gh, tart, go2port, cargo2port}}
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
	cap(byName["port-tclsh"].Found, "evaluation", "no port-tclsh: install MacPorts")
	cap(byName["git"].Found && byName["git"].Note == "", "branches and worktrees", "git missing or below floor")
	cap(byName["gh"].Found, "GitHub integration", "no gh")
	cap(byName["tart"].Found, "VM verification", "no tart")
	cap(byName["go2port"].Found, "Go vendored blocks", "no go2port")
	cap(byName["cargo2port"].Found, "Rust vendored blocks", "no cargo2port")
	return b.String()
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
