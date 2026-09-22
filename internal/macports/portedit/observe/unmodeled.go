package observe

import (
	"fmt"
	"regexp"
	"strings"

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
var toolchainName = regexp.MustCompile(`^configure\.(?:compiler|cc|cxx|objc|objcxx|f77|f90|fc|cpp|cflags|cxxflags|objcflags|objcxxflags|fflags|f90flags|fcflags|ldflags|cppflags|[a-z]+_archflags|universal_[a-z]+|sdkroot)$`)

func toolchainRead(name string) bool { return toolchainName.MatchString(name) }

// unmodeledRead names the OS minor version and deployment target, which no
// modeled context sets.
func unmodeledRead(name string) bool {
	switch name {
	case "os.version", "macosx_version", "macos_version", "macosx_deployment_target":
		return true
	}
	return false
}

// dimension is one class of read the scanner classifies by position: the
// pattern that spots a read, the words for a refusal, and the variables a
// set command has tainted with such a read so their later uses count too.
type dimension struct {
	read    func(name string) bool
	label   string
	tainted map[string]bool
}

// any reports whether one of the reads is of the dimension, or of a
// variable a set has tainted with it.
func (d *dimension) any(src []byte, reads []syntax.VarSub) bool {
	for _, read := range reads {
		name := read.Name.Text(src)
		if d.read(name) || d.tainted[name] {
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
		if controls, bodies, ok := cmd.Control(src); ok {
			guarded := d.any(src, cmd.Reads(src))
			if guarded && name == "foreach" {
				// Loop variables bound from the dimension carry it into the body.
				for i := 0; i < len(controls); i += 2 {
					if names, ok := controls[i].Literal(src); ok {
						d.tainted[names] = true
					} else if len(controls[i].Segments) == 1 {
						if braced, ok := controls[i].Segments[0].(syntax.Braced); ok {
							if names, errs := syntax.ListValues(braced.Body.Text(src)); len(errs) == 0 {
								for _, variable := range names {
									d.tainted[variable] = true
								}
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
				d.tainted[variable] = d.any(src, cmd.Words[2].Variables(src))
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
			if d.any(src, word.Variables(src)) && !benignSink(name) {
				return fmt.Errorf("%w: %s reads %s", ErrInconclusive, name, d.label)
			}
		}
	}
	return nil
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
		return fmt.Errorf("%w: a branch selected by %s is not a literal script", ErrInconclusive, label)
	}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		_, nested, control := cmd.Control(src)
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
			return fmt.Errorf("%w: %s is selected by %s", ErrInconclusive, name, label)
		}
		for _, word := range nested {
			if err := benignBody(src, word, d); err != nil {
				return err
			}
		}
	}
	return nil
}
