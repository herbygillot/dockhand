package bump

import (
	"fmt"
	"log/slog"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/checksums/rewrite"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// reasonDistfileName is the sentence a reader sees beside a replacement
// that renames a distfile inside a checksums block. What such a
// replacement IS travels as edit.DistfileName; this is prose and
// nothing decides from it.
//
// They are optional, and the only optional one: a block may spell its
// distfile with substitutions — ${name}-${version}${extract.suffix} is
// the common form — in which case the version edit has already renamed
// it and there is no literal to rewrite. The shadow evaluation the new
// names came from is the proof that it re-derived correctly.
const reasonDistfileName = "distfile name"

// checksumEdits computes the edits bringing a checksums block to the
// new distfiles' sums.
//
// What is bump's own, rather than checksum mechanics: the distfiles
// are renamed by the version edit, so old and new are matched
// positionally — a bump does not reorder or add distfiles, and a
// changed count means the edit did more than a bump may — and the old
// names, where the block writes them literally, are rewritten too.
func checksumEdits(src []byte, cst *syntax.Script, contextName string, old []checksums.Recorded, oldDistfiles, newDistfiles []string, sums map[string]checksums.Sums) ([]edit.Edit, bool, error) {
	if len(old) == 0 {
		return nil, false, nil
	}
	if len(oldDistfiles) != len(newDistfiles) {
		return nil, false, &plan.Decline{Type: plan.ChecksumsNotLocated,
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
		return nil, false, &plan.Decline{Type: plan.ChecksumsNotLocated, Detail: err.Error()}
	}
	seen := make(map[string]bool)
	for _, r := range old {
		if r.File == "" || seen[r.File] || renamed[r.File] == r.File {
			continue
		}
		seen[r.File] = true
		reps = append(reps, checksums.Replacement{
			Kind: edit.DistfileName,
			Old:  r.File, New: renamed[r.File], Reason: reasonDistfileName,
		})
	}

	// A VALUE WRITTEN TWICE IN ONE SCOPE HAS NO UNIQUE PLACE TO GO.
	// rewrite.Edits locates by value and takes the FIRST match, so a
	// digest appearing in two checksums commands the same scope reaches
	// sends its replacement to whichever comes first in the file —
	// correct only by luck, and silently wrong otherwise.
	//
	// Scope is most of the defence and it was measured rather than
	// assumed: a rewrite runs under ScopeOf, which reaches conditionals
	// and the context's own subport and no other, so sibling subports
	// holding identical digests never compete. What survives that is
	// small — LyX keeps two byte-identical blocks inside two branches
	// of one `if` — and for those the honest answer is to refuse rather
	// than to write somewhere plausible.
	if dup, ok := ambiguous(src, cst, portstyle.ScopeOf(src, contextName), reps); ok {
		return nil, false, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: fmt.Sprintf("%q is written in more than one checksums command in this context, so there is no one place to rewrite it", dup)}
	}

	edits, unlocated, viaSet := rewrite.Edits(src, cst, portstyle.ScopeOf(src, contextName), contextName, reps)
	for _, u := range unlocated {
		if u.Kind == edit.DistfileName {
			slog.Debug("distfile name is not a literal; the version edit renames it",
				"old", u.Old, "new", u.New)
			continue
		}
		// A checksum value that is not written literally cannot be
		// rewritten, and a bump that left an old hash in place would ship
		// a Portfile that fails to fetch.
		return nil, false, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", u.Old, u.Reason)}
	}
	return edits, viaSet, nil
}

// ambiguous reports a replacement whose old value is written more than
// once among the checksums commands a scope reaches, and so cannot be
// rewritten in a place anyone could defend.
//
// Every value a replacement carries is checked, sizes included. Two
// distfiles of identical length is not a digest collision and it is the
// same problem: the rewrite would put one file's new size where the
// other's belongs.
//
// A value written in a `set` rather than in the command has its own
// aliasing guard inside rewrite, which is where that knowledge lives;
// this is only about the commands themselves.
func ambiguous(src []byte, cst *syntax.Script, scope func(syntax.Command) bool, reps []checksums.Replacement) (string, bool) {
	seen := map[string]int{}
	for cmd := range cst.Commands(src, scope) {
		if n, ok := cmd.Name(src); !ok || (n != "checksums" && n != "checksums-append") {
			continue
		}
		for _, w := range cmd.Words[1:] {
			if lit, ok := w.Literal(src); ok {
				seen[lit]++
			}
		}
	}
	for _, r := range reps {
		if r.New != r.Old && seen[r.Old] > 1 {
			return r.Old, true
		}
	}
	return "", false
}
