package eval

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
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
	body, ok := strings.CutPrefix(body, "global {*}[info globals]\n")
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	return rejectionCommands(src, scriptCommands(script))
}

// rejectionCommands accepts diagnostics followed by an unconditional error return.
func rejectionCommands(src []byte, commands []syntax.Command) bool {
	if len(commands) == 0 {
		return false
	}
	for i, command := range commands {
		words := command.Words
		if i == len(commands)-1 {
			if len(words) < 3 || len(words) > 4 || !commandWords(src, syntax.Command{Words: words[:3]}, "return", "-code", "error") {
				return false
			}
			if len(words) == 4 && (words[3].Expand || !messageSegments(words[3].Segments)) {
				return false
			}
		} else if len(words) != 2 || words[0].Span.Text(src) != "ui_error" || words[1].Expand || !messageSegments(words[1].Segments) {
			return false
		}
	}
	return true
}

// A conditional rejection consists only of if statements whose conditions
// read variables and whose every branch is a rejection: the perl5 PortGroup's
// required-variant check, for example. Such a hook can fail the fetch but
// never change what is fetched. Conditions with command substitutions, and
// branches that do anything else, are not recognized.
func conditionalRejection(body string) bool {
	body, ok := strings.CutPrefix(body, "global {*}[info globals]\n")
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	commands := scriptCommands(script)
	if len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		words := command.Words
		if len(words) < 3 || words[0].Span.Text(src) != "if" {
			return false
		}
		expectCondition := true
		for _, word := range words[1:] {
			literal, _ := word.Literal(src)
			switch {
			case expectCondition:
				if word.Expand || len(word.Segments) != 1 {
					return false
				}
				braced, ok := word.Segments[0].(syntax.Braced)
				if !ok || strings.ContainsAny(braced.Body.Text(src), "[]") {
					return false
				}
				expectCondition = false
			case literal == "then" || literal == "else":
			case literal == "elseif":
				expectCondition = true
			default:
				block, ok := word.BracedScript(src)
				if !ok || !rejectionCommands(src, scriptCommands(block)) {
					return false
				}
			}
		}
		if expectCondition {
			return false
		}
	}
	return true
}

// The Go PortGroup's compatibility check does not change the fetched archive.
// Recognize its structure; different commands or substitutions prevent direct
// archive fetching. MacPorts still runs this check during verification.
func goToolchainCheck(body string) bool {
	body, ok := strings.CutPrefix(body, "global {*}[info globals]\n")
	if !ok {
		return false
	}
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	commands := scriptCommands(script)
	if len(commands) != 4 || !commandWords(src, commands[0], "global", "go.toolchain_unmet") || !commandWords(src, commands[1], "set", "ceiling", "[go_toolchain.ceiling]") {
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
		statements := scriptCommands(block)
		if len(statements) != 2 || len(statements[0].Words) != 2 || len(statements[1].Words) != 4 {
			return false
		}
		if statements[0].Words[0].Span.Text(src) != "ui_error" || !commandWords(src, syntax.Command{Words: statements[1].Words[:3]}, "return", "-code", "error") {
			return false
		}
		for _, message := range []syntax.Word{statements[0].Words[1], statements[1].Words[3]} {
			if message.Expand || !messageSegments(message.Segments) {
				return false
			}
		}
	}
	return true
}

func scriptCommands(script *syntax.Script) []syntax.Command {
	var commands []syntax.Command
	for _, item := range script.Items {
		if command, ok := item.(syntax.Command); ok {
			commands = append(commands, command)
		}
	}
	return commands
}

func commandWords(src []byte, command syntax.Command, expected ...string) bool {
	if len(command.Words) != len(expected) {
		return false
	}
	for i, word := range command.Words {
		if word.Expand || word.Span.Text(src) != expected[i] {
			return false
		}
	}
	return true
}

func messageSegments(segments []syntax.Segment) bool {
	for _, segment := range segments {
		switch value := segment.(type) {
		case syntax.Literal, syntax.Braced:
		case syntax.VarSub:
			if value.HasIndex {
				return false
			}
		case syntax.Quoted:
			if !messageSegments(value.Segments) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
