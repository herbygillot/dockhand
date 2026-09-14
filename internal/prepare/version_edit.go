package prepare

import (
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/v2/internal/text"
)

func literalVersion(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}

func versionEdits(src []byte, current, next string, revision int) ([]byte, error) {
	if !literalVersion(next) || current == next {
		return nil, fmt.Errorf("%w: requested version is unchanged or is not a supported literal", ErrUnsupported)
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid Tcl syntax", ErrUnsupported)
	}
	var versions, setups, revisions []syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		switch name {
		case "fetch", "pre-fetch", "post-fetch":
			return nil, fmt.Errorf("%w: custom fetch commands require a dedicated preparer", ErrUnsupported)
		case "version":
			versions = append(versions, cmd)
		case "github.setup", "gitlab.setup", "go.setup":
			setups = append(setups, cmd)
		case "revision":
			revisions = append(revisions, cmd)
		}
	}
	if len(versions) > 1 || len(setups) > 1 || len(revisions) > 1 {
		return nil, fmt.Errorf("%w: ambiguous version, setup, or revision commands", ErrUnsupported)
	}
	var value syntax.Word
	switch {
	case len(versions) == 1:
		if len(versions[0].Words) != 2 {
			return nil, fmt.Errorf("%w: version requires one argument", ErrUnsupported)
		}
		value = versions[0].Words[1]
		if len(setups) == 1 {
			argument, err := setupVersion(src, setups[0])
			if err != nil {
				return nil, err
			}
			if argument.Span.Text(src) != "$version" && argument.Span.Text(src) != "${version}" {
				return nil, fmt.Errorf("%w: setup must use the literal version declaration", ErrUnsupported)
			}
		}
	case len(setups) == 1:
		var err error
		value, err = setupVersion(src, setups[0])
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: no supported version declaration", ErrUnsupported)
	}
	actual, ok := value.Literal(src)
	if !ok || actual != current {
		return nil, fmt.Errorf("%w: version expression does not have a matching literal", ErrUnsupported)
	}
	edits := []text.Edit{{Span: value.Span, New: []byte(next)}}
	if len(revisions) == 1 {
		cmd := revisions[0]
		if len(cmd.Words) != 2 {
			return nil, fmt.Errorf("%w: revision requires one literal", ErrUnsupported)
		}
		value, ok := cmd.Words[1].Literal(src)
		n, err := strconv.Atoi(value)
		if !ok || err != nil || n != revision {
			return nil, fmt.Errorf("%w: revision expression does not have a matching literal", ErrUnsupported)
		}
		edits = append(edits, text.Edit{Span: cmd.Words[1].Span, New: []byte("0")})
	} else if revision != 0 {
		return nil, fmt.Errorf("%w: nonzero revision is set outside the supported scope", ErrUnsupported)
	}
	return text.Apply(src, edits)
}

func setupVersion(src []byte, command syntax.Command) (syntax.Word, error) {
	name, _ := command.Name(src)
	index := 3
	if name == "go.setup" {
		index = 2
	}
	if len(command.Words) < index+1 || len(command.Words) > index+3 {
		return syntax.Word{}, fmt.Errorf("%w: %s arguments", ErrUnsupported, name)
	}
	return command.Words[index], nil
}
