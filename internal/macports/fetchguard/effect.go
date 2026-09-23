package fetchguard

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// A definition is what the worker says a command a hook calls is: an
// option command, judged by its option; a procedure, judged by its body;
// or a built-in command, judged by name. A command with no definition is
// unknown, and unknown is refused.
type definition struct {
	kind   string
	option string
	args   string
	body   string
}

type Definitions map[string]definition

// ParseDefinitions reads the worker's list: one entry per command, a name,
// a kind, and the kind's detail.
func ParseDefinitions(value string) Definitions {
	entries, errs := syntax.ListValues(value)
	if len(errs) != 0 {
		return nil
	}
	result := Definitions{}
	for _, entry := range entries {
		fields, errs := syntax.ListValues(entry)
		if len(errs) != 0 || len(fields) != 3 {
			continue
		}
		definition := definition{kind: fields[1]}
		switch fields[1] {
		case "option":
			definition.option = fields[2]
		case "proc":
			parts, errs := syntax.ListValues(fields[2])
			if len(errs) != 0 || len(parts) != 2 {
				continue
			}
			definition.args, definition.body = parts[0], parts[1]
		case "command":
		default:
			continue
		}
		result[fields[0]] = definition
	}
	return result
}

// maxProcedureDepth is how many procedure bodies deep the grammar follows
// a hook: what the worker ships, and enough for a PortGroup's helper, the
// Base helpers it calls, and theirs.
const maxProcedureDepth = 5

// harmlessBuiltins are the built-in and Base commands a hook may run
// without changing what is fetched: they read, compute, or say something,
// and write only the plain variables their arguments name, which are
// checked. Control commands and the writers of variables are handled by
// name below, and everything else is refused.
var harmlessBuiltins = map[string]bool{
	"ui_error": true, "ui_warn": true, "ui_msg": true, "ui_info": true, "ui_notice": true, "ui_debug": true,
	"global": true, "variable": true, "expr": true, "string": true, "format": true, "join": true, "split": true, "concat": true,
	"list": true, "lindex": true, "llength": true, "lrange": true, "lsearch": true, "lsort": true, "lreverse": true, "lrepeat": true,
	"regexp": true, "regsub": true, "scan": true, "vercmp": true, "variant_isset": true, "variant_exists": true, "fortran_variant_name": true, "mpi_variant_name": true,
	"info": true, "return": true, "error": true, "break": true, "continue": true, "puts": true,
	// Base's registry and path queries, aliased into the port's interpreter:
	// they read what is installed and what is on the path.
	"registry_active": true, "registry_installed": true, "registry_exists": true, "registry_file_registered": true, "_portnameactive": true, "_mportsearchpath": true,
}

// fileReads are the file subcommands that read the host and change nothing.
var fileReads = map[string]bool{"exists": true, "isdirectory": true, "isfile": true, "readable": true, "executable": true, "tail": true, "dirname": true, "join": true, "normalize": true, "extension": true, "rootname": true, "split": true, "type": true}

// variableWriters write the variable their first argument names and
// nothing else; the variable must not be one the fetch reads.
var variableWriters = map[string]bool{"set": true, "append": true, "lappend": true, "incr": true, "unset": true, "lassign": false}

// effectReason judges one command by its effect on the fetch: the empty
// refusal when it demonstrably changes nothing the fetch reads, and
// otherwise why not, worded for the line that names the command. It
// follows a procedure into its body, and reports what stopped it there.
func effectReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	name, literal := command.Name(src)
	if !literal || name == "" {
		return refuse(command.Span.Start, "is a computed command")
	}
	words := command.Words
	for _, word := range words {
		if word.Expand {
			return refuse(command.Span.Start, "expands `%s`", snippet(src, word.Span))
		}
	}
	switch name {
	case "if":
		return ifReason(src, command, defs, depth)
	case "foreach", "while", "for", "catch":
		return loopReason(src, command, defs, depth)
	case "switch":
		return switchReason(src, command, defs, depth)
	case "option", "default":
		// Base's option command reads an option with one argument and
		// writes it with two; default sets an option's default. Its body
		// writes a variable named by its argument, which is what is judged
		// here rather than by following it.
		if len(words) < 2 || len(words) > 3 {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		option, ok := words[1].Literal(src)
		if !ok {
			return refuse(command.Span.Start, "names a computed option")
		}
		if len(words) == 3 || name == "default" {
			if AffectsFetch(option) {
				return refuse(command.Span.Start, "writes `%s`", option)
			}
		}
		return argumentsReason(src, words[2:], defs, depth)
	case "file":
		if len(words) < 2 {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		if sub, ok := words[1].Literal(src); !ok || !fileReads[sub] {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		return argumentsReason(src, words[2:], defs, depth)
	case "dict", "array":
		if len(words) < 2 {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		sub, ok := words[1].Literal(src)
		if !ok {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		switch sub {
		case "get", "exists", "keys", "values", "size", "names", "for", "create":
			return argumentsReason(src, words[2:], defs, depth)
		case "set", "lappend", "append", "incr", "unset":
			if len(words) < 3 {
				return refuse(command.Span.Start, "is not followed by the grammar")
			}
			if refused := variableReason(src, command, words[2]); refused.text != "" {
				return refused
			}
			return argumentsReason(src, words[3:], defs, depth)
		}
		return refuse(command.Span.Start, "is not followed by the grammar")
	}
	if variableWriters[name] {
		if len(words) < 2 {
			return refuse(command.Span.Start, "is not followed by the grammar")
		}
		if refused := variableReason(src, command, words[1]); refused.text != "" {
			return refused
		}
		return argumentsReason(src, words[2:], defs, depth)
	}
	if harmlessBuiltins[name] {
		return argumentsReason(src, words[1:], defs, depth)
	}
	if definition, ok := defs[name]; ok {
		switch definition.kind {
		case "option":
			if AffectsFetch(definition.option) {
				return refuse(command.Span.Start, "writes `%s`", definition.option)
			}
			return argumentsReason(src, words[1:], defs, depth)
		case "proc":
			if depth >= maxProcedureDepth {
				return refuse(command.Span.Start, "reaches `%s` beyond the depth the grammar follows", name)
			}
			if refused := argumentsReason(src, words[1:], defs, depth); refused.text != "" {
				return refused
			}
			body := []byte(definition.body)
			script, errs := syntax.Parse(body)
			if len(errs) != 0 {
				return refuse(command.Span.Start, "is a procedure whose body does not parse as Tcl")
			}
			if refused := scriptReason(body, script.Direct(), defs, depth+1, name); refused.text != "" {
				return refuse(command.Span.Start, "%s", refused.text)
			}
			return accepted
		}
	}
	if AffectsFetch(name) {
		return refuse(command.Span.Start, "writes `%s`", OptionName(name))
	}
	if _, known := defs[name]; known || unfollowed[name] {
		return refuse(command.Span.Start, "is not followed by the grammar")
	}
	return refuse(command.Span.Start, "is unknown to the grammar")
}

// unfollowed are the commands the grammar refuses on sight: they run
// programs, read and write files, evaluate text as code, or reshape the
// interpreter, and what they do cannot be read off a hook.
var unfollowed = map[string]bool{"exec": true, "system": true, "open": true, "close": true, "cd": true, "source": true, "eval": true, "uplevel": true, "subst": true, "namespace": true, "proc": true, "rename": true, "interp": true, "reinplace": true, "xinstall": true, "delete": true, "copy": true, "move": true, "ln": true, "touch": true, "file": true, "exit": true, "after": true, "vwait": true, "socket": true, "fconfigure": true, "read": true, "gets": true, "seek": true, "glob": true, "pwd": true, "set_option": true, "default": true, "option": true, "options": true, "variant": true, "default_variants": true, "PortGroup": true, "platform": true}

// scriptReason judges a script inside a procedure or a control body: every
// command must be a rejection or harmless by effect. A refusal names the
// command, where it is when inside a procedure, and what it does.
func scriptReason(src []byte, commands []syntax.Command, defs Definitions, depth int, where string) refusal {
	for _, command := range commands {
		if isRejection(src, command) {
			continue
		}
		if refused := effectReason(src, command, defs, depth); refused.text != "" {
			if where == "" {
				return refuse(refused.at, "runs `%s`, which %s", snippet(src, command.Span), refused.text)
			}
			return refuse(refused.at, "runs `%s` inside %s, which %s", snippet(src, command.Span), where, refused.text)
		}
	}
	return accepted
}

// ifReason judges an if by effect: braced conditions whose command calls
// are harmless, and bodies that are harmless scripts.
func ifReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	controls, bodies, ok := command.Control(src)
	if !ok || len(bodies) == 0 {
		return refuse(command.Span.Start, "has an if the grammar cannot read: `%s`", snippet(src, command.Span))
	}
	for _, control := range controls {
		if control.Expand || len(control.Segments) != 1 {
			return refuse(control.Span.Start, "has a condition that is not braced: `%s`", snippet(src, control.Span))
		}
		braced, ok := control.Segments[0].(syntax.Braced)
		if !ok {
			return refuse(control.Span.Start, "has a condition that is not braced: `%s`", snippet(src, control.Span))
		}
		if refused := conditionEffectReason(src, braced.Body, defs, depth); refused.text != "" {
			return refused
		}
	}
	for _, body := range bodies {
		block, ok := body.BracedScript(src)
		if !ok {
			return refuse(body.Span.Start, "has a branch that is not braced: `%s`", snippet(src, body.Span))
		}
		if refused := scriptReason(src, block.Direct(), defs, depth, ""); refused.text != "" {
			return refused
		}
	}
	return accepted
}

// loopReason judges foreach, while, for, and catch: their variables must
// not be ones the fetch reads, their expressions and lists must be
// harmless, and their bodies harmless scripts.
func loopReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	name, _ := command.Name(src)
	controls, scripts, ok := command.Control(src)
	if !ok {
		return refuse(command.Span.Start, "is a %s the grammar cannot read", name)
	}
	// The syntax package separates the words that run as scripts from the
	// ones that select. Which selecting words a loop binds is the
	// command's own shape: every other word of a foreach, and a catch's
	// result variables, which run nothing and so are not controls.
	var variables, values []syntax.Word
	switch name {
	case "foreach":
		if len(controls) < 2 || len(controls)%2 != 0 {
			return refuse(command.Span.Start, "is a %s the grammar cannot read", name)
		}
		for i, control := range controls {
			if i%2 == 0 {
				variables = append(variables, control)
			} else {
				values = append(values, control)
			}
		}
	case "while":
		if len(controls) != 1 {
			return refuse(command.Span.Start, "is a %s the grammar cannot read", name)
		}
		values = controls
	case "for":
		values = controls
	case "catch":
		if len(command.Words) > 4 {
			return refuse(command.Span.Start, "is a %s the grammar cannot read", name)
		}
		variables = command.Words[2:]
	}
	for _, variable := range variables {
		names, ok := variable.Literal(src)
		if !ok {
			// A braced list of loop variables is literal text too.
			if variable.Expand || len(variable.Segments) != 1 {
				return refuse(variable.Span.Start, "is a %s the grammar cannot read", name)
			}
			braced, isBraced := variable.Segments[0].(syntax.Braced)
			if !isBraced {
				return refuse(variable.Span.Start, "is a %s the grammar cannot read", name)
			}
			names = braced.Body.Text(src)
		}
		for _, variable := range strings.Fields(names) {
			if AffectsFetch(variable) {
				return refuse(command.Span.Start, "writes `%s`", variable)
			}
		}
	}
	for _, value := range values {
		// A braced expression is a condition; anything else is an argument.
		if !value.Expand && len(value.Segments) == 1 {
			if braced, ok := value.Segments[0].(syntax.Braced); ok && name != "foreach" {
				if refused := conditionEffectReason(src, braced.Body, defs, depth); refused.text != "" {
					return refused
				}
				continue
			}
		}
		if refused := argumentsReason(src, []syntax.Word{value}, defs, depth); refused.text != "" {
			return refused
		}
	}
	for _, script := range scripts {
		block, ok := script.BracedScript(src)
		if !ok {
			return refuse(script.Span.Start, "is a %s the grammar cannot read", name)
		}
		if refused := scriptReason(src, block.Direct(), defs, depth, ""); refused.text != "" {
			return refused
		}
	}
	return accepted
}

// switchReason judges a switch: its options, the string it switches on,
// and its patterns are harmless arguments, and every body is a harmless
// script. The syntax package reads the arms, braced as one list or bare.
func switchReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	controls, bodies, ok := command.Control(src)
	if !ok {
		return refuse(command.Span.Start, "is a switch the grammar cannot read")
	}
	if refused := argumentsReason(src, controls, defs, depth); refused.text != "" {
		return refused
	}
	// A match or index variable is a write, judged as a set of it is.
	for i := 0; i+1 < len(controls); i++ {
		if option, _ := controls[i].Literal(src); syntax.SwitchWritesVariable(option) {
			if refused := variableReason(src, command, controls[i+1]); refused.text != "" {
				return refused
			}
		}
	}
	for _, body := range bodies {
		block, ok := body.BracedScript(src)
		if !ok {
			return refuse(body.Span.Start, "is a switch the grammar cannot read")
		}
		if refused := scriptReason(src, block.Direct(), defs, depth, ""); refused.text != "" {
			return refused
		}
	}
	return accepted
}

// conditionEffectReason judges a braced condition by effect: it must parse
// as an expression, and every command it substitutes must be harmless.
func conditionEffectReason(src []byte, body text.Span, defs Definitions, depth int) refusal {
	e, errs := syntax.ParseExpr(src, body)
	if len(errs) != 0 {
		return refuse(body.Start, "has a condition that does not parse as an expression: `%s`", snippet(src, body))
	}
	refused := accepted
	syntax.WalkExpr(e, func(node syntax.Expr) bool {
		call, ok := node.(syntax.Call)
		if !ok {
			return refused.text == ""
		}
		for _, command := range call.Script.Direct() {
			if r := effectReason(src, command, defs, depth); r.text != "" {
				refused = r
				return false
			}
		}
		return false
	})
	return refused
}

// variableReason refuses a write to a variable the fetch reads, or to a
// variable whose name is computed.
func variableReason(src []byte, command syntax.Command, variable syntax.Word) refusal {
	name, ok := variable.Literal(src)
	if !ok {
		return refuse(command.Span.Start, "writes a computed variable")
	}
	if AffectsFetch(name) {
		return refuse(command.Span.Start, "writes `%s`", name)
	}
	return accepted
}

// argumentsReason judges the arguments of a harmless command: text and
// variable substitutions are fine, and a command substitution is judged
// by the commands it runs.
func argumentsReason(src []byte, words []syntax.Word, defs Definitions, depth int) refusal {
	for _, word := range words {
		if refused := segmentsReason(src, word.Segments, defs, depth); refused.text != "" {
			return refused
		}
	}
	return accepted
}

func segmentsReason(src []byte, segments []syntax.Segment, defs Definitions, depth int) refusal {
	for _, segment := range segments {
		switch value := segment.(type) {
		case syntax.VarSub:
			// An array element's index is text the grammar does not read;
			// one that substitutes anything is refused.
			if value.HasIndex && strings.ContainsAny(value.Index.Text(src), "[$\\") {
				return refuse(value.Span.Start, "substitutes a computed index in `%s`", snippet(src, value.Span))
			}
		case syntax.CmdSub:
			for _, command := range value.Script.Direct() {
				if refused := effectReason(src, command, defs, depth); refused.text != "" {
					return refuse(refused.at, "runs `%s`, which %s", snippet(src, command.Span), refused.text)
				}
			}
		case syntax.Quoted:
			if refused := segmentsReason(src, value.Segments, defs, depth); refused.text != "" {
				return refused
			}
		}
	}
	return accepted
}

// isRejection reports whether the command fails the fetch outright: return
// -code error with at most a plain message, or error with a plain message
// and at most Tcl's info and code arguments.
func isRejection(src []byte, command syntax.Command) bool {
	words := command.Words
	switch {
	case len(words) >= 3 && len(words) <= 4 && (syntax.Command{Words: words[:3]}).Is(src, "return", "-code", "error"):
		return len(words) == 3 || words[3].Plain()
	case len(words) >= 2 && len(words) <= 4 && !words[0].Expand && words[0].Span.Text(src) == "error":
		for _, word := range words[1:] {
			if !word.Plain() {
				return false
			}
		}
		return true
	}
	return false
}
