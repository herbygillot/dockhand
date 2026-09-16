package portfile

import "github.com/herbygillot/dockhand/internal/tcl/syntax"

// CandidateInSelection excludes literals owned by a different declared subport.
// The evaluator still has to establish that the remaining literal controls the version.
func CandidateInSelection(src []byte, candidate Candidate, selected string) bool {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return false
	}
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		name, _ := cmd.Name(src)
		if name != "subport" || len(cmd.Words) != 3 {
			continue
		}
		body := cmd.Words[2].Span
		if candidate.Span.Start >= body.Start && candidate.Span.End <= body.End {
			subport, literal := cmd.Words[1].Literal(src)
			if literal && subport != selected {
				return false
			}
		}
	}
	return true
}
