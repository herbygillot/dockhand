package project

import (
	"fmt"
	"strings"
)

// A Cargo target table's key says where its dependencies apply: a target
// triple, "x86_64-pc-windows-gnu", or a cfg() expression, "cfg(windows)",
// "cfg(all(unix, not(target_os = \"macos\")))" (The Cargo Book,
// "Platform specific dependencies"; the Rust Reference, "Conditional
// compilation"). It's read as a MacPorts build would see it on macOS, on
// either of the architectures a Mac has: where what's known of macOS
// settles it, on both, it applies or doesn't, and where an architecture
// or a name it can't know of, as a feature, decides, it's unknown, never
// a guess.

// macArchitectures are the architectures a Mac builds for, by Rust's
// names.
var macArchitectures = []string{"aarch64", "x86_64"}

// macCfg are the cfg options macOS sets, by name, each with its values;
// one with no values is set bare, as unix is. target_arch is the
// architecture's own.
var macCfg = map[string][]string{
	"unix": nil, "target_os": {"macos"}, "target_family": {"unix"}, "target_vendor": {"apple"},
	"target_env": {""}, "target_abi": {""}, "target_endian": {"little"}, "target_pointer_width": {"64"},
}

// knownCfg are the names whose every value macOS settles: one of them it
// doesn't set is false, as windows is. Any other name, as feature or
// debug_assertions, is unknown.
var knownCfg = map[string]bool{
	"unix": true, "windows": true, "target_os": true, "target_family": true, "target_vendor": true, "target_env": true,
	"target_abi": true, "target_endian": true, "target_pointer_width": true, "target_arch": true,
}

// CargoTargetOnMacOS says whether a Cargo target table's dependencies
// apply to a macOS build. A key it can't parse is an error.
func CargoTargetOnMacOS(key string) (Applies, error) {
	var each []Applies
	for _, architecture := range macArchitectures {
		applies, err := cargoTargetOn(key, architecture)
		if err != nil {
			return Unknown, err
		}
		each = append(each, applies)
	}
	if each[0] == each[1] {
		return each[0], nil
	}
	return Unknown, nil
}

// cargoTargetOn reads a target key on macOS for one architecture.
func cargoTargetOn(key, architecture string) (Applies, error) {
	key = strings.TrimSpace(key)
	inner, ok := strings.CutPrefix(key, "cfg(")
	if !ok {
		// A triple: arch-vendor-os, macOS's being apple-darwin.
		arch, rest, _ := strings.Cut(key, "-")
		switch {
		case !strings.HasSuffix(rest, "apple-darwin"):
			return No, nil
		case arch == architecture || arch == "arm64" && architecture == "aarch64":
			return Yes, nil
		}
		return No, nil
	}
	inner, ok = strings.CutSuffix(inner, ")")
	if !ok {
		return Unknown, fmt.Errorf("%q: an unclosed cfg(", key)
	}
	tokens, err := cfgTokens(inner)
	if err != nil {
		return Unknown, fmt.Errorf("%q: %w", key, err)
	}
	p := cfgParser{tokens: tokens, architecture: architecture}
	applies, err := p.predicate()
	if err == nil && p.at < len(p.tokens) {
		err = fmt.Errorf("unexpected %q", p.tokens[p.at])
	}
	if err != nil {
		return Unknown, fmt.Errorf("%q: %w", key, err)
	}
	return applies, nil
}

// cfgTokens splits a cfg predicate into names, quoted strings (kept with
// their quote), "=", ",", and parentheses.
func cfgTokens(text string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case strings.IndexByte("(),=", c) >= 0:
			tokens = append(tokens, string(c))
			i++
		case c == '"':
			end := strings.IndexByte(text[i+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("an unclosed string")
			}
			tokens = append(tokens, text[i:i+end+2])
			i += end + 2
		default:
			j := i
			for j < len(text) && (text[j] == '_' || text[j] >= 'a' && text[j] <= 'z' || text[j] >= 'A' && text[j] <= 'Z' || text[j] >= '0' && text[j] <= '9') {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("unexpected %q", c)
			}
			tokens = append(tokens, text[i:j])
			i = j
		}
	}
	return tokens, nil
}

// cfgParser reads a cfg predicate's tokens, three-valued.
type cfgParser struct {
	tokens       []string
	at           int
	architecture string
}

func (p *cfgParser) next() string {
	if p.at >= len(p.tokens) {
		return ""
	}
	token := p.tokens[p.at]
	p.at++
	return token
}

func (p *cfgParser) peek() string {
	if p.at >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.at]
}

// predicate is an option, or all(), any(), or not() of predicates.
func (p *cfgParser) predicate() (Applies, error) {
	name := p.next()
	if name == "" || strings.IndexByte("(),=\"", name[0]) >= 0 {
		return Unknown, fmt.Errorf("a predicate expected, not %q", name)
	}
	switch {
	case p.peek() == "(" && (name == "all" || name == "any" || name == "not"):
		p.next()
		var list []Applies
		for p.peek() != ")" {
			applies, err := p.predicate()
			if err != nil {
				return Unknown, err
			}
			list = append(list, applies)
			if p.peek() == "," {
				p.next()
			} else if p.peek() != ")" {
				return Unknown, fmt.Errorf("unexpected %q", p.peek())
			}
		}
		p.next()
		return combine(name, list)
	case p.peek() == "=":
		p.next()
		value := p.next()
		if len(value) < 2 || value[0] != '"' {
			return Unknown, fmt.Errorf("%s = expects a string, not %q", name, value)
		}
		return p.option(name, value[1:len(value)-1]), nil
	}
	return p.option(name, ""), nil
}

// option is one cfg option on macOS: name = "value", or bare where value
// is empty.
func (p *cfgParser) option(name, value string) Applies {
	if !knownCfg[name] {
		return Unknown
	}
	if name == "target_arch" {
		if value == p.architecture {
			return Yes
		}
		return No
	}
	values, set := macCfg[name]
	switch {
	case !set:
		return No
	case values == nil:
		// A bare option, as unix is; unix = "x" isn't one.
		if value == "" {
			return Yes
		}
		return No
	}
	for _, have := range values {
		if have == value {
			return Yes
		}
	}
	return No
}

// combine is all(), any(), or not() over what its predicates say.
func combine(operator string, list []Applies) (Applies, error) {
	switch operator {
	case "not":
		if len(list) != 1 {
			return Unknown, fmt.Errorf("not() takes one predicate, not %d", len(list))
		}
		switch list[0] {
		case Yes:
			return No, nil
		case No:
			return Yes, nil
		}
		return Unknown, nil
	case "all":
		result := Yes
		for _, applies := range list {
			if applies == No {
				return No, nil
			}
			if applies == Unknown {
				result = Unknown
			}
		}
		return result, nil
	}
	result := No
	for _, applies := range list {
		if applies == Yes {
			return Yes, nil
		}
		if applies == Unknown {
			result = Unknown
		}
	}
	return result, nil
}
