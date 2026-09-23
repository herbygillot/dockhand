// Package fetchguard reads a port's pre-fetch hooks and decides whether the
// fetch is standard, guarded by a rejection that changes nothing fetched,
// or custom. It is static analysis over the hook text and the definitions
// the observation worker ships, with no interpreter of its own.
package fetchguard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// Origin says where a pre-fetch hook was written: the Portfile or a
// PortGroup, and the line its body starts on. Unknown when Label is empty.
type Origin struct {
	Label string
	Line  int
}

func Assess(info macports.PortInfo, procedure, pre, post string, origins []Origin, defs Definitions) macports.FetchSemantics {
	result := macports.FetchSemantics{Kind: "custom", Procedure: procedure}
	if procedure != "portfetch::fetch_main" {
		result.Problem = "custom fetch procedure " + procedure
		return result
	}
	if post != "" {
		result.Problem = "post-fetch hooks can modify archive preparation"
		return result
	}
	hooks, errs := syntax.ListValues(pre)
	if len(errs) != 0 {
		result.Problem = "unrecognized pre-fetch registration"
		return result
	}
	for i, hook := range hooks {
		guard, rejected, refused := classifyHook(info, hook, defs)
		if refused.text != "" {
			var origin Origin
			if i < len(origins) {
				origin = origins[i]
			}
			result.Problem = fmt.Sprintf("pre-fetch hook %d %s%s", i+1, refused.text, refused.where(hook, origin))
			return result
		}
		result.Rejected = result.Rejected || rejected
		if guard == "Go toolchain compatibility guard" {
			result.Guards = append(result.Guards, guard)
		} else {
			result.Guards = append(result.Guards, fmt.Sprintf("pre-fetch hook %d %s", i+1, guard))
		}
	}
	result.Kind = "standard"
	if len(result.Guards) > 0 {
		result.Kind = "guarded"
	}
	return result
}

// A refusal names the first command or condition the grammar stopped at,
// and where it is in the hook body, so the message can say the line.
type refusal struct {
	text string
	at   int
}

var accepted = refusal{at: -1}

func refuse(at int, format string, args ...any) refusal {
	return refusal{text: fmt.Sprintf(format, args...), at: at}
}

// where places the refusal: at the Portfile line or the PortGroup line of
// the offending command when the hook's origin is known, else nothing. The
// origin's line is where the trimmed body starts, so leading blank lines of
// the body do not count.
func (r refusal) where(hook string, origin Origin) string {
	if origin.Label == "" || origin.Line <= 0 {
		return ""
	}
	line := origin.Line
	if body, ok := hookBody(hook); ok && r.at >= 0 {
		src := []byte(body)
		offending, _ := text.Position(src, r.at)
		lead, _ := text.Position(src, len(body)-len(strings.TrimLeft(body, " \t\r\n")))
		line += offending - lead
	}
	if origin.Label == "Portfile" {
		return fmt.Sprintf(", at Portfile line %d", line)
	}
	return fmt.Sprintf(", in %s at line %d", origin.Label, line)
}

// classifyHook recognizes one registered hook by the shape of its first
// command and says which guard it is, whether it rejects unconditionally,
// and otherwise why the grammar refused it. The specific checks come
// first: the Go PortGroup's toolchain check by its first line. A hook that
// starts with if is read as a conditional rejection; anything else as a
// rejection, or as a hook that changes nothing the fetch reads. Commands
// the grammar's own rules do not name are judged by their effect on the
// fetch, a procedure by its body as the worker shipped it.
func classifyHook(info macports.PortInfo, hook string, defs Definitions) (guard string, rejected bool, refused refusal) {
	body, ok := hookBody(hook)
	if !ok {
		return "", false, refuse(-1, "is not wrapped as a Base hook")
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return "", false, refuse(-1, "does not parse as Tcl")
	}
	commands := script.Direct()
	if len(commands) == 0 {
		return "", false, refuse(-1, "is empty")
	}
	name, _ := commands[0].Name(src)
	switch {
	case commands[0].Is(src, "global", "go.toolchain_unmet"):
		if domain := info.Options["go.domain"]; domain != "github.com" {
			if domain == "" {
				domain = "unset"
			}
			return "", false, refuse(-1, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is %s", domain)
		}
		return "Go toolchain compatibility guard", false, goToolchainReason(src, commands)
	case name == "if":
		if refused := conditionalReason(src, commands, defs); refused.text != "" {
			return "", false, refused
		}
		// A conditional hook that ends by rejecting outright is a rejection.
		return "only rejects unsupported configurations", isRejection(src, commands[len(commands)-1]), accepted
	}
	if refused := rejectionReason(src, commands, defs); refused.text != "" {
		return "", false, refused
	}
	if isRejection(src, commands[len(commands)-1]) {
		return "unconditionally rejects this platform", true, accepted
	}
	// A hook that opens with a helper and rejects inside an if later is
	// a conditional rejection all the same.
	for _, command := range commands {
		if name, _ := command.Name(src); name == "if" {
			return "only rejects unsupported configurations", false, accepted
		}
	}
	return "changes nothing the fetch reads", false, accepted
}

// rejectionReason accepts commands that change nothing the fetch reads,
// diagnostics first among them, followed by an unconditional error,
// return -code error or Tcl's error with one plain message, or by
// nothing that rejects at all; otherwise it names the command that does
// something to the fetch, and what.
func rejectionReason(src []byte, commands []syntax.Command, defs Definitions) refusal {
	if len(commands) == 0 {
		return refuse(-1, "is empty")
	}
	for i, command := range commands {
		words := command.Words
		last := i == len(commands)-1
		if last {
			var message *syntax.Word
			switch {
			case len(words) >= 3 && len(words) <= 4 && (syntax.Command{Words: words[:3]}).Is(src, "return", "-code", "error"):
				if len(words) == 4 {
					message = &words[3]
				}
			case len(words) == 2 && !words[0].Expand && words[0].Span.Text(src) == "error":
				message = &words[1]
			default:
				if refused := effectReason(src, command, defs, 0); refused.text != "" {
					return refuse(command.Span.Start, "ends with `%s` rather than return -code error, which %s", snippet(src, command.Span), refused.text)
				}
				continue
			}
			if message != nil && !message.Plain() {
				return refuse(command.Span.Start, "returns a computed message `%s`", snippet(src, message.Span))
			}
			continue
		}
		if len(words) == 2 && words[0].Span.Text(src) == "ui_error" && words[1].Plain() {
			continue
		}
		if refused := effectReason(src, command, defs, 0); refused.text != "" {
			return refuse(command.Span.Start, "runs `%s` before rejecting, which %s", snippet(src, command.Span), refused.text)
		}
	}
	return accepted
}
func conditionalReason(src []byte, commands []syntax.Command, defs Definitions) refusal {
	if len(commands) == 0 {
		return refuse(-1, "is empty")
	}
	for _, command := range commands {
		name, _ := command.Name(src)
		controls, bodies, ok := command.Control(src)
		if name != "if" {
			// A command beside the ifs may reject, or change nothing the
			// fetch reads; anything else is refused by what it does.
			if isRejection(src, command) {
				continue
			}
			if refused := effectReason(src, command, defs, 0); refused.text != "" {
				return refuse(command.Span.Start, "runs `%s` outside an if, which %s", snippet(src, command.Span), refused.text)
			}
			continue
		}
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
			if refused := pureConditionReason(src, braced.Body, defs); refused.text != "" {
				return refused
			}
		}
		for _, body := range bodies {
			block, ok := body.BracedScript(src)
			if !ok {
				return refuse(body.Span.Start, "has a branch that is not braced: `%s`", snippet(src, body.Span))
			}
			if refused := branchReason(src, block.Direct(), defs); refused.text != "" {
				return refused
			}
		}
	}
	return accepted
}

// branchReason reads one branch of a conditional rejection: a rejection,
// or, when it opens with if, a conditional rejection in its own right,
// whose refusals already say what they stopped at.
func branchReason(src []byte, commands []syntax.Command, defs Definitions) refusal {
	if len(commands) > 0 {
		if name, _ := commands[0].Name(src); name == "if" {
			return conditionalReason(src, commands, defs)
		}
	}
	if refused := rejectionReason(src, commands, defs); refused.text != "" {
		return refuse(refused.at, "has a branch that %s", refused.text)
	}
	return accepted
}

// pureConditionCommands are the commands a rejection's condition may call:
// queries of the selected variants that read interpreter state and change
// nothing, and take literal arguments only.
var pureConditionCommands = map[string]bool{"variant_isset": true, "variant_exists": true, "fortran_variant_name": true, "mpi_variant_name": true}

// hostReadCommands are the reads of the host a rejection's condition may
// make, each a predicate with no effect on the host or the interpreter: a
// file's existence or kind, a version comparison, whether a variable is
// set. Their arguments may substitute variables but not commands.
var hostReadCommands = map[string]func(args []string) bool{
	"file": func(args []string) bool {
		return len(args) == 2 && (args[0] == "exists" || args[0] == "isdirectory" || args[0] == "isfile")
	},
	"vercmp": func(args []string) bool { return len(args) == 2 || len(args) == 3 },
	"info":   func(args []string) bool { return len(args) == 2 && args[0] == "exists" },
}

func pureConditionReason(src []byte, body text.Span, defs Definitions) refusal {
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
		commands := call.Script.Direct()
		if len(commands) != 1 {
			refused = refuse(body.Start, "has a condition with a command substitution the grammar cannot read: `%s`", snippet(src, body))
			return false
		}
		name, _ := commands[0].Name(src)
		if allowed, ok := hostReadCommands[name]; ok {
			switch plain, read := hostRead(src, commands[0], allowed); {
			case !plain:
				refused = refuse(commands[0].Span.Start, "calls `%s` with a computed argument in its condition: `%s`", name, snippet(src, body))
			case !read:
				refused = refuse(commands[0].Span.Start, "calls `%s` in its condition: `%s`", snippet(src, commands[0].Span), snippet(src, body))
			}
			return false
		}
		args, literal := commands[0].LiteralArgs(src)
		switch {
		case !pureConditionCommands[name]:
			// A call the grammar does not name is judged by its effect: a
			// read of the host or the records changes nothing the fetch
			// reads, and is admitted; a write, or a command the grammar
			// cannot follow, is refused by what it does.
			if effect := effectReason(src, commands[0], defs, 0); effect.text != "" {
				refused = refuse(commands[0].Span.Start, "calls `%s` in its condition, which %s: `%s`", name, effect.text, snippet(src, body))
			}
		case !literal:
			refused = refuse(commands[0].Span.Start, "calls `%s` with a computed argument in its condition: `%s`", name, snippet(src, body))
		}
		for _, argument := range args {
			if refused.text == "" && strings.ContainsAny(argument, "$[]{}\\\"") {
				refused = refuse(commands[0].Span.Start, "calls `%s` with a computed argument in its condition: `%s`", name, snippet(src, body))
			}
		}
		return false
	})
	return refused
}

// hostRead reports whether a host read's arguments are all plain, text and
// simple variable substitutions, and whether its subcommand and arity are
// ones the rule allows, judged on the words' text with the substitutions
// left in place.
func hostRead(src []byte, command syntax.Command, allowed func(args []string) bool) (plain, read bool) {
	args := make([]string, 0, len(command.Words)-1)
	for _, word := range command.Words[1:] {
		if !word.Plain() {
			return false, false
		}
		args = append(args, word.Span.Text(src))
	}
	return true, allowed(args)
}
func goToolchainReason(src []byte, commands []syntax.Command) refusal {
	const known = "differs from the Go PortGroup's toolchain check as dockhand knows it"
	if len(commands) != 4 || !commands[0].Is(src, "global", "go.toolchain_unmet") || !commands[1].Is(src, "set", "ceiling", "[go_toolchain.ceiling]") {
		at := -1
		if len(commands) > 0 {
			at = commands[len(commands)-1].Span.Start
			if len(commands) > 1 && !commands[1].Is(src, "set", "ceiling", "[go_toolchain.ceiling]") {
				at = commands[1].Span.Start
			}
		}
		return refuse(at, "%s", known)
	}
	for index, condition := range []string{`${ceiling} eq "none"`, `${go.toolchain_unmet} ne ""`} {
		command := commands[index+2]
		if len(command.Words) != 3 || command.Words[0].Span.Text(src) != "if" {
			return refuse(command.Span.Start, "%s", known)
		}
		word := command.Words[1]
		if word.Expand || len(word.Segments) != 1 {
			return refuse(command.Span.Start, "%s", known)
		}
		braced, ok := word.Segments[0].(syntax.Braced)
		if !ok || strings.Join(strings.Fields(braced.Body.Text(src)), " ") != condition {
			return refuse(command.Span.Start, "%s", known)
		}
		block, ok := command.Words[2].BracedScript(src)
		if !ok {
			return refuse(command.Span.Start, "%s", known)
		}
		statements := block.Direct()
		if len(statements) != 2 || len(statements[0].Words) != 2 || len(statements[1].Words) != 4 {
			return refuse(command.Span.Start, "%s", known)
		}
		if statements[0].Words[0].Span.Text(src) != "ui_error" || !(syntax.Command{Words: statements[1].Words[:3]}).Is(src, "return", "-code", "error") {
			return refuse(command.Span.Start, "%s", known)
		}
		for _, message := range []syntax.Word{statements[0].Words[1], statements[1].Words[3]} {
			if !message.Plain() {
				return refuse(message.Span.Start, "%s", known)
			}
		}
	}
	return accepted
}

// snippet is one line of source for a message: whitespace collapsed and cut
// at sixty characters.
func snippet(src []byte, span text.Span) string {
	line := strings.Join(strings.Fields(span.Text(src)), " ")
	if len(line) > 60 {
		line = line[:57] + "..."
	}
	return line
}

// ParseOrigins reads the worker's origin list: one entry per hook, each a
// label and a line, or empty when the body was found in no file.
func ParseOrigins(value string) []Origin {
	entries, errs := syntax.ListValues(value)
	if len(errs) != 0 {
		return nil
	}
	origins := make([]Origin, len(entries))
	for i, entry := range entries {
		fields, errs := syntax.ListValues(entry)
		if len(errs) != 0 || len(fields) != 2 {
			continue
		}
		line, err := strconv.Atoi(fields[1])
		if err != nil || line <= 0 {
			continue
		}
		origins[i] = Origin{Label: fields[0], Line: line}
	}
	return origins
}

// A hook registered through Base arrives wrapped: its first line imports
// every global, and the body the Portfile wrote follows.
const baseHookPrefix = "global {*}[info globals]\n"

func hookBody(body string) (string, bool) { return strings.CutPrefix(body, baseHookPrefix) }
