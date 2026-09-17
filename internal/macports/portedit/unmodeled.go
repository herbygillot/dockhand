package portedit

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Reads of the OS minor version, macOS version, or deployment target have no
// modeled profile dimension. They only matter where they can select or name a
// source declaration: profiles exist so every branch of version, distfile, and
// checksum declarations is observed. A read that formats a build-only command,
// or guards branches made of such commands, changes nothing Dockhand edits or
// compares across profiles, so it is classified as harmless instead of refused.

// benignSinks accept the read as text without affecting source declarations.
var benignSinks = map[string]bool{
	"system": true, "reinplace": true, "xinstall": true, "copy": true, "move": true, "delete": true, "ln": true, "file": true,
	"notes": true, "notes-append": true, "ui_debug": true, "ui_info": true, "ui_msg": true, "ui_notice": true, "ui_warn": true, "ui_error": true,
	"return": true, "error": true, "puts": true,
}

// benignFamilies are option groups that only configure, build, test, or install.
var benignFamilies = []string{"configure.", "build.", "destroot.", "test.", "compiler.", "cmake.", "meson.", "depends_"}

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

// unmodeledReads refuses reads of unmodeled dimensions that can reach a
// source declaration: as an argument of any command outside the benign set,
// or in the control words of a branch or loop whose bodies contain one.
func unmodeledReads(src []byte, script *syntax.Script) error {
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		if controls, bodies, ok := controlParts(src, cmd); ok {
			guarded := false
			for _, control := range controls {
				guarded = guarded || unmodeledRead.MatchString(control.Span.Text(src))
			}
			for _, body := range bodies {
				if guarded {
					if err := benignBody(src, body); err != nil {
						return err
					}
				}
				if script, ok := body.BracedScript(src); ok {
					if err := unmodeledReads(src, script); err != nil {
						return err
					}
				}
			}
			continue
		}
		for _, word := range cmd.Words[1:] {
			if body, ok := word.BracedScript(src); ok {
				if err := unmodeledReads(src, body); err != nil {
					return err
				}
				continue
			}
			if unmodeledRead.MatchString(word.Span.Text(src)) && !benignSink(name) {
				return fmt.Errorf("%w: %s reads %s", errProbeInconclusive, name, unmodeledDimension)
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

// benignBody accepts a guarded branch only when every command in it, at any
// depth, is a benign sink or a control structure whose bodies are benign.
func benignBody(src []byte, body syntax.Word) error {
	script, ok := body.BracedScript(src)
	if !ok {
		return fmt.Errorf("%w: a branch selected by %s is not a literal script", errProbeInconclusive, unmodeledDimension)
	}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		_, nested, control := controlParts(src, cmd)
		if !control {
			if benignSink(name) {
				continue
			}
			return fmt.Errorf("%w: %s is selected by %s", errProbeInconclusive, name, unmodeledDimension)
		}
		for _, word := range nested {
			if err := benignBody(src, word); err != nil {
				return err
			}
		}
	}
	return nil
}
