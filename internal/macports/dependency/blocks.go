package dependency

import (
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

const Go = "go.vendors"
const Cargo = "cargo.crates"
const CargoGit = "cargo.crates_github"

// ErrToolUnavailable identifies a missing optional regeneration prerequisite.
var ErrToolUnavailable = errors.New("dependency: cannot regenerate")

type Tools struct{ Go2Port, Cargo2Port string }
type Availability struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
}

func (t Tools) Probe() []Availability {
	result := []Availability{}
	for _, item := range []struct{ name, path string }{{"go2port", t.Go2Port}, {"cargo2port", t.Cargo2Port}} {
		path := item.path
		if path == "" {
			path = item.name
		}
		resolved, err := exec.LookPath(path)
		result = append(result, Availability{Name: item.name, Path: resolved, Available: err == nil})
	}
	return result
}
func (t Tools) Resolve(kind string) (string, error) {
	name, path := "cargo2port", t.Cargo2Port
	if kind == Go {
		name, path = "go2port", t.Go2Port
	}
	if path == "" {
		path = name
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("%w %s: missing executable %s; install the tool or select --%s", ErrToolUnavailable, kind, name, name)
	}
	return filepath.Abs(resolved)
}

type Plan struct {
	source []byte
	Kind   string
	Values map[string][]string
	// Git states how a Cargo port obtains Git-pinned crates; empty for Go.
	Git GitPolicy
}

func Inspect(src []byte, options map[string]string) (*Plan, error) {
	commands, err := blocks(src)
	if err != nil {
		return nil, err
	}
	_, explicitGo := commands[Go]
	kind := ""
	if options[Go] != "" || explicitGo {
		kind = Go
	}
	_, cargo := options[Cargo]
	if cargo || options[CargoGit] != "" {
		if kind != "" {
			return nil, fmt.Errorf("dependency: mixed Go and Cargo declarations require manual preparation")
		}
		kind = Cargo
	}
	if kind == "" {
		return nil, nil
	}
	values := map[string][]string{}
	for _, name := range names(kind) {
		expected, errs := syntax.ListValues(options[name])
		if len(errs) > 0 {
			return nil, fmt.Errorf("dependency: invalid evaluated %s", name)
		}
		cmd, found := commands[name]
		if !found && len(expected) > 0 {
			return nil, fmt.Errorf("dependency: %s requires one explicit declaration", name)
		}
		var actual []string
		if found {
			actual, err = literalWords(src, cmd)
			if err != nil {
				return nil, err
			}
		}
		if !slices.Equal(actual, expected) {
			return nil, fmt.Errorf("dependency: calculated or overridden %s requires manual preparation", name)
		}
		values[name] = expected
	}
	plan := &Plan{Kind: kind, Values: values, source: slices.Clone(src)}
	if kind == Cargo {
		_, declared := commands[CargoGit]
		plan.Git = gitPolicy(options, declared)
	}
	return plan, nil
}
func names(kind string) []string {
	if kind == Go {
		return []string{Go}
	}
	return []string{Cargo, CargoGit}
}
func blocks(src []byte) (map[string]syntax.Command, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("dependency: invalid Tcl syntax")
	}
	result := map[string]syntax.Command{}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		if name != Go && name != Cargo && name != CargoGit {
			continue
		}
		if _, found := result[name]; found {
			return nil, fmt.Errorf("dependency: duplicate %s declarations", name)
		}
		result[name] = cmd
	}
	return result, nil
}
func safeToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("./_+:-@", r)) {
			return false
		}
	}
	return true
}
func literalWords(src []byte, cmd syntax.Command) ([]string, error) {
	var values []string
	for _, word := range cmd.Words[1:] {
		value, ok := word.Literal(src)
		if !ok || !safeToken(value) {
			return nil, fmt.Errorf("dependency: declaration contains expressions or unsupported tokens")
		}
		values = append(values, value)
	}
	return values, nil
}
func (p *Plan) Strip(src []byte) ([]byte, error) {
	replacements := map[string][]string{}
	for _, name := range names(p.Kind) {
		replacements[name] = nil
	}
	return Apply(src, replacements)
}

// Apply retains the original spelling of dependency blocks whose values did not change.
func (p *Plan) Apply(src []byte, values map[string][]string) ([]byte, error) {
	out, err := Apply(src, values)
	if err != nil {
		return nil, err
	}
	original, err := blocks(p.source)
	if err != nil {
		return nil, err
	}
	updated, err := blocks(out)
	if err != nil {
		return nil, err
	}
	var edits []text.Edit
	for name, tokens := range values {
		before, existed := original[name]
		after, present := updated[name]
		if existed && present && Equivalent(name, p.Values[name], tokens) {
			edits = append(edits, text.Edit{Span: after.Span, New: []byte(before.Span.Text(p.source))})
		}
	}
	return text.Apply(out, edits)
}

func Apply(src []byte, values map[string][]string) ([]byte, error) {
	commands, err := blocks(src)
	if err != nil {
		return nil, err
	}
	var edits []text.Edit
	var added []byte
	for _, name := range []string{Go, Cargo, CargoGit} {
		tokens, ok := values[name]
		if !ok {
			continue
		}
		for _, token := range tokens {
			if !safeToken(token) {
				return nil, fmt.Errorf("dependency: unsafe generated token in %s", name)
			}
		}
		body := name
		if len(tokens) > 0 {
			rows, err := formattedRows(name, tokens)
			if err != nil {
				return nil, err
			}
			body += " \\\n    " + strings.Join(rows, " \\\n    ")
		}
		if cmd, found := commands[name]; found {
			edits = append(edits, text.Edit{Span: cmd.Span, New: []byte(body)})
		} else if len(tokens) > 0 {
			added = append(added, []byte("\n"+body+"\n")...)
		}
	}
	out, err := text.Apply(src, edits)
	if err != nil {
		return nil, err
	}
	return append(out, added...), nil
}
func Generated(src []byte, name string) ([]string, error) {
	commands, err := blocks(src)
	if err != nil {
		return nil, err
	}
	command, ok := commands[name]
	if !ok {
		return nil, nil
	}
	return literalWords(src, command)
}

func formattedRows(kind string, tokens []string) ([]string, error) {
	var groups [][]string
	if kind == Go {
		var err error
		groups, err = goRows(tokens)
		if err != nil {
			return nil, err
		}
	} else {
		width := 3
		if kind == CargoGit {
			width = 5
		}
		if len(tokens)%width != 0 {
			return nil, fmt.Errorf("dependency: incomplete %s row", kind)
		}
		for i := 0; i < len(tokens); i += width {
			groups = append(groups, tokens[i:i+width])
		}
	}
	rows := make([]string, len(groups))
	for i, row := range groups {
		rows[i] = strings.Join(row, " ")
	}
	return rows, nil
}

func Equivalent(kind string, a, b []string) bool {
	if kind == Go {
		ar, err := goRows(a)
		if err != nil {
			return false
		}
		br, err := goRows(b)
		if err != nil {
			return false
		}
		normalize := func(rows [][]string) map[string]string {
			m := map[string]string{}
			for _, row := range rows {
				pairs := []string{}
				for i := 1; i < len(row); i += 2 {
					pairs = append(pairs, row[i]+" "+row[i+1])
				}
				slices.Sort(pairs)
				m[row[0]] = strings.Join(pairs, " ")
			}
			return m
		}
		return len(ar) == len(br) && maps.Equal(normalize(ar), normalize(br))
	}
	width := 3
	if kind == CargoGit {
		width = 5
	}
	if len(a)%width != 0 || len(b)%width != 0 || len(a) != len(b) {
		return false
	}
	rows := func(values []string) []string {
		out := []string{}
		for i := 0; i < len(values); i += width {
			out = append(out, strings.Join(values[i:i+width], " "))
		}
		slices.Sort(out)
		return out
	}
	return slices.Equal(rows(a), rows(b))
}
