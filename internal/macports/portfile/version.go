package portfile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

type Candidate struct {
	Span  text.Span
	Value string
}

func Literal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-+", r)) {
			return false
		}
	}
	return true
}

func Candidates(src []byte) ([]Candidate, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("portfile: invalid Tcl syntax")
	}

	var result []Candidate
	seen := map[text.Span]bool{}
	var walk func(*syntax.Script, bool)
	var word func(syntax.Word)
	var substitutions func([]syntax.Segment)
	substitutions = func(segments []syntax.Segment) {
		for _, segment := range segments {
			switch value := segment.(type) {
			case syntax.CmdSub:
				walk(value.Script, true)
			case syntax.Quoted:
				substitutions(value.Segments)
			}
		}
	}
	word = func(w syntax.Word) {
		if w.Expand {
			return
		}
		span := w.Span
		value := span.Text(src)
		if len(value) >= 2 && ((value[0] == '{' && value[len(value)-1] == '}') || (value[0] == '"' && value[len(value)-1] == '"')) {
			span.Start++
			span.End--
			value = span.Text(src)
		}
		if Literal(value) && strings.ContainsAny(value, "0123456789") && !seen[span] {
			seen[span] = true
			result = append(result, Candidate{Span: span, Value: value})
		}
		substitutions(w.Segments)
	}
	walk = func(script *syntax.Script, expression bool) {
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			if !ok {
				continue
			}
			name, _ := cmd.Name(src)
			if expression {
				for _, w := range cmd.Words[1:] {
					word(w)
				}
				continue
			}
			index := -1
			switch name {
			case "version", "return":
				if len(cmd.Words) == 2 {
					index = 1
				}
			case "set":
				if len(cmd.Words) == 3 {
					index = 2
				}
			case "github.setup", "gitlab.setup":
				if len(cmd.Words) >= 4 && len(cmd.Words) <= 6 {
					index = 3
				}
			case "go.setup":
				if len(cmd.Words) >= 3 && len(cmd.Words) <= 5 {
					index = 2
				}
			// The perl5, R, and ruby PortGroups carry the version as a setup
			// argument: perl5.setup module vers ?cpandir?, R.setup domain
			// author package version ?tag_prefix? ?tag_suffix?, and
			// ruby.setup module vers ?type? ?docs? ?source? ?implementation?.
			case "perl5.setup":
				if len(cmd.Words) >= 3 && len(cmd.Words) <= 4 {
					index = 2
				}
			case "R.setup":
				if len(cmd.Words) >= 5 && len(cmd.Words) <= 7 {
					index = 4
				}
			case "ruby.setup":
				if len(cmd.Words) >= 3 && len(cmd.Words) <= 7 {
					index = 2
				}
			}
			if index >= 0 {
				word(cmd.Words[index])
			}
			body := func(index int) {
				if index < len(cmd.Words) {
					if script, ok := cmd.Words[index].BracedScript(src); ok {
						walk(script, false)
					}
				}
			}
			switch name {
			case "proc", "platform", "variant", "subport", "foreach":
				body(len(cmd.Words) - 1)
			case "if":
				i := 2
				for i < len(cmd.Words) {
					if value, _ := cmd.Words[i].Literal(src); value == "then" {
						i++
					}
					body(i)
					i++
					if i >= len(cmd.Words) {
						break
					}
					value, _ := cmd.Words[i].Literal(src)
					if value == "else" {
						body(i + 1)
						break
					}
					if value != "elseif" {
						break
					}
					i += 2
				}
			}
		}
	}
	walk(script, false)

	return result, nil
}

func (c Candidate) Replace(src []byte, value string) ([]byte, error) {
	if !Literal(value) {
		return nil, fmt.Errorf("portfile: replacement is not a safe version literal")
	}
	return text.Apply(src, []text.Edit{{Span: c.Span, New: []byte(value)}})
}

func (c Candidate) Probe() string {
	var result strings.Builder
	for _, r := range c.Value {
		switch {
		case r >= '0' && r <= '9':
			if r == '7' {
				r = '4'
			} else {
				r = '7'
			}
		case r >= 'a' && r <= 'z':
			if r == 'q' {
				r = 'w'
			} else {
				r = 'q'
			}
		case r >= 'A' && r <= 'Z':
			if r == 'Q' {
				r = 'W'
			} else {
				r = 'Q'
			}
		}
		result.WriteRune(r)
	}
	return result.String()
}

func ResetRevision(src []byte, current int) ([]byte, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("portfile: invalid Tcl syntax")
	}
	var commands []syntax.Command
	for _, item := range script.Items {
		if cmd, ok := item.(syntax.Command); ok {
			if name, _ := cmd.Name(src); name == "revision" {
				commands = append(commands, cmd)
			}
		}
	}
	if len(commands) > 1 {
		return nil, fmt.Errorf("portfile: ambiguous revision commands")
	}
	if len(commands) == 0 {
		if current != 0 {
			return nil, fmt.Errorf("portfile: nonzero revision is set outside the supported scope")
		}
		return src, nil
	}
	cmd := commands[0]
	if len(cmd.Words) != 2 {
		return nil, fmt.Errorf("portfile: revision requires one literal")
	}
	value, ok := cmd.Words[1].Literal(src)
	number, err := strconv.Atoi(value)
	if !ok || err != nil || number != current {
		return nil, fmt.Errorf("portfile: revision expression does not have a matching literal")
	}
	return text.Apply(src, []text.Edit{{Span: cmd.Words[1].Span, New: []byte("0")}})
}
