package intent

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// replacement is one literal that must be rewritten: an old checksum
// value, or an old distfile name embedded in the checksums block.
type replacement struct {
	old     string
	new     string
	reason  string
	located bool
}

// checksumEdits computes the edits bringing a checksums block to the
// new distfiles' sums. Old values are located as literal words of
// checksums commands within the context's scopes — the same
// corroboration principle as version location: an edit target must
// equal the evaluated value it claims to carry. Old distfile names
// written literally in the block are renamed to their successors,
// matched positionally — the block's structure does not change across
// a version bump, only its values.
func checksumEdits(src []byte, cst *syntax.Script, contextName string, old []checksums.Recorded, oldDistfiles, newDistfiles []string, sums map[string]checksums.Sums) ([]plan.Edit, error) {
	if len(old) == 0 {
		return nil, nil
	}
	if len(oldDistfiles) != len(newDistfiles) {
		return nil, &Decline{Type: ChecksumsNotLocated,
			Detail: fmt.Sprintf("distfile count changed: %d before, %d after", len(oldDistfiles), len(newDistfiles))}
	}
	fileMap := make(map[string]string, len(oldDistfiles))
	for i, o := range oldDistfiles {
		fileMap[o] = newDistfiles[i]
	}

	var reps []replacement
	seenFile := make(map[string]bool)
	for _, r := range old {
		if checksums.IsLegacyType(r.Type) {
			return nil, &Decline{Type: ChecksumsNotLocated,
				Detail: fmt.Sprintf("legacy checksum type %s cannot be recomputed", r.Type)}
		}
		s, err := sumsFor(r.File, fileMap, newDistfiles, sums)
		if err != nil {
			return nil, err
		}
		newValue, ok := s.Value(r.Type)
		if !ok {
			return nil, &Decline{Type: ChecksumsNotLocated,
				Detail: fmt.Sprintf("unknown checksum type %s", r.Type)}
		}
		reps = append(reps, replacement{old: r.Value, new: newValue, reason: "checksum " + r.Type})
		if r.File != "" && !seenFile[r.File] && fileMap[r.File] != r.File {
			seenFile[r.File] = true
			reps = append(reps, replacement{old: r.File, new: fileMap[r.File], reason: "distfile name"})
		}
	}

	// Locate each replacement as a literal word of a checksums command,
	// in document order, first unclaimed match wins.
	var edits []plan.Edit
	for cmd := range cst.Commands(src, portstyle.ScopeOf(src, contextName)) {
		name, ok := cmd.Name(src)
		if !ok || (name != "checksums" && name != "checksums-append") {
			continue
		}
		for _, w := range cmd.Words[1:] {
			lit, ok := w.Literal(src)
			if !ok {
				continue
			}
			for i := range reps {
				if !reps[i].located && reps[i].old == lit {
					reps[i].located = true
					edits = append(edits, plan.Edit{
						Start:  w.Span.Start,
						End:    w.Span.End,
						Old:    lit,
						New:    reps[i].new,
						Reason: reps[i].reason,
					})
					break
				}
			}
		}
	}
	for _, r := range reps {
		if !r.located {
			return nil, &Decline{Type: ChecksumsNotLocated,
				Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", r.old, r.reason)}
		}
	}
	return edits, nil
}

// sumsFor resolves which fetched distfile a recorded triple describes:
// its named file mapped to its successor, or the sole distfile when
// unnamed.
func sumsFor(file string, fileMap map[string]string, newDistfiles []string, sums map[string]checksums.Sums) (checksums.Sums, error) {
	name := ""
	switch {
	case file != "":
		mapped, ok := fileMap[file]
		if !ok {
			return checksums.Sums{}, &Decline{Type: ChecksumsNotLocated,
				Detail: fmt.Sprintf("checksums name %s but the port fetches no such distfile", file)}
		}
		name = mapped
	case len(newDistfiles) == 1:
		name = newDistfiles[0]
	default:
		return checksums.Sums{}, &Decline{Type: ChecksumsNotLocated,
			Detail: fmt.Sprintf("unnamed checksums with %d distfiles", len(newDistfiles))}
	}
	s, ok := sums[name]
	if !ok {
		return checksums.Sums{}, &Decline{Type: ChecksumsNotLocated,
			Detail: fmt.Sprintf("no fetched sums for %s", name)}
	}
	return s, nil
}
