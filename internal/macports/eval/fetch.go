package eval

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func archiveFetchCompatible(info macports.PortInfo, procedure, pre, post string) bool {
	if procedure != "portfetch::fetch_main" || post != "" {
		return false
	}
	hooks, errs := syntax.ListValues(pre)
	if len(errs) != 0 {
		return false
	}
	for _, hook := range hooks {
		if info.Options["go.domain"] != "github.com" || !goToolchainCheck(hook) {
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
