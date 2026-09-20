package eval

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

func assessFetch(info macports.PortInfo, procedure, pre, post string) macports.FetchSemantics {
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
		switch {
		case rejectionOnly(hook):
			result.Rejected = true
			result.Guards = append(result.Guards, fmt.Sprintf("pre-fetch hook %d unconditionally rejects this platform", i+1))
		case conditionalRejection(hook):
			result.Guards = append(result.Guards, fmt.Sprintf("pre-fetch hook %d only rejects unsupported configurations", i+1))
		case info.Options["go.domain"] == "github.com" && goToolchainCheck(hook):
			result.Guards = append(result.Guards, "Go toolchain compatibility guard")
		default:
			result.Problem = fmt.Sprintf("pre-fetch hook %d has unrecognized behavior", i+1)
			return result
		}
	}
	result.Kind = "standard"
	if len(result.Guards) > 0 {
		result.Kind = "guarded"
	}
	return result
}

// A rejection-only hook consists of harmless diagnostic arguments followed by
// an unconditional error return. The registered Base wrapper must also match.
func rejectionOnly(body string) bool {
	body, ok := hookBody(body)
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	return rejectionCommands(src, script.Direct())
}

// rejectionCommands accepts diagnostics followed by an unconditional error return.
func rejectionCommands(src []byte, commands []syntax.Command) bool {
	if len(commands) == 0 {
		return false
	}
	for i, command := range commands {
		words := command.Words
		if i == len(commands)-1 {
			if len(words) < 3 || len(words) > 4 || !(syntax.Command{Words: words[:3]}).Is(src, "return", "-code", "error") {
				return false
			}
			if len(words) == 4 && (!words[3].Plain()) {
				return false
			}
		} else if len(words) != 2 || words[0].Span.Text(src) != "ui_error" || !words[1].Plain() {
			return false
		}
	}
	return true
}

// A conditional rejection consists only of if statements whose conditions
// read variables and whose every branch is a rejection: the perl5 PortGroup's
// required-variant check, for example. Such a hook can fail the fetch but
// never change what is fetched. Conditions may call the variant queries in
// pureConditionCommands, which the compilers PortGroup's Fortran check
// needs; any other command substitution, and branches that do anything
// else, are not recognized.
func conditionalRejection(body string) bool {
	body, ok := hookBody(body)
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	commands := script.Direct()
	if len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		name, _ := command.Name(src)
		controls, bodies, ok := command.Control(src)
		if !ok || name != "if" || len(bodies) == 0 {
			return false
		}
		for _, control := range controls {
			if control.Expand || len(control.Segments) != 1 {
				return false
			}
			braced, ok := control.Segments[0].(syntax.Braced)
			if !ok || !pureCondition(src, braced.Body) {
				return false
			}
		}
		for _, body := range bodies {
			block, ok := body.BracedScript(src)
			if !ok || !rejectionCommands(src, block.Direct()) {
				return false
			}
		}
	}
	return true
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
	e, errs := syntax.ParseExpr(src, body)
	if len(errs) != 0 {
		return false
	}
	pure := true
	syntax.WalkExpr(e, func(node syntax.Expr) bool {
		call, ok := node.(syntax.Call)
		if !ok {
			return pure
		}
		commands := call.Script.Direct()
		if len(commands) != 1 {
			pure = false
			return false
		}
		name, _ := commands[0].Name(src)
		args, literal := commands[0].LiteralArgs(src)
		if !pureConditionCommands[name] || !literal {
			pure = false
			return false
		}
		for _, argument := range args {
			if strings.ContainsAny(argument, "$[]{}\\\"") {
				pure = false
			}
		}
		return false
	})
	return pure
}

// The Go PortGroup's compatibility check does not change the fetched archive.
// Recognize its structure; different commands or substitutions prevent direct
// archive fetching. MacPorts still runs this check during verification.
func goToolchainCheck(body string) bool {
	body, ok := hookBody(body)
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	commands := script.Direct()
	if len(commands) != 4 || !commands[0].Is(src, "global", "go.toolchain_unmet") || !commands[1].Is(src, "set", "ceiling", "[go_toolchain.ceiling]") {
		return false
	}
	for index, condition := range []string{`${ceiling} eq "none"`, `${go.toolchain_unmet} ne ""`} {
		command := commands[index+2]
		if len(command.Words) != 3 || command.Words[0].Span.Text(src) != "if" {
			return false
		}
		word := command.Words[1]
		if word.Expand || len(word.Segments) != 1 {
			return false
		}
		braced, ok := word.Segments[0].(syntax.Braced)
		if !ok || strings.Join(strings.Fields(braced.Body.Text(src)), " ") != condition {
			return false
		}
		block, ok := command.Words[2].BracedScript(src)
		if !ok {
			return false
		}
		statements := block.Direct()
		if len(statements) != 2 || len(statements[0].Words) != 2 || len(statements[1].Words) != 4 {
			return false
		}
		if statements[0].Words[0].Span.Text(src) != "ui_error" || !(syntax.Command{Words: statements[1].Words[:3]}).Is(src, "return", "-code", "error") {
			return false
		}
		for _, message := range []syntax.Word{statements[0].Words[1], statements[1].Words[3]} {
			if !message.Plain() {
				return false
			}
		}
	}
	return true
}

// A hook registered through Base arrives wrapped: its first line imports
// every global, and the body the Portfile wrote follows.
const baseHookPrefix = "global {*}[info globals]\n"

func hookBody(body string) (string, bool) { return strings.CutPrefix(body, baseHookPrefix) }
