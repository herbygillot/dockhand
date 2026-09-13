package prepare

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/v2/internal/text"
)

func checksumWords(src []byte, evaluated string) ([]syntax.Word, error) {
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
		return nil, fmt.Errorf("%w: expected one literal checksums command", ErrUnsupported)
	}
	words := commands[0].Words[1:]
	if len(words) == 0 || len(words)%2 != 0 {
		return nil, fmt.Errorf("%w: only unnamed checksums for one distfile are supported", ErrUnsupported)
	}
	values := []string{}
	seen := map[string]bool{}
	for i, word := range words {
		value, ok := word.Literal(src)
		if !ok {
			return nil, fmt.Errorf("%w: calculated checksums are not supported", ErrUnsupported)
		}
		if i%2 == 0 {
			if value != "rmd160" && value != "sha256" && value != "size" || seen[value] {
				return nil, fmt.Errorf("%w: unsupported or duplicate checksum %s", ErrUnsupported, value)
			}
			seen[value] = true
		}
		values = append(values, value)
	}
	if !seen["sha256"] {
		return nil, fmt.Errorf("%w: a sha256 checksum is required", ErrUnsupported)
	}
	evaluatedValues, errs := syntax.ListValues(evaluated)
	if len(errs) > 0 || !slices.Equal(values, evaluatedValues) {
		return nil, fmt.Errorf("%w: checksum source does not match evaluation", ErrUnsupported)
	}
	return words, nil
}

func replaceChecksums(src []byte, evaluated string, download Download) ([]byte, string, error) {
	words, err := checksumWords(src, evaluated)
	if err != nil {
		return nil, "", err
	}
	sums := map[string]string{"rmd160": download.RMD160, "sha256": download.SHA256, "size": strconv.FormatInt(download.Size, 10)}
	var edits []text.Edit
	var expected []string
	for i := 0; i < len(words); i += 2 {
		kind, _ := words[i].Literal(src)
		edits = append(edits, text.Edit{Span: words[i+1].Span, New: []byte(sums[kind])})
		expected = append(expected, kind, sums[kind])
	}
	out, err := text.Apply(src, edits)
	return out, strings.Join(expected, " "), err
}
