package portfile

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// Unrewritable is a setup naming the moved repository in words dockhand
// doesn't edit in place: its Command, and where it would point it, To, as
// that command writes it.
type Unrewritable struct{ Command, To string }

func (u *Unrewritable) Error() string {
	what := "repository"
	if u.Command == "go.setup" {
		what = "module path"
	}
	return fmt.Sprintf("Dockhand can't rewrite this %s %s because it uses Tcl substitution. Set it to %s by hand", u.Command, what, u.To)
}

// SetupCommand is the command that names a GitHub repository, as
// owner/project, in a Portfile: go.setup where a go.setup's module path
// is on github.com and names it, else github.setup, which every message
// about the repository names (#167).
func SetupCommand(src []byte, repository string) string {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return "github.setup"
	}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, _ := cmd.Name(src); name == "go.setup" && len(cmd.Words) >= 2 {
			if module, literal := cmd.Words[1].Literal(src); !literal || goRepository(module, repository) {
				return "go.setup"
			}
		}
	}
	return "github.setup"
}

// goRepository reports whether a Go module path is a GitHub repository's,
// owner/project, or a package within it.
func goRepository(module, repository string) bool {
	rest, ok := strings.CutPrefix(module, "github.com/")
	if !ok {
		return false
	}
	parts := strings.SplitN(rest, "/", 3)
	return len(parts) >= 2 && strings.EqualFold(parts[0]+"/"+parts[1], repository)
}

// MoveGitHub points each github.setup naming a GitHub repository, from,
// as owner/project, at another, to: its first two words, where each is a
// bare literal, as `github.setup Byron gitoxide 0.45.0 v` becomes
// `github.setup GitoxideLabs gitoxide 0.45.0 v`; and each go.setup whose
// literal module path is the repository's, its owner and project, any
// package path after them kept, as `go.setup github.com/Tufin/oasdiff`
// becomes `go.setup github.com/oasdiff/oasdiff` (#167). It reports how many
// it moved, and refuses one naming from in words it can't edit in place
// (Unrewritable).
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
					refused = &Unrewritable{Command: "github.setup", To: to}
				}
			}
			if name, _ := cmd.Name(src); name == "go.setup" && len(cmd.Words) >= 2 {
				module, literal := cmd.Words[1].Literal(src)
				switch {
				case literal && goRepository(module, from):
					moved++
					parts := strings.SplitN(module, "/", 4)
					parts[1], parts[2] = toOwner, toProject
					if next := strings.Join(parts, "/"); next != module {
						edits = append(edits, textedit.Edit{Span: cmd.Words[1].Span, New: []byte(next)})
					}
				case !literal:
					refused = &Unrewritable{Command: "go.setup", To: "github.com/" + to}
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
