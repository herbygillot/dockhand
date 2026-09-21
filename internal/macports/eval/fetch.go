package eval

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// hookOrigin says where a pre-fetch hook was written: the Portfile or a
// PortGroup, and the line its body starts on. Unknown when Label is empty.
type hookOrigin struct {
	Label string
	Line  int
}

func assessFetch(info macports.PortInfo, procedure, pre, post string, origins []hookOrigin) macports.FetchSemantics {
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
		guard, rejected, refused := classifyHook(info, hook)
		if refused.text != "" {
			var origin hookOrigin
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
func (r refusal) where(hook string, origin hookOrigin) string {
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
// and otherwise why the grammar refused it. A hook that starts with if is
// read as a conditional rejection; the Go PortGroup's toolchain check by its
// first line; anything else as an unconditional rejection.
func classifyHook(info macports.PortInfo, hook string) (guard string, rejected bool, refused refusal) {
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
	case name == "if":
		return "only rejects unsupported configurations", false, conditionalReason(src, commands)
	case commands[0].Is(src, "global", "go.toolchain_unmet"):
		if domain := info.Options["go.domain"]; domain != "github.com" {
			if domain == "" {
				domain = "unset"
			}
			return "", false, refuse(-1, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is %s", domain)
		}
		return "Go toolchain compatibility guard", false, goToolchainReason(src, commands)
	}
	return "unconditionally rejects this platform", true, rejectionReason(src, commands)
}

// A rejection-only hook consists of harmless diagnostic arguments followed by
// an unconditional error return. The registered Base wrapper must also match.
func rejectionOnly(hook string) bool {
	src, commands, ok := parseHook(hook)
	return ok && rejectionReason(src, commands).text == ""
}

// rejectionReason accepts diagnostics followed by an unconditional error
// return, and otherwise names the command that is neither.
func rejectionReason(src []byte, commands []syntax.Command) refusal {
	if len(commands) == 0 {
		return refuse(-1, "is empty")
	}
	for i, command := range commands {
		words := command.Words
		if i == len(commands)-1 {
			if len(words) < 3 || len(words) > 4 || !(syntax.Command{Words: words[:3]}).Is(src, "return", "-code", "error") {
				return refuse(command.Span.Start, "ends with `%s` rather than return -code error", snippet(src, command.Span))
			}
			if len(words) == 4 && (!words[3].Plain()) {
				return refuse(command.Span.Start, "returns a computed message `%s`", snippet(src, words[3].Span))
			}
		} else if len(words) != 2 || words[0].Span.Text(src) != "ui_error" || !words[1].Plain() {
			return refuse(command.Span.Start, "runs `%s` before rejecting", snippet(src, command.Span))
		}
	}
	return accepted
}

// A conditional rejection consists only of if statements whose conditions
// read variables and whose every branch is a rejection: the perl5 PortGroup's
// required-variant check, for example. Such a hook can fail the fetch but
// never change what is fetched. Conditions may call the variant queries in
// pureConditionCommands, which the compilers PortGroup's Fortran check
// needs; any other command substitution, and branches that do anything
// else, are not recognized.
func conditionalRejection(hook string) bool {
	src, commands, ok := parseHook(hook)
	return ok && conditionalReason(src, commands).text == ""
}

func conditionalReason(src []byte, commands []syntax.Command) refusal {
	if len(commands) == 0 {
		return refuse(-1, "is empty")
	}
	for _, command := range commands {
		name, _ := command.Name(src)
		controls, bodies, ok := command.Control(src)
		if name != "if" {
			return refuse(command.Span.Start, "runs `%s` outside an if", snippet(src, command.Span))
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
			if refused := pureConditionReason(src, braced.Body); refused.text != "" {
				return refused
			}
		}
		for _, body := range bodies {
			block, ok := body.BracedScript(src)
			if !ok {
				return refuse(body.Span.Start, "has a branch that is not braced: `%s`", snippet(src, body.Span))
			}
			if refused := rejectionReason(src, block.Direct()); refused.text != "" {
				return refuse(refused.at, "has a branch that %s", refused.text)
			}
		}
	}
	return accepted
}

// pureConditionCommands are the commands a rejection's condition may call:
// queries of the selected variants that read interpreter state and change
// nothing, and take literal arguments only.
var pureConditionCommands = map[string]bool{"variant_isset": true, "variant_exists": true, "fortran_variant_name": true}

// pureCondition accepts a condition whose command substitutions, if any, are
// all pure variant queries with literal arguments. A condition that does not
// parse as an expression, a call to anything else, an argument that is not a
// bare literal, and a nested substitution are refused.
func pureCondition(src []byte, body text.Span) bool {
	return pureConditionReason(src, body).text == ""
}

func pureConditionReason(src []byte, body text.Span) refusal {
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
		args, literal := commands[0].LiteralArgs(src)
		switch {
		case !pureConditionCommands[name]:
			refused = refuse(commands[0].Span.Start, "calls `%s` in its condition: `%s`", name, snippet(src, body))
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

// The Go PortGroup's compatibility check does not change the fetched archive.
// Recognize its structure; different commands or substitutions prevent direct
// archive fetching. MacPorts still runs this check during verification.
func goToolchainCheck(hook string) bool {
	src, commands, ok := parseHook(hook)
	return ok && goToolchainReason(src, commands).text == ""
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

// parseHook strips the Base wrapper and parses the body a Portfile wrote.
func parseHook(hook string) ([]byte, []syntax.Command, bool) {
	body, ok := hookBody(hook)
	if !ok {
		return nil, nil, false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, nil, false
	}
	return src, script.Direct(), true
}

// parseOrigins reads the worker's origin list: one entry per hook, each a
// label and a line, or empty when the body was found in no file.
func parseOrigins(value string) []hookOrigin {
	entries, errs := syntax.ListValues(value)
	if len(errs) != 0 {
		return nil
	}
	origins := make([]hookOrigin, len(entries))
	for i, entry := range entries {
		fields, errs := syntax.ListValues(entry)
		if len(errs) != 0 || len(fields) != 2 {
			continue
		}
		line, err := strconv.Atoi(fields[1])
		if err != nil || line <= 0 {
			continue
		}
		origins[i] = hookOrigin{Label: fields[0], Line: line}
	}
	return origins
}

// A hook registered through Base arrives wrapped: its first line imports
// every global, and the body the Portfile wrote follows.
const baseHookPrefix = "global {*}[info globals]\n"

func hookBody(body string) (string, bool) { return strings.CutPrefix(body, baseHookPrefix) }
