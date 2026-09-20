package portfile

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// UniqueLiteral finds the one place the Portfile writes value as a whole
// token, or as a run of adjacent tokens such as the words "revision 3", at
// any depth: a command's words, a table held in a set, a subport's
// declarations, a quoted string, a command substitution. Comments do not
// count. Two occurrences, or none, mean the value has no owner that can be
// edited with confidence.
func UniqueLiteral(src []byte, value string) (text.Span, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || strings.TrimSpace(value) == "" {
		return text.Span{}, false
	}
	var found []text.Span
	for _, run := range script.TokenRuns(src) {
		for i := range run {
			for j := i; j < len(run); j++ {
				candidate := text.Span{Start: run[i].Start, End: run[j].End}
				if candidate.Len() > len(value) {
					break
				}
				if candidate.Text(src) == value {
					found = append(found, candidate)
					break
				}
			}
		}
	}
	if len(found) != 1 {
		return text.Span{}, false
	}
	return found[0], true
}
