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
	Owner   string
	Value   string
	Span    text.Span
	Literal bool
}
type Group struct {
	Name   string
	Values map[string]Token
	// Kinds lists the algorithms in written order; Pairs locates each one
	// with its value, in the same order.
	Kinds []string
	Pairs []portfile.ChecksumWords
	// Owner is the first algorithm's owner, "<declaration>/<word>", and
	// identifies the group across observed contexts.
	Owner string
}

func (g Group) ID() string { return g.Owner }

// Legacy reports whether the group is rewritten as a whole when its
// archive is refreshed rather than edited value by value.
func (g Group) Legacy() bool { return portfile.LegacyChecksums(g.Kinds) }

// Span covers the group's written pairs, first algorithm through last value.
func (g Group) Span() text.Span {
	return text.Span{Start: g.Pairs[0].Kind.Start, End: g.Pairs[len(g.Pairs)-1].Value.End}
}

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
		ordinal := -1
		if err == nil {
			script, _ := syntax.Parse(src)
			index := 0
			for candidate := range script.Commands(src, func(syntax.Command) bool { return true }) {
				if candidate.Span == cmd.Span {
					ordinal = index
					break
				}
				index++
			}
		}
		tokens := make([]Token, len(declaration.Values))
		for i, value := range declaration.Values {
			tokens[i].Value = value
			if err == nil && len(cmd.Words) == len(tokens)+1 {
				word := cmd.Words[i+1]
				literal, ok := word.Literal(src)
				tokens[i].Owner = fmt.Sprintf("%d/%d", ordinal, i)
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
			if group.Owner == "" {
				group.Owner = kind.Owner
			} else if declarationOf(kind.Owner) != declarationOf(group.Owner) && portfile.LegacyChecksums(append(group.Kinds, kind.Value)) {
				return result, fmt.Errorf("%w: legacy checksum group spans declarations", portfile.ErrUnsupported)
			}
			group.Values[kind.Value] = value
			group.Kinds = append(group.Kinds, kind.Value)
			group.Pairs = append(group.Pairs, portfile.ChecksumWords{Kind: kind.Span, Value: value.Span})
			i += 2
		}
		if len(group.Pairs) == 0 {
			return result, fmt.Errorf("%w: checksum group without an algorithm", portfile.ErrUnsupported)
		}
		if named[group.Name] {
			return result, fmt.Errorf("%w: ambiguous checksum group", portfile.ErrUnsupported)
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
func algorithm(value string) bool {
	return value == "sha256" || value == "rmd160" || value == "size" || value == "md5" || value == "sha1"
}

func declarationOf(owner string) string { return owner[:strings.IndexByte(owner, '/')] }
