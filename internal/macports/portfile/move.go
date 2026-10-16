package portfile

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// MoveGitHub points each github.setup naming a GitHub repository, from,
// as owner/project, at another, to: its first two words, where each is a
// bare literal, as `github.setup Byron gitoxide 0.45.0 v` becomes
// `github.setup GitoxideLabs gitoxide 0.45.0 v`. It reports how many it
// moved, and refuses one naming from in words it can't edit in place.
func MoveGitHub(src []byte, from, to string) ([]byte, int, error) {
	fromOwner, fromProject, ok := strings.Cut(from, "/")
	toOwner, toProject, ok2 := strings.Cut(to, "/")
	if !ok || !ok2 || !Literal(toOwner) || !Literal(toProject) {
		return nil, 0, fmt.Errorf("portfile: %q to %q is not one GitHub repository to another", from, to)
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, 0, fmt.Errorf("portfile: invalid Tcl syntax")
	}
	var edits []textedit.Edit
	moved := 0
	var refused error
	var walk func(*syntax.Script)
	walk = func(script *syntax.Script) {
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			if !ok {
				continue
			}
			if name, _ := cmd.Name(src); name == "github.setup" && len(cmd.Words) >= 3 {
				owner, ownerLiteral := cmd.Words[1].Literal(src)
				project, projectLiteral := cmd.Words[2].Literal(src)
				switch {
				case ownerLiteral && projectLiteral && strings.EqualFold(owner, fromOwner) && strings.EqualFold(project, fromProject):
					moved++
					if owner != toOwner {
						edits = append(edits, textedit.Edit{Span: cmd.Words[1].Span, New: []byte(toOwner)})
					}
					if project != toProject {
						edits = append(edits, textedit.Edit{Span: cmd.Words[2].Span, New: []byte(toProject)})
					}
				case !ownerLiteral || !projectLiteral:
					refused = fmt.Errorf("portfile: a github.setup names its repository by substitution, which dockhand doesn't edit; point it at %s yourself", to)
				}
			}
			for _, body := range bodies(src, cmd) {
				walk(body)
			}
		}
	}
	walk(script)
	if refused != nil && len(edits) == 0 {
		return nil, 0, refused
	}
	if len(edits) == 0 {
		return src, 0, nil
	}
	out, err := textedit.Apply(src, edits)
	if err != nil {
		return nil, 0, err
	}
	return out, moved, nil
}
