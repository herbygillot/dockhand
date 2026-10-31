package portfile

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// GitHubSetups are the repositories the Portfile's github.setup lines name
// in literal words, as owner/project, in the order written, at any depth,
// as `github.setup Byron gitoxide 0.45.0 v` names Byron/gitoxide; and the
// golang PortGroup's go.setup lines naming a github.com package, whose
// owner and project it sets up with github.setup, as
// `go.setup github.com/nektos/act 0.2.92 v` names nektos/act. It reads
// the text, not an evaluation: a line naming its repository by
// substitution, as `github.setup ${author} gitoxide`, is left out, and a
// line in a variant may never run. It's a guess at what an evaluation
// reads, for reading ahead, never what discovery decides by. None where
// the Portfile doesn't parse.
func GitHubSetups(src []byte) []string {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var names []string
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		switch name, _ := cmd.Name(src); {
		case name == "github.setup" && len(cmd.Words) >= 3:
			owner, ownerLiteral := cmd.Words[1].Literal(src)
			project, projectLiteral := cmd.Words[2].Literal(src)
			if ownerLiteral && projectLiteral && owner != "" && project != "" {
				names = append(names, owner+"/"+project)
			}
		case name == "go.setup" && len(cmd.Words) >= 2:
			// The PortGroup splits the package at its slashes: the host,
			// the owner, the project, and a directory within it.
			pkg, literal := cmd.Words[1].Literal(src)
			parts := strings.Split(pkg, "/")
			if literal && len(parts) >= 3 && parts[0] == "github.com" && parts[1] != "" && parts[2] != "" {
				names = append(names, parts[1]+"/"+parts[2])
			}
		}
	})
	return names
}
