// Package doctor probes the machine for the tools dockhand's capabilities
// depend on, and reports which capabilities that implies. A missing tool
// is a fact about the machine, never a finding about any port — and the
// same probe runs before a batch begins, so absence surfaces at plan time
// rather than forty minutes in.
package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/session"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify/tart"
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

// baseImage answers which IMAGE a provisioned base was built from, by
// release name. Indirected on provisioned's precedent so a doctor test
// stays hermetic.
//
// It is read and never derived: the bases come from a `:latest` tag that
// moves, so the only moment that could answer is the pull, and that
// answer was written down then. A base provisioned before dockhand
// recorded one answers "", which doctor prints as nothing rather than as
// a guess.
var baseImage = func(release string) string {
	return tart.BaseImage(tart.BaseName(platform.Release{Name: release}))
}

// restorable is indirected for hermetic tests, on provisioned's
// precedent; the default asks the provisioner which goldens exist.
var restorable = func(ctx context.Context, tools *tool.Finder) ([]string, error) {
	rels, err := (provision.Tart{Tools: tools}).Restorable(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rels))
	for _, r := range rels {
		names = append(names, r.Name)
	}
	return names, nil
}

// without is the goldens that have no base of their own: a release this
// machine could verify on after one clone, and is not verifying on now.
func without(goldens, bases []string) []string {
	has := make(map[string]bool, len(bases))
	for _, b := range bases {
		has[b] = true
	}
	out := make([]string, 0, len(goldens))
	for _, g := range goldens {
		if !has[g] {
			out = append(out, g)
		}
	}
	return out
}

// runVersion is indirected for hermetic tests; binary discovery goes
// through the run's tool.Finder — the SAME finder every component
// execs through, which is what keeps this report honest: doctor cannot
// say "available" about a tool the working code would fail to find,
// nor the reverse, because there is exactly one finder.
//
// The context is the run's, so a probe dies with the run: asking a
// binary its version is the cheapest question there is, but it is
// still an exec, and one that never answers must not outlive the
// interrupt that was meant to stop it.
//
// A version that could not be read is empty rather than an error. The
// tool was found; what it calls itself is decoration on that fact, and
// the one place a version is load-bearing — git's floor — declines to
// claim anything about a version it cannot parse.
var (
	runVersion = func(ctx context.Context, path string, args ...string) string {
		res, err := tool.Output(ctx, path, tool.Opts{Args: args})
		if err != nil {
			return ""
		}
		out := res.Stdout
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
	// VMImages is the exact image each provisioned base was built from,
	// by release name, for the bases that recorded one. A release absent
	// from the map is a base that predates the recording, which doctor
	// says by saying nothing.
	VMImages map[string]string
	// VMGoldens are the releases whose GOLDEN copy is present: the ones
	// a base can be cloned back from without leaving the machine.
	//
	// It is reported beside the bases because the two make a diagnosis
	// that neither makes alone. A host with no base and no golden has to
	// fetch and provision; a host with no base and a golden is one
	// `provision tart --macos <release> --restore` away — seconds,
	// copy-on-write — and this line is the difference between a person
	// knowing that and rebuilding from scratch.
	VMGoldens []string
	// TreeRoot is the ports tree this report was taken in, or empty for a
	// machine that is not standing in one.
	//
	// DOCTOR IS ABOUT THE MACHINE and this is the one tree fact it
	// carries, because leaving it out made the rest of the report read as
	// more than it was: "branch workflow available", "VM verification
	// available", and a bump that then refused because the tree carried
	// no PortIndex. Every tool was present and every capability was
	// genuinely there; what was missing was a generated file that no
	// amount of installing fixes. A person reading a page of "available"
	// had no way to see it.
	//
	// It says WHERE it looked and never invents a tree: outside one, this
	// is empty and the report says so, which is the answer rather than
	// the absence of one.
	TreeRoot string
	// TreeIndex is whether that tree carries a readable ports index — the
	// prerequisite of every road that settles a build, since a passing
	// attempt surveys its dependents. Meaningless when TreeRoot is empty.
	TreeIndex bool
}

// Probe examines the machine through the run's finder.
//
// The context is the caller's and bounds every probe that execs. Only
// three of them do: the MacPorts version, the version strings git and
// gh state, and the base-image listing tart answers — finding a binary
// is a PATH stat and cannot block. Those that do exec are one-shot
// questions that should answer in milliseconds, but a binary on a
// wedged network mount or a port client waiting on something answers
// never, and a report is exactly what someone asks for when the
// machine is already misbehaving. With the context threaded, an
// interrupt reaches the probe rather than being noticed after it.
func Probe(ctx context.Context, tools *tool.Finder, treeRoot string) Report {
	find := func(which tool.Tool, fallback string) Tool {
		t := Tool{Name: string(which)}
		path, err := tools.FindWith(which, fallback)
		if err != nil {
			return t
		}
		t.Found, t.Path = true, path
		return t
	}

	// The one tree fact, and it is a stat: whether the ports index a
	// settling road will survey with is there. Probe never opens a tree
	// and never fails on one — outside a tree this stays empty and the
	// report says the question went unasked.
	treeIndex := false
	if treeRoot != "" {
		if _, err := os.Stat(filepath.Join(treeRoot, macports.IndexFile)); err == nil {
			treeIndex = true
		}
	}

	portTclsh := find(tool.PortTclsh, prefix.Prefix(macports.DefaultPrefix).PortTclsh())
	if portTclsh.Found {
		// The MacPorts version is not trivia: it selects the Tcl shims
		// dockhand speaks to this installation with.
		pfx := prefix.Prefix(filepath.Dir(filepath.Dir(portTclsh.Path)))
		if v, err := pfx.Version(ctx); err == nil {
			portTclsh.Version = v
			// An installation newer than any shim still works — selection
			// falls back rather than failing — but it is being driven by a
			// shim written for an older MacPorts, and the day that stops
			// working it should not be a surprise. Derived from the shims
			// themselves, so this notices without anyone remembering to
			// check.
			if newest, err := session.NewestShim(); err == nil {
				portTclsh.Note = shimNote(v, newest)
			}
		} else {
			portTclsh.Note = "version undetermined; dockhand will use its newest shim"
		}
	}
	tclsh := find(tool.Tclsh, "")
	git := find(tool.Git, "")
	if git.Found {
		git.Version = strings.TrimPrefix(runVersion(ctx, git.Path, "--version"), "git version ")
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
		gh.Version = runVersion(ctx, gh.Path, "--version")
	}
	curl := find(tool.Curl, "")
	tart := find(tool.Tart, "")
	var bases, goldens []string
	if tart.Found {
		if rels, err := provisioned(ctx, tools); err == nil {
			bases = rels
		}
		if rels, err := restorable(ctx, tools); err == nil {
			goldens = rels
		}
	}
	go2port := find(tool.Go2Port, "")
	cargo2port := find(tool.Cargo2Port, "")

	images := map[string]string{}
	for _, b := range bases {
		if img := baseImage(b); img != "" {
			images[b] = img
		}
	}
	return Report{Tools: []Tool{portTclsh, tclsh, git, gh, curl, tart, go2port, cargo2port},
		VMBases: bases, VMGoldens: goldens, VMImages: images,
		TreeRoot: treeRoot, TreeIndex: treeIndex}
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
	case r.TreeRoot == "":
		// NOT AN "unavailable", because nothing is wrong with the machine:
		// doctor was simply not run in a tree, and every line above still
		// stands. Said rather than omitted, so a reader knows which
		// question went unasked.
		fmt.Fprintf(&b, "  %-24s not checked (not standing in a ports tree)\n", "dependent survey")
	case r.TreeIndex:
		fmt.Fprintf(&b, "  %-24s available (%s)\n", "dependent survey", r.TreeRoot)
	default:
		fmt.Fprintf(&b, "  %-24s unavailable (no PortIndex in %s: run `portindex %s`)\n",
			"dependent survey", r.TreeRoot, r.TreeRoot)
	}
	switch {
	case !byName[tool.Tart].Found:
		cap(false, "VM verification", "no tart")
	case len(r.VMBases) == 0 && len(r.VMGoldens) > 0:
		// The cheap remedy, named because it is available. A golden is a
		// base's reference copy and restoring from it is a clone: seconds,
		// no download. Reporting only "no base images" here sent a person
		// to rebuild from scratch beside a copy that would have taken
		// seconds.
		cap(false, "VM verification", "no base images, but goldens for "+strings.Join(r.VMGoldens, ", ")+
			": `dockhand provision tart --macos <release> --restore` clones one back")
	case len(r.VMBases) == 0:
		cap(false, "VM verification", "no base images and no goldens: run `dockhand provision tart --macos <release>`")
	default:
		line := strings.Join(r.VMBases, ", ")
		if kept := without(r.VMGoldens, r.VMBases); len(kept) > 0 {
			// A golden with no base of its own is a release this machine can
			// restore but is not currently verifying on, which is worth
			// saying: it is a capability one command away.
			line += "; restorable: " + strings.Join(kept, ", ")
		}
		fmt.Fprintf(&b, "  %-24s available (%s)\n", "VM verification", line)
		// The image under the release, because "Tahoe" names a macOS and
		// not a build of it, and a verdict is only as reproducible as the
		// base it was earned on.
		for _, rel := range r.VMBases {
			if img := r.VMImages[rel]; img != "" {
				fmt.Fprintf(&b, "  %-24s   %s: %s\n", "", rel, img)
			}
		}
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
