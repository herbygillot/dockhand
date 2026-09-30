package project

import (
	"fmt"
	"strings"
)

// A PEP 508 environment marker, the condition after a requirement's ";"
// that says where it applies: "sys_platform == 'win32'", "python_version <
// '3.10' and os_name == 'posix'". It's read as the port's own build would
// see it on macOS, where what's known of that environment settles it, and
// where it isn't, as for a machine or an extra, it's unknown, never a
// guess (the helper-ownership review's finding 1).

// Applies is what a marker says of an environment: it applies, it
// doesn't, or it can't be told.
type Applies int

const (
	Unknown Applies = iota
	Yes
	No
)

// MacOS is the environment a MacPorts build is: Darwin, POSIX, CPython,
// with the Python version the port uses where it's known ("3.13").
func MacOS(pythonVersion string) map[string]string {
	environment := map[string]string{
		"os_name": "posix", "sys_platform": "darwin", "platform_system": "Darwin",
		"implementation_name": "cpython", "platform_python_implementation": "CPython",
	}
	if pythonVersion != "" {
		environment["python_version"] = pythonVersion
		environment["python_full_version"] = pythonVersion
	}
	return environment
}

// Evaluate reads a marker in an environment. A marker it can't parse is
// an error; a variable the environment doesn't have makes what depends
// on it unknown.
func Evaluate(marker string, environment map[string]string) (Applies, error) {
	tokens, err := markerTokens(marker)
	if err != nil {
		return Unknown, err
	}
	p := markerParser{tokens: tokens, environment: environment}
	applies, err := p.or()
	if err == nil && p.at < len(p.tokens) {
		err = fmt.Errorf("%q: unexpected %q", marker, p.tokens[p.at])
	}
	if err != nil {
		return Unknown, err
	}
	return applies, nil
}

// markerVariables are the names PEP 508 defines.
var markerVariables = map[string]bool{
	"python_version": true, "python_full_version": true, "os_name": true, "sys_platform": true, "platform_release": true,
	"platform_system": true, "platform_version": true, "platform_machine": true, "platform_python_implementation": true,
	"implementation_name": true, "implementation_version": true, "extra": true,
}

// markerTokens splits a marker into quoted strings (kept with their
// quote), names, operators, and parentheses.
func markerTokens(marker string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(marker); {
		c := marker[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')':
			tokens = append(tokens, string(c))
			i++
		case c == '\'' || c == '"':
			end := strings.IndexByte(marker[i+1:], c)
			if end < 0 {
				return nil, fmt.Errorf("%q: an unclosed string", marker)
			}
			tokens = append(tokens, marker[i:i+end+2])
			i += end + 2
		case strings.ContainsRune("=!<>~", rune(c)):
			j := i
			for j < len(marker) && strings.ContainsRune("=!<>~", rune(marker[j])) {
				j++
			}
			tokens = append(tokens, marker[i:j])
			i = j
		default:
			j := i
			for j < len(marker) && (marker[j] == '_' || marker[j] == '.' || marker[j] >= 'a' && marker[j] <= 'z' || marker[j] >= 'A' && marker[j] <= 'Z' || marker[j] >= '0' && marker[j] <= '9') {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("%q: unexpected %q", marker, c)
			}
			tokens = append(tokens, marker[i:j])
			i = j
		}
	}
	return tokens, nil
}

type markerParser struct {
	tokens      []string
	at          int
	environment map[string]string
}

func (p *markerParser) peek() string {
	if p.at < len(p.tokens) {
		return p.tokens[p.at]
	}
	return ""
}

// or and and are three-valued: a known answer settles them where it can,
// "no or unknown" is unknown, and "yes or unknown" is yes.
func (p *markerParser) or() (Applies, error) {
	left, err := p.and()
	for err == nil && p.peek() == "or" {
		p.at++
		var right Applies
		if right, err = p.and(); err == nil {
			switch {
			case left == Yes || right == Yes:
				left = Yes
			case left == Unknown || right == Unknown:
				left = Unknown
			default:
				left = No
			}
		}
	}
	return left, err
}

func (p *markerParser) and() (Applies, error) {
	left, err := p.expression()
	for err == nil && p.peek() == "and" {
		p.at++
		var right Applies
		if right, err = p.expression(); err == nil {
			switch {
			case left == No || right == No:
				left = No
			case left == Unknown || right == Unknown:
				left = Unknown
			default:
				left = Yes
			}
		}
	}
	return left, err
}

func (p *markerParser) expression() (Applies, error) {
	if p.peek() == "(" {
		p.at++
		inner, err := p.or()
		if err != nil {
			return Unknown, err
		}
		if p.peek() != ")" {
			return Unknown, fmt.Errorf("a ( without its )")
		}
		p.at++
		return inner, nil
	}
	left, leftKnown, err := p.value()
	if err != nil {
		return Unknown, err
	}
	operator := p.peek()
	p.at++
	if operator == "not" {
		if p.peek() != "in" {
			return Unknown, fmt.Errorf("not without in")
		}
		p.at++
		operator = "not in"
	}
	right, rightKnown, err := p.value()
	if err != nil {
		return Unknown, err
	}
	if !leftKnown || !rightKnown {
		return Unknown, nil
	}
	return compareMarker(left, operator, right)
}

// value is a quoted string, or a variable's value in the environment, and
// whether it's known there.
func (p *markerParser) value() (string, bool, error) {
	token := p.peek()
	p.at++
	switch {
	case token == "":
		return "", false, fmt.Errorf("a comparison without its value")
	case token[0] == '\'' || token[0] == '"':
		return token[1 : len(token)-1], true, nil
	case markerVariables[token]:
		value, known := p.environment[token]
		return value, known, nil
	}
	return "", false, fmt.Errorf("%q isn't a marker variable", token)
}

// compareMarker compares as PEP 508 does: versions as PEP 440 orders them
// where both sides are versions, otherwise strings, and in as a
// substring.
func compareMarker(left, operator, right string) (Applies, error) {
	answer := func(b bool) Applies {
		if b {
			return Yes
		}
		return No
	}
	switch operator {
	case "in":
		return answer(strings.Contains(right, left)), nil
	case "not in":
		return answer(!strings.Contains(right, left)), nil
	}
	if _, err := parseVersion(left); err == nil {
		if admits, err := Admits(operator+right, left); err == nil {
			return answer(admits), nil
		}
	}
	switch operator {
	case "==", "===":
		return answer(left == right), nil
	case "!=":
		return answer(left != right), nil
	case "<", "<=", ">", ">=", "~=":
		// Not versions: ordered as strings, which PEP 508 allows, but
		// nothing a Portfile relies on.
		return Unknown, nil
	}
	return Unknown, fmt.Errorf("%q isn't a marker comparison", operator)
}
