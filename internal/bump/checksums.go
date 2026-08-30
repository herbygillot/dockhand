package bump

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/checksums/rewrite"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// checksumEdits computes the edits bringing a checksums block to the
// new distfiles' sums.
//
// What is bump's own, rather than checksum mechanics: the distfiles
// are renamed by the version edit, so old and new are matched
// positionally — a bump does not reorder or add distfiles, and a
// changed count means the edit did more than a bump may — and the old
// names, where the block writes them literally, are rewritten too.
func checksumEdits(src []byte, cst *syntax.Script, contextName string, old []checksums.Recorded, oldDistfiles, newDistfiles []string, sums map[string]checksums.Sums) ([]plan.Edit, error) {
	if len(old) == 0 {
		return nil, nil
	}
	if len(oldDistfiles) != len(newDistfiles) {
		return nil, &intent.Decline{Type: intent.ChecksumsNotLocated,
			Detail: fmt.Sprintf("distfile count changed: %d before, %d after", len(oldDistfiles), len(newDistfiles))}
	}
	// The block records the OLD names, so the fetched sums are keyed by
	// the name each record will be found under.
	renamed := make(map[string]string, len(oldDistfiles))
	byRecordedName := make(map[string]checksums.Sums, len(oldDistfiles))
	for i, name := range oldDistfiles {
		renamed[name] = newDistfiles[i]
		if s, ok := sums[newDistfiles[i]]; ok {
			byRecordedName[name] = s
		}
	}

	reps, err := checksums.Replacements(old, byRecordedName)
	if err != nil {
		return nil, &intent.Decline{Type: intent.ChecksumsNotLocated, Detail: err.Error()}
	}
	seen := make(map[string]bool)
	for _, r := range old {
		if r.File == "" || seen[r.File] || renamed[r.File] == r.File {
			continue
		}
		seen[r.File] = true
		reps = append(reps, checksums.Replacement{
			Old: r.File, New: renamed[r.File], Reason: "distfile name",
		})
	}

	edits, unlocated := rewrite.Edits(src, cst, portstyle.ScopeOf(src, contextName), reps)
	if len(unlocated) > 0 {
		u := unlocated[0]
		return nil, &intent.Decline{Type: intent.ChecksumsNotLocated,
			Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", u.Old, u.Reason)}
	}
	return edits, nil
}
