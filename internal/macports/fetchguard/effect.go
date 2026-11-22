package fetchguard

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// A definition is what the worker says a command a hook calls is: an
// option command, judged by its option; a procedure, judged by its body;
// or a built-in command, judged by name. A command with no definition is
// unknown, and unknown is refused.
type definition struct {
	kind   string
	option string
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
			definition.body = parts[1]
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
//
// Every entry was checked on 2026-11-22 against Tcl 8.6's manual pages,
// and Base's and the PortGroups' own procedures, for writing a variable
// its arguments name or running a script they hold: variable, string is
// -failindex and info default write one, lsort -command runs one, and
// expr evaluates its braced expression; builtinReason judges those
// (#293). lassign, lmap, gets and binary scan stay off the list.
var harmlessBuiltins = withWorkerAliasReads(map[string]bool{
	"ui_error": true, "ui_warn": true, "ui_msg": true, "ui_info": true, "ui_notice": true, "ui_debug": true,
	"global": true, "variable": true, "expr": true, "string": true, "format": true, "join": true, "split": true, "concat": true,
	"list": true, "lindex": true, "llength": true, "lrange": true, "lsearch": true, "lsort": true, "lreverse": true, "lrepeat": true,
	"vercmp": true, "variant_isset": true, "variant_exists": true, "fortran_variant_name": true, "mpi_variant_name": true,
	"info": true, "return": true, "error": true, "break": true, "continue": true, "puts": true,
})

// withWorkerAliasReads adds Base's registry and path queries, aliased into
// the port's interpreter, to builtins: they read what is installed and
// what is on the path, the aliases the evaluator's dispatcher answers
// rather than refuses, by the one list (macports.WorkerAliases).
func withWorkerAliasReads(builtins map[string]bool) map[string]bool {
	for _, name := range macports.WorkerAliasReads() {
		builtins[name] = true
	}
	return builtins
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
	words := command.Words
	for _, word := range words {
		if word.Expand {
			return refuse(command.Span.Start, "expands `%s`", snippet(src, word.Span))
		}
	}
	name, literal := command.Name(src)
	if !literal || name == "" {
		// A computed name is judged by the family its fixed prefix names,
		// where every member of the family is harmless: the Java
		// PortGroup's depends_${deptype}-append writes a dependency, which
		// no fetch reads (decision 18).
		if len(words) > 0 && computedFamily(src, words[0]) == "depends_" {
			return argumentsReason(src, words, defs, depth)
		}
		return refuse(command.Span.Start, "is a computed command")
	}
	switch name {
	case "exec":
		// A hook may run a program that only reports, by the rules the
		// evaluator's dispatcher runs it by (macports.HostPrograms,
		// decision 18). What it prints lands where the word around the
		// exec is judged, a plain variable the fetch doesn't read.
		var line []macports.Argument
		for _, word := range words[1:] {
			text, plain := plainText(src, word)
			line = append(line, macports.Argument{Text: text, Literal: plain})
		}
		if reason := macports.CommandLineRefusal(line); reason != "" {
			return refuse(command.Span.Start, "%s", reason)
		}
		return argumentsReason(src, words[1:], defs, depth)
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
		case "for":
			if name == "dict" {
				return dictForReason(src, command, defs, depth)
			}
			return refuse(command.Span.Start, "is not followed by the grammar")
		case "get", "exists", "keys", "values", "size", "names", "create":
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
	case "regexp", "regsub", "scan":
		return matchReason(src, command, defs, depth)
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
		return builtinReason(src, command, name, defs, depth)
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
// programs other than exec's that only report, read and write files,
// evaluate text as code, or reshape the interpreter, and what they do
// cannot be read off a hook.
var unfollowed = map[string]bool{"system": true, "open": true, "close": true, "cd": true, "source": true, "eval": true, "uplevel": true, "subst": true, "namespace": true, "proc": true, "rename": true, "interp": true, "reinplace": true, "xinstall": true, "delete": true, "copy": true, "move": true, "ln": true, "touch": true, "file": true, "exit": true, "after": true, "vwait": true, "socket": true, "fconfigure": true, "read": true, "gets": true, "seek": true, "glob": true, "pwd": true, "set_option": true, "default": true, "option": true, "options": true, "variant": true, "default_variants": true, "PortGroup": true, "platform": true}

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
		names, ok := loopVariables(src, variable)
		if !ok {
			return refuse(variable.Span.Start, "is a %s the grammar cannot read", name)
		}
		for _, variable := range names {
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

// loopVariables are the names a loop's variable word binds: a literal
// name, or a braced list of them, which is literal text too. A name that
// isn't a plain word, as {distfiles} in a list, is one Tcl reads as a list
// element the grammar would misname, so the list is one it cannot read
// (#293; Codex's review).
func loopVariables(src []byte, variable syntax.Word) ([]string, bool) {
	text, ok := variable.Literal(src)
	if !ok {
		if variable.Expand || len(variable.Segments) != 1 {
			return nil, false
		}
		braced, isBraced := variable.Segments[0].(syntax.Braced)
		if !isBraced {
			return nil, false
		}
		text = braced.Body.Text(src)
	}
	names := strings.Fields(text)
	for i, name := range names {
		if strings.ContainsAny(name, "{}\"$[]\\") {
			return nil, false
		}
		variable, ok := variableName(name)
		if !ok {
			return nil, false
		}
		names[i] = variable
	}
	return names, true
}

// variableName is the variable a name written in a Portfile names: a
// leading :: is the global one, ::distfiles being distfiles. A name with
// :: anywhere else names a namespace's variable, which the grammar cannot
// read (#293; Codex's review of 78b4e353).
func variableName(name string) (string, bool) {
	name = strings.TrimPrefix(name, "::")
	if name == "" || strings.Contains(name, "::") {
		return "", false
	}
	return name, true
}

// dictForReason judges dict for as the loop it is: its two variables are
// written on every turn, so neither may be one the fetch reads; the
// dictionary is a harmless argument; and its body is a harmless script.
// Taken as a read, its body went unjudged (#293).
func dictForReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	words := command.Words
	if len(words) != 5 {
		return refuse(command.Span.Start, "is a dict for the grammar cannot read")
	}
	names, ok := loopVariables(src, words[2])
	if !ok || len(names) != 2 {
		return refuse(words[2].Span.Start, "is a dict for the grammar cannot read")
	}
	for _, variable := range names {
		if AffectsFetch(variable) {
			return refuse(command.Span.Start, "writes `%s`", variable)
		}
	}
	if refused := argumentsReason(src, words[3:4], defs, depth); refused.text != "" {
		return refused
	}
	block, ok := words[4].BracedScript(src)
	if !ok {
		return refuse(words[4].Span.Start, "is a dict for the grammar cannot read")
	}
	return scriptReason(src, block.Direct(), defs, depth, "")
}

// matchReason judges regexp, regsub and scan: each writes the variables
// named after its fixed arguments, which go through the check a set's
// does; the rest are harmless arguments (#293). A switch the grammar
// doesn't know, or a computed one, is refused, since it could move which
// word is a variable.
func matchReason(src []byte, command syntax.Command, defs Definitions, depth int) refusal {
	name, _ := command.Name(src)
	words := command.Words[1:]
	if name != "scan" {
		for len(words) > 0 {
			flag, ok := words[0].Literal(src)
			if !ok || !strings.HasPrefix(flag, "-") {
				break
			}
			words = words[1:]
			switch {
			case flag == "--":
			case flag == "-start":
				if len(words) == 0 {
					return refuse(command.Span.Start, "is not followed by the grammar")
				}
				if refused := argumentsReason(src, words[:1], defs, depth); refused.text != "" {
					return refused
				}
				words = words[1:]
				continue
			case matchSwitches[name][flag]:
				continue
			default:
				return refuse(command.Span.Start, "is not followed by the grammar")
			}
			break
		}
	}
	// regexp exp string ?var ...?, regsub exp string subSpec ?var?, and
	// scan string format ?var ...?.
	fixed := map[string]int{"regexp": 2, "regsub": 3, "scan": 2}[name]
	if len(words) < fixed {
		return refuse(command.Span.Start, "is not followed by the grammar")
	}
	if refused := argumentsReason(src, words[:fixed], defs, depth); refused.text != "" {
		return refused
	}
	for _, variable := range words[fixed:] {
		if refused := variableReason(src, command, variable); refused.text != "" {
			return refused
		}
	}
	return accepted
}

// matchSwitches are the switches regexp and regsub take that bind no
// argument of their own. regsub's -command isn't one: it runs its
// substitution as a command, so it is refused.
var matchSwitches = map[string]map[string]bool{
	"regexp": {"-about": true, "-expanded": true, "-indices": true, "-line": true, "-linestop": true, "-lineanchor": true, "-nocase": true, "-all": true, "-inline": true},
	"regsub": {"-all": true, "-expanded": true, "-line": true, "-linestop": true, "-lineanchor": true, "-nocase": true},
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
func conditionEffectReason(src []byte, body textedit.Span, defs Definitions, depth int) refusal {
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
	written, ok := variable.Literal(src)
	if !ok {
		return refuse(command.Span.Start, "writes a computed variable")
	}
	name, ok := variableName(written)
	if !ok {
		return refuse(command.Span.Start, "writes `%s`, a variable the grammar cannot read", written)
	}
	if AffectsFetch(name) {
		return refuse(command.Span.Start, "writes `%s`", name)
	}
	return accepted
}

// builtinReason judges a harmless builtin's arguments, and the variables
// and scripts the few that write or run one name (harmlessBuiltins): what
// the grammar can't read of those is refused, as a computed subcommand or
// option is, since it could be the one that writes.
func builtinReason(src []byte, command syntax.Command, name string, defs Definitions, depth int) refusal {
	words := command.Words
	unread := refuse(command.Span.Start, "is not followed by the grammar")
	switch name {
	case "variable":
		// variable ?name value ...? ?name?: each name given a value is
		// written; a last one alone is only declared.
		for i := 1; i+1 < len(words); i += 2 {
			if refused := variableReason(src, command, words[i]); refused.text != "" {
				return refused
			}
		}
	case "string":
		// string is class ?-strict? ?-failindex varname? string.
		sub, ok := literal(src, words, 1)
		if !ok {
			return unread
		}
		if sub == "is" {
			for i := 3; i < len(words)-1; i++ {
				option, ok := words[i].Literal(src)
				switch {
				case !ok:
					return unread
				case abbreviates(option, "-failindex"):
					if i+1 >= len(words)-1 {
						return unread
					}
					i++
					if refused := variableReason(src, command, words[i]); refused.text != "" {
						return refused
					}
				case !abbreviates(option, "-strict"):
					return unread
				}
			}
		}
	case "info":
		// info default procname arg varname; d is default's shortest
		// abbreviation, which Tcl takes.
		sub, ok := literal(src, words, 1)
		if !ok {
			return unread
		}
		if abbreviates(sub, "default") {
			if len(words) != 5 {
				return unread
			}
			if refused := variableReason(src, command, words[4]); refused.text != "" {
				return refused
			}
		}
	case "lsort":
		// lsort ?options? list: -command runs a script the grammar
		// doesn't follow, and an option it can't read could be it.
		for i := 1; i < len(words)-1; i++ {
			option, ok := words[i].Literal(src)
			if !ok || abbreviates(option, "-command") {
				return unread
			}
			if option == "-index" || option == "-stride" {
				i++
			}
		}
	case "expr":
		// A braced expression is evaluated, its commands with it.
		if len(words) == 2 && len(words[1].Segments) == 1 {
			if braced, ok := words[1].Segments[0].(syntax.Braced); ok {
				return conditionEffectReason(src, braced.Body, defs, depth)
			}
		}
	}
	return argumentsReason(src, words[1:], defs, depth)
}

// literal is words[i]'s literal text, where there is one.
func literal(src []byte, words []syntax.Word, i int) (string, bool) {
	if i >= len(words) {
		return "", false
	}
	return words[i].Literal(src)
}

// abbreviates reports whether word names option as Tcl's option and
// subcommand tables take it: the option itself or a prefix of it, beyond
// a lone dash. The callers' options have no other with that prefix.
func abbreviates(word, option string) bool {
	return word != "" && word != "-" && strings.HasPrefix(option, word)
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

// plainText is a word's text where it substitutes nothing: bare, braced,
// or quoted around literal text.
func plainText(src []byte, word syntax.Word) (string, bool) {
	if text, ok := word.Literal(src); ok {
		return text, true
	}
	if len(word.Segments) != 1 {
		return "", false
	}
	switch segment := word.Segments[0].(type) {
	case syntax.Braced:
		return segment.Body.Text(src), true
	case syntax.Quoted:
		var text strings.Builder
		for _, inner := range segment.Segments {
			literal, ok := inner.(syntax.Literal)
			if !ok {
				return "", false
			}
			text.WriteString(literal.Span.Text(src))
		}
		return text.String(), true
	}
	return "", false
}

// computedFamily is the fixed prefix of a computed command name, the text
// before its first substitution.
func computedFamily(src []byte, word syntax.Word) string {
	if len(word.Segments) < 2 {
		return ""
	}
	literal, ok := word.Segments[0].(syntax.Literal)
	if !ok {
		return ""
	}
	return literal.Span.Text(src)
}
