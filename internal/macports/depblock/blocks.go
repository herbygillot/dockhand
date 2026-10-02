package depblock

import (
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
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
	// which is the declaration of each name that ran, by its place among
	// the Portfile's declarations of it (declaration).
	which map[string]int
}

// Declared is the crates or Go modules a port's Portfile declares, as
// MacPorts evaluated the port (Inspect), or nil for a port with none; a
// declaration MacPorts couldn't evaluate is an error.
func Declared(src []byte, info macports.PortInfo) (*Plan, error) {
	for _, key := range []string{Go, Cargo, CargoGit} {
		if info.OptionErrors[key] != "" {
			return nil, fmt.Errorf("dependency: cannot evaluate %s", key)
		}
	}
	return Inspect(src, info.Options)
}

func Inspect(src []byte, options map[string]string) (*Plan, error) {
	commands, err := declarations(src)
	if err != nil {
		return nil, err
	}
	explicitGo := len(commands[Go]) > 0
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
	values, which := map[string][]string{}, map[string]int{}
	for _, name := range names(kind) {
		expected, errs := syntax.ListValues(options[name])
		if len(errs) > 0 {
			return nil, fmt.Errorf("dependency: invalid evaluated %s", name)
		}
		i, err := declaration(src, name, commands[name], expected)
		if err != nil {
			return nil, err
		}
		if i < 0 && len(expected) > 0 {
			return nil, fmt.Errorf("dependency: %s requires one explicit declaration", name)
		}
		var actual []string
		if i >= 0 {
			which[name] = i
			actual, err = literalWords(src, commands[name][i])
			if err != nil {
				return nil, err
			}
		}
		if !slices.Equal(actual, expected) {
			return nil, fmt.Errorf("dependency: calculated or overridden %s requires manual preparation", name)
		}
		values[name] = expected
	}
	plan := &Plan{Kind: kind, Values: values, source: slices.Clone(src), which: which}
	if kind == Cargo {
		plan.Git = gitPolicy(options, len(commands[CargoGit]) > 0)
	}
	return plan, nil
}
func names(kind string) []string {
	if kind == Go {
		return []string{Go}
	}
	return []string{Cargo, CargoGit}
}

// declarations are each Go or Cargo declaration MacPorts runs, at the
// Portfile's top or in a body it runs, by name, in the order they're
// written. Only the top was read: cargo's Portfile declares cargo.crates
// in each branch of a platform's if, and update refused it as declaring
// none (the rust and cargo run, batch 24).
func declarations(src []byte) (map[string][]syntax.Command, error) {
	found, ok := portfile.RunCommands(src)
	if !ok {
		return nil, fmt.Errorf("dependency: invalid Tcl syntax")
	}
	result := map[string][]syntax.Command{}
	top := map[string]bool{}
	for _, cmd := range found {
		switch name, _ := cmd.Name(src); name {
		case Go, Cargo, CargoGit:
			// Two at the top both run, the later replacing the earlier,
			// which no edit of one can say.
			if !cmd.Nested && top[name] {
				return nil, fmt.Errorf("dependency: duplicate %s declarations", name)
			}
			top[name] = top[name] || !cmd.Nested
			result[name] = append(result[name], cmd.Command)
		}
	}
	return result, nil
}

// declaration is which of a name's declarations ran, by its place among
// them: the only one, or of several, the one whose literal list is what
// MacPorts evaluated, as the branch of cargo's Portfile for current
// systems is, beside the frozen list for older ones, so an edit is made
// where the version it moves is; -1 where there's none. Several that are
// each what was evaluated, or none of them, can't say which ran.
func declaration(src []byte, name string, candidates []syntax.Command, expected []string) (int, error) {
	switch len(candidates) {
	case 0:
		return -1, nil
	case 1:
		return 0, nil
	}
	ran := -1
	for i, cmd := range candidates {
		words, err := literalWords(src, cmd)
		if err != nil || !slices.Equal(words, expected) {
			continue
		}
		if ran >= 0 {
			return -1, fmt.Errorf("dependency: %s is declared %d times, and more than one is what MacPorts evaluated, so which ran can't be told", name, len(candidates))
		}
		ran = i
	}
	if ran < 0 {
		return -1, fmt.Errorf("dependency: %s is declared %d times, and none is what MacPorts evaluated", name, len(candidates))
	}
	return ran, nil
}

// pick is the declaration of a name an edit makes: the one which names,
// or the only one; several with none named can't say which.
func pick(commands map[string][]syntax.Command, which map[string]int, name string) (syntax.Command, bool, error) {
	candidates := commands[name]
	if i, ok := which[name]; ok {
		if i >= len(candidates) {
			return syntax.Command{}, false, fmt.Errorf("dependency: the Portfile no longer has the %s declaration the plan was made from", name)
		}
		return candidates[i], true, nil
	}
	switch len(candidates) {
	case 0:
		return syntax.Command{}, false, nil
	case 1:
		return candidates[0], true, nil
	}
	return syntax.Command{}, false, fmt.Errorf("dependency: %s is declared %d times; which to edit isn't known", name, len(candidates))
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
	values, ok := cmd.LiteralArgs(src)
	for _, value := range values {
		ok = ok && safeToken(value)
	}
	if !ok {
		return nil, fmt.Errorf("dependency: declaration contains expressions or unsupported tokens")
	}
	return values, nil
}
func (p *Plan) Strip(src []byte) ([]byte, error) {
	replacements := map[string][]string{}
	for _, name := range names(p.Kind) {
		replacements[name] = nil
	}
	return apply(src, replacements, nil, p.which)
}

// ApplyPlain is Apply for the declarations the plan was made from, where
// the Portfile has several of a name.
func (p *Plan) ApplyPlain(src []byte, values map[string][]string) ([]byte, error) {
	return apply(src, values, nil, p.which)
}

// Apply retains the original spelling of dependency blocks whose values did
// not change, and lays out changed blocks like the original: unchanged rows
// stay byte for byte and new rows take the same columns.
func (p *Plan) Apply(src []byte, values map[string][]string) ([]byte, error) {
	original, err := declarations(p.source)
	if err != nil {
		return nil, err
	}
	layouts := map[string]*blockLayout{}
	for _, name := range []string{Go, Cargo, CargoGit} {
		if cmd, found, err := pick(original, p.which, name); err != nil {
			return nil, err
		} else if found {
			layouts[name] = inferLayout(name, cmd.Span.Text(p.source))
		}
	}
	out, err := apply(src, values, layouts, p.which)
	if err != nil {
		return nil, err
	}
	updated, err := declarations(out)
	if err != nil {
		return nil, err
	}
	var edits []textedit.Edit
	for name, tokens := range values {
		before, existed, err := pick(original, p.which, name)
		if err != nil {
			return nil, err
		}
		after, present, err := pick(updated, p.which, name)
		if err != nil {
			return nil, err
		}
		if existed && present && Equivalent(name, p.Values[name], tokens) {
			edits = append(edits, textedit.Edit{Span: after.Span, New: []byte(before.Span.Text(p.source))})
		}
	}
	return textedit.Apply(out, edits)
}

// Apply writes plain single-space rows; Plan.Apply follows an existing layout.
func Apply(src []byte, values map[string][]string) ([]byte, error) {
	return apply(src, values, nil, nil)
}

func apply(src []byte, values map[string][]string, layouts map[string]*blockLayout, which map[string]int) ([]byte, error) {
	commands, err := declarations(src)
	if err != nil {
		return nil, err
	}
	var edits []textedit.Edit
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
			if layout := layouts[name]; layout != nil {
				groups, err := tokenRows(name, tokens)
				if err != nil {
					return nil, err
				}
				body = layout.format(groups)
			}
		}
		cmd, found, err := pick(commands, which, name)
		if err != nil {
			return nil, err
		}
		if found {
			edits = append(edits, textedit.Edit{Span: cmd.Span, New: []byte(body)})
		} else if len(tokens) > 0 {
			added = append(added, []byte("\n"+body+"\n")...)
		}
	}
	out, err := textedit.Apply(src, edits)
	if err != nil {
		return nil, err
	}
	return append(out, added...), nil
}
func generated(src []byte, name string) ([]string, error) {
	commands, err := declarations(src)
	if err != nil {
		return nil, err
	}
	command, ok, err := pick(commands, nil, name)
	if err != nil || !ok {
		return nil, err
	}
	return literalWords(src, command)
}

// tokenRows groups a block's tokens into declaration rows.
func tokenRows(kind string, tokens []string) ([][]string, error) {
	if kind == Go {
		return goRows(tokens)
	}
	width := 3
	if kind == CargoGit {
		width = 5
	}
	if len(tokens)%width != 0 {
		return nil, fmt.Errorf("dependency: incomplete %s row", kind)
	}
	var groups [][]string
	for i := 0; i < len(tokens); i += width {
		groups = append(groups, tokens[i:i+width])
	}
	return groups, nil
}

func formattedRows(kind string, tokens []string) ([]string, error) {
	groups, err := tokenRows(kind, tokens)
	if err != nil {
		return nil, err
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

// Entries counts a regenerated block's entries, the modules or crates it
// declares, and among them those that aren't an entry of the old block as
// written: a crate at a new version, or a new one. An entry is the block's
// own row, a Go module with its version and checksums, as Equivalent
// compares them.
func Entries(kind string, old, next []string) (count, changed int, err error) {
	rows := func(values []string) (map[string]bool, int, error) {
		groups, err := tokenRows(kind, values)
		if err != nil {
			return nil, 0, err
		}
		keys := map[string]bool{}
		for _, row := range groups {
			if kind == Go {
				pairs := []string{}
				for i := 1; i+1 < len(row); i += 2 {
					pairs = append(pairs, row[i]+" "+row[i+1])
				}
				slices.Sort(pairs)
				row = append([]string{row[0]}, pairs...)
			}
			keys[strings.Join(row, " ")] = true
		}
		return keys, len(groups), nil
	}
	before, _, err := rows(old)
	if err != nil {
		return 0, 0, err
	}
	after, count, err := rows(next)
	if err != nil {
		return 0, 0, err
	}
	for key := range after {
		if !before[key] {
			changed++
		}
	}
	return count, changed, nil
}
