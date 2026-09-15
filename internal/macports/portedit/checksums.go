package portedit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

type checksumPair struct {
	kind  string
	value syntax.Word
}
type checksumGroup struct {
	name  string
	pairs []checksumPair
}

func checksumKind(s string) bool { return s == "rmd160" || s == "sha256" || s == "size" }

func checksumGroups(src []byte, evaluated string) ([]checksumGroup, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid checksum syntax", ErrUnsupported)
	}
	var commands []syntax.Command
	for _, item := range script.Items {
		if cmd, ok := item.(syntax.Command); ok {
			if name, _ := cmd.Name(src); name == "checksums" {
				commands = append(commands, cmd)
			}
		}
	}
	if len(commands) != 1 {
		return nil, fmt.Errorf("%w: expected one checksums command", ErrUnsupported)
	}
	words := commands[0].Words[1:]
	values, errs := syntax.ListValues(evaluated)
	if len(errs) > 0 || len(words) == 0 || len(words) != len(values) {
		return nil, fmt.Errorf("%w: checksum source does not match evaluation", ErrUnsupported)
	}
	var groups []checksumGroup
	names := map[string]bool{}
	for i := 0; i < len(words); {
		group := checksumGroup{}
		if !checksumKind(values[i]) {
			group.name = values[i]
			literal, ok := words[i].Literal(src)
			if group.name == "" || words[i].Expand || (ok && literal != group.name) || names[group.name] {
				return nil, fmt.Errorf("%w: ambiguous checksum filename", ErrUnsupported)
			}
			names[group.name] = true
			i++
		} else if len(groups) > 0 {
			return nil, fmt.Errorf("%w: mixed named and unnamed checksums", ErrUnsupported)
		}
		seen := map[string]bool{}
		for i < len(words) && checksumKind(values[i]) {
			kind, ok := words[i].Literal(src)
			if !ok || kind != values[i] || seen[kind] || i+1 >= len(words) {
				return nil, fmt.Errorf("%w: ambiguous checksum algorithm", ErrUnsupported)
			}
			value, ok := words[i+1].Literal(src)
			if !ok || words[i+1].Expand || value != values[i+1] {
				return nil, fmt.Errorf("%w: calculated or overridden checksums", ErrUnsupported)
			}
			seen[kind] = true
			group.pairs = append(group.pairs, checksumPair{kind, words[i+1]})
			i += 2
		}
		if !seen["sha256"] {
			return nil, fmt.Errorf("%w: each distfile requires a sha256 checksum", ErrUnsupported)
		}
		groups = append(groups, group)
		if group.name == "" && i < len(words) {
			return nil, fmt.Errorf("%w: mixed named and unnamed checksums", ErrUnsupported)
		}
	}
	return groups, nil
}

func replaceChecksums(src []byte, evaluated string, downloads ...Download) ([]byte, string, error) {
	groups, err := checksumGroups(src, evaluated)
	if err != nil {
		return nil, "", err
	}
	byName := map[string]Download{}
	for _, d := range downloads {
		if _, ok := byName[d.Name]; ok {
			return nil, "", fmt.Errorf("%w: duplicate distfile %s", ErrUnsupported, d.Name)
		}
		byName[d.Name] = d
	}
	if len(groups) != len(downloads) {
		return nil, "", fmt.Errorf("%w: checksums must cover every source distfile exactly once", ErrUnsupported)
	}
	var edits []text.Edit
	var expected []string
	for _, group := range groups {
		download, ok := byName[group.name]
		if group.name == "" && len(downloads) == 1 {
			download, ok = downloads[0], true
		}
		if !ok {
			return nil, "", fmt.Errorf("%w: no source distfile for checksums %s", ErrUnsupported, group.name)
		}
		sums := map[string]string{"rmd160": download.RMD160, "sha256": download.SHA256, "size": strconv.FormatInt(download.Size, 10)}
		if group.name != "" {
			expected = append(expected, group.name)
		}
		for _, pair := range group.pairs {
			edits = append(edits, text.Edit{Span: pair.value.Span, New: []byte(sums[pair.kind])})
			expected = append(expected, pair.kind, sums[pair.kind])
		}
	}
	out, err := text.Apply(src, edits)
	return out, strings.Join(expected, " "), err
}
