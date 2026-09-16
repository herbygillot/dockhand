package distfiles

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"net/url"
	"slices"
	"strings"
)

type Token struct {
	Value   string
	Span    text.Span
	Literal bool
}
type Group struct {
	Name   string
	Values map[string]Token
}

func (g Group) Key() text.Span { return g.Values["sha256"].Span }

type Artifact struct {
	macports.Distfile
	Group Group
}
type Binding struct {
	Tokens    []Token
	Groups    []Group
	Artifacts []Artifact
}

func Bind(src []byte, path string, info macports.PortInfo, observed macports.PortObservation) (Binding, error) {
	var result Binding
	if len(observed.Problems) > 0 {
		return result, fmt.Errorf("%w: %v", portfile.ErrUnsupported, observed.Problems)
	}
	for _, declaration := range observed.Declarations {
		name := strings.TrimPrefix(declaration.Command, "::")
		if name != "checksums" && name != "checksums-append" && name != "checksums-prepend" {
			continue
		}
		cmd, err := portfile.LocateDeclaration(src, path, declaration)
		tokens := make([]Token, len(declaration.Values))
		for i, value := range declaration.Values {
			tokens[i].Value = value
			if err == nil && len(cmd.Words) == len(tokens)+1 {
				word := cmd.Words[i+1]
				literal, ok := word.Literal(src)
				tokens[i].Span = word.Span
				tokens[i].Literal = ok && literal == value && !word.Expand
			}
		}
		switch name {
		case "checksums":
			result.Tokens = tokens
		case "checksums-append":
			result.Tokens = append(result.Tokens, tokens...)
		case "checksums-prepend":
			result.Tokens = append(tokens, result.Tokens...)
		}
	}
	actual, errs := syntax.ListValues(info.Options["checksums"])
	var values []string
	for _, token := range result.Tokens {
		values = append(values, token.Value)
	}
	if len(errs) > 0 || !slices.Equal(actual, values) {
		return result, fmt.Errorf("%w: checksum observations differ from evaluated values", portfile.ErrUnsupported)
	}
	named := map[string]bool{}
	for i := 0; i < len(result.Tokens); {
		group := Group{Values: map[string]Token{}}
		if !algorithm(result.Tokens[i].Value) {
			group.Name = result.Tokens[i].Value
			i++
		}
		for i < len(result.Tokens) && algorithm(result.Tokens[i].Value) {
			kind := result.Tokens[i]
			if !kind.Literal || i+1 >= len(result.Tokens) {
				return result, fmt.Errorf("%w: calculated checksum algorithm", portfile.ErrUnsupported)
			}
			value := result.Tokens[i+1]
			if !value.Literal {
				return result, fmt.Errorf("%w: checksum value has no unique literal owner", portfile.ErrUnsupported)
			}
			if _, ok := group.Values[kind.Value]; ok {
				return result, fmt.Errorf("%w: duplicate checksum algorithm", portfile.ErrUnsupported)
			}
			group.Values[kind.Value] = value
			i += 2
		}
		if _, ok := group.Values["sha256"]; !ok || named[group.Name] {
			return result, fmt.Errorf("%w: missing SHA256 or ambiguous checksum group", portfile.ErrUnsupported)
		}
		named[group.Name] = true
		result.Groups = append(result.Groups, group)
	}
	seen := map[string]bool{}
	for _, file := range observed.Distfiles {
		if seen[file.Name] {
			return result, fmt.Errorf("%w: duplicate fetch file %s", portfile.ErrUnsupported, file.Name)
		}
		seen[file.Name] = true
		var group *Group
		for i := range result.Groups {
			g := &result.Groups[i]
			if g.Name == file.Name || g.Name == "" && len(observed.Distfiles) == 1 && len(result.Groups) == 1 {
				group = g
			}
		}
		if group == nil {
			return result, fmt.Errorf("%w: no checksum declaration for %s", portfile.ErrUnsupported, file.Name)
		}
		var addresses []string
		for _, address := range file.URLs {
			u, err := url.Parse(address)
			if err == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http") {
				addresses = append(addresses, address)
			}
		}
		if len(addresses) == 0 {
			return result, fmt.Errorf("%w: no HTTP(S) fetch location for %s", portfile.ErrUnsupported, file.Name)
		}
		file.URLs = slices.Compact(addresses)
		result.Artifacts = append(result.Artifacts, Artifact{Distfile: file, Group: *group})
	}
	return result, nil
}
func algorithm(value string) bool { return value == "sha256" || value == "rmd160" || value == "size" }
