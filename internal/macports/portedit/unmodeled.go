package portedit

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Reads of the OS minor version, macOS version, or deployment target have no
// modeled profile dimension, and reads of the selected compiler or its flags
// depend on the host toolchain. Either only matters where it can select or
// name a source declaration: profiles exist so every branch of version,
// distfile, and checksum declarations is observed. A read that formats a
// build-only command, or guards branches made of such commands, changes
// nothing Dockhand edits or compares across profiles, so it is classified as
// harmless instead of refused. A variable set from such a read carries it.

// benignSinks accept the read as text without affecting source declarations.
// macosx_deployment_target is among them because reassigning the deployment
// target, typically capped from a read of itself (the qt5 and qt6 Portfiles,
// TeXShop), changes only how the port compiles.
var benignSinks = map[string]bool{
	"macosx_deployment_target": true,
	// Patch selection changes what is applied after extraction, never which
	// archive is fetched or how it is checked; the patch check reads the
	// native selection.
	"patchfiles": true, "patchfiles-append": true, "patchfiles-prepend": true, "patchfiles-delete": true,
	"system": true, "reinplace": true, "xinstall": true, "copy": true, "move": true, "delete": true, "ln": true, "file": true,
	"notes": true, "notes-append": true, "ui_debug": true, "ui_info": true, "ui_msg": true, "ui_notice": true, "ui_warn": true, "ui_error": true,
	"return": true, "error": true, "puts": true, "close": true, "flush": true, "fconfigure": true,
}

// benignFamilies are option groups that only configure, build, test, or install.
var benignFamilies = []string{"configure.", "build.", "destroot.", "test.", "compiler.", "cmake.", "meson.", "depends_", "legacysupport."}

func benignSink(name string) bool {
	if benignSinks[name] {
		return true
	}
	for _, family := range benignFamilies {
		if strings.HasPrefix(name, family) {
			return true
		}
	}
	return false
}

const unmodeledDimension = "the OS minor version or deployment target, which is not modeled"
const toolchainDimension = "the selected compiler or its flags, which depend on the host toolchain"

// toolchainRead matches options whose defaults run MacPorts compiler
// selection, which probes the host for compilers. Their values shape builds only.
var toolchainRead = regexp.MustCompile(`\$(?:\{configure\.(?:compiler|cc|cxx|objc|objcxx|f77|f90|fc|cpp|cflags|cxxflags|objcflags|objcxxflags|fflags|f90flags|fcflags|ldflags|cppflags|[a-z]+_archflags|universal_[a-z]+|sdkroot)\}|configure\.(?:compiler|cc|cxx|objc|objcxx|f77|f90|fc|cpp|cflags|cxxflags|objcflags|objcxxflags|fflags|f90flags|fcflags|ldflags|cppflags|[a-z]+_archflags|universal_[a-z]+|sdkroot)\b)`)

// dimension is one class of read the scanner classifies by position: the
// pattern that spots a read, the words for a refusal, and the variables a
// set command has tainted with such a read so their later uses count too.
type dimension struct {
	read    *regexp.Regexp
	label   string
	tainted map[string]bool
}

func (d *dimension) matches(text string) bool {
	if d.read.MatchString(text) {
		return true
	}
	for name, tainted := range d.tainted {
		if tainted && regexp.MustCompile(`\$(?:\{`+regexp.QuoteMeta(name)+`\}|`+regexp.QuoteMeta(name)+`\b)`).MatchString(text) {
			return true
		}
	}
	return false
}

// unmodeledReads refuses reads of unmodeled dimensions that can reach a
// source declaration: as an argument of any command outside the benign set,
// or in the control words of a branch or loop whose bodies contain one.
func unmodeledReads(src []byte, script *syntax.Script) error {
	return classifyReads(src, script, &dimension{read: unmodeledRead, label: unmodeledDimension, tainted: map[string]bool{}})
}

// toolchainReadsBenign reports whether every read of a toolchain option, and
// of a variable set from one, sits in a build-only position. It is what lets
// a host probe from compiler selection be tolerated in a modeled evaluation.
func toolchainReadsBenign(src []byte) bool {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return false
	}
	return classifyReads(src, script, &dimension{read: toolchainRead, label: toolchainDimension, tainted: map[string]bool{}}) == nil
}

// classifyReads walks the script in source order.
func classifyReads(src []byte, script *syntax.Script, d *dimension) error {
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		if controls, bodies, ok := controlParts(src, cmd); ok {
			guarded := false
			for _, control := range controls {
				guarded = guarded || d.matches(control.Span.Text(src))
			}
			if guarded && name == "foreach" {
				// Loop variables bound from the dimension carry it into the body.
				for i := 0; i < len(controls); i += 2 {
					if names, ok := controls[i].Literal(src); ok {
						d.tainted[names] = true
					} else if len(controls[i].Segments) == 1 {
						if braced, ok := controls[i].Segments[0].(syntax.Braced); ok {
							for _, variable := range strings.Fields(braced.Body.Text(src)) {
								d.tainted[variable] = true
							}
						}
					}
				}
			}
			for _, body := range bodies {
				if guarded {
					if err := benignBody(src, body, d); err != nil {
						return err
					}
				}
				if script, ok := body.BracedScript(src); ok {
					if err := classifyReads(src, script, d); err != nil {
						return err
					}
				}
			}
			continue
		}
		if name == "set" && len(cmd.Words) == 3 {
			// A variable set from the dimension carries it; one set from
			// anything else stops carrying it.
			if variable, ok := cmd.Words[1].Literal(src); ok {
				d.tainted[variable] = d.matches(cmd.Words[2].Span.Text(src))
				continue
			}
		}
		for _, word := range cmd.Words[1:] {
			if body, ok := word.BracedScript(src); ok {
				if err := classifyReads(src, body, d); err != nil {
					return err
				}
				continue
			}
			if d.matches(word.Span.Text(src)) && !benignSink(name) {
				return fmt.Errorf("%w: %s reads %s", errProbeInconclusive, name, d.label)
			}
		}
	}
	return nil
}

// controlParts separates a control structure's conditions or iteration words
// from the bodies it executes. Other commands report ok false.
func controlParts(src []byte, cmd syntax.Command) (controls, bodies []syntax.Word, ok bool) {
	name, _ := cmd.Name(src)
	words := cmd.Words[1:]
	switch {
	case name == "if":
		expectCondition := true
		for _, word := range words {
			literal, _ := word.Literal(src)
			switch {
			case expectCondition:
				controls = append(controls, word)
				expectCondition = false
			case literal == "then" || literal == "else":
			case literal == "elseif":
				expectCondition = true
			default:
				bodies = append(bodies, word)
			}
		}
	case (name == "foreach" || name == "while") && len(words) > 1:
		controls, bodies = words[:len(words)-1], words[len(words)-1:]
	case name == "catch" && len(words) > 0:
		bodies = words[:1]
	case name == "for" && len(words) == 4:
		controls, bodies = words[1:2], []syntax.Word{words[0], words[2], words[3]}
	default:
		return nil, nil, false
	}
	return controls, bodies, true
}

// phaseHook matches hook and phase-override commands that run after the
// source is fetched and extracted; their bodies are judged like a branch.
// Fetch and extract hooks are left to the fetch-semantics check.
var phaseHook = regexp.MustCompile(`^(?:(?:pre|post)-(?:patch|configure|build|test|destroot|install|activate)|patch|configure|build|test|destroot)$`)

// benignBody accepts a guarded branch only when every command in it, at any
// depth, is a benign sink, a control structure whose bodies are benign, or a
// post-extraction hook whose body is benign. A set inside the branch taints
// its variable, so later uses are judged where they happen.
func benignBody(src []byte, body syntax.Word, d *dimension) error {
	label := d.label
	script, ok := body.BracedScript(src)
	if !ok {
		return fmt.Errorf("%w: a branch selected by %s is not a literal script", errProbeInconclusive, label)
	}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		_, nested, control := controlParts(src, cmd)
		if !control && phaseHook.MatchString(name) {
			control = true
			for _, word := range cmd.Words[1:] {
				if _, ok := word.BracedScript(src); ok {
					nested = append(nested, word)
				}
			}
		}
		if !control {
			if name == "set" && len(cmd.Words) == 3 {
				if variable, ok := cmd.Words[1].Literal(src); ok {
					d.tainted[variable] = true
					continue
				}
			}
			if benignSink(name) {
				continue
			}
			return fmt.Errorf("%w: %s is selected by %s", errProbeInconclusive, name, label)
		}
		for _, word := range nested {
			if err := benignBody(src, word, d); err != nil {
				return err
			}
		}
	}
	return nil
}

// toolchainProbesOnly reports whether every recorded host access came from
// MacPorts compiler selection. Such probes look for compilers the modeled
// profile lacks; when the Portfile reads toolchain options only in build
// positions, they cannot change any source declaration.
func toolchainProbesOnly(port macports.PortObservation) bool {
	seen := false
	for _, declaration := range port.Declarations {
		if declaration.Command != "dockhand.host-access" {
			continue
		}
		seen = true
		toolchain := false
		for _, frame := range declaration.Frames {
			if strings.Contains(frame.Command, "portconfigure::") {
				toolchain = true
				break
			}
		}
		if !toolchain {
			return false
		}
	}
	return seen
}

// tolerateToolchainProbes returns the observation with compiler-selection
// probes removed from its problems when every recorded host access is such a
// probe and the Portfile reads toolchain options only in build positions.
// The second result says whether the remaining host state, if any, still
// makes the context inconclusive.
func tolerateToolchainProbes(port macports.PortObservation, contents []byte) (macports.PortObservation, bool) {
	if !(port.HostAccess || port.ModeledHostAccess) {
		return port, false
	}
	if !toolchainProbesOnly(port) || !toolchainReadsBenign(contents) {
		return port, true
	}
	explained := map[string]bool{}
	for _, declaration := range port.Declarations {
		if declaration.Command == "dockhand.host-access" && len(declaration.Values) > 0 {
			explained[declaration.Values[0]] = true
		}
	}
	var remaining []string
	for _, problem := range port.Problems {
		if !explained[problem] {
			remaining = append(remaining, problem)
		}
	}
	port.Problems = remaining
	port.HostAccess, port.ModeledHostAccess = false, false
	return port, false
}
