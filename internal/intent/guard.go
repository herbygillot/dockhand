package intent

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// The guards are the questions every intent asks of its own predicted
// delta, in the order Finish asks them. They are exported because an
// intent that must ask one out of order — before an expensive step, or
// interleaved with a judgment only it can make — should ask the same
// question rather than write a fourth copy of it.

// SubportsUnchanged refuses a prediction in which evaluation contexts
// appear or disappear. No intent in the catalogue creates or destroys a
// subport, so either the edits did far more than they were asked to or
// the Portfile's structure is not what the planner read — and both are
// beyond what a prediction can honestly promise.
func SubportsUnchanged(predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &plan.Decline{Type: plan.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}
	return nil
}

// OnlyFields refuses a prediction that moves a field outside the set
// the intent is allowed to move. The set is the intent's own: the same
// field can be required by one intent and forbidden to another, which
// is why this takes the set rather than knowing one.
//
// When more than one context or field is out of bounds, the least by
// (subport, variants, field) is the one named. Which one is reported
// does not change the decision, but a message that varies between runs
// on identical input is a message nobody can test against, and the
// order chosen here is the one a plan already renders contexts in.
func OnlyFields(predicted info.Delta, may map[info.Field]bool) error {
	var (
		found bool
		at    info.SubportKey
		field info.Field
	)
	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if may[ch.Field] {
				continue
			}
			// Changes within a context arrive in canonical field order,
			// so the first offender in one is that context's least.
			if !found || keyLess(key, at) {
				found, at, field = true, key, ch.Field
			}
			break
		}
	}
	if !found {
		return nil
	}
	return &plan.Decline{Type: plan.UnexpectedChange,
		Detail: fmt.Sprintf("%s: %s", at.Subport, field)}
}

// ViaSetIsolated refuses a prediction in which any context but the
// named one moved, and exists for edits whose justification is one
// context's evaluation.
//
// A checksum located in a set variable stands outside any checksums
// command, so the corroboration that placed it is a single subport's
// evaluation — and two subports can record an identical value. The
// total shadow is the proof the aliasing hazard demands: with such an
// edit in play, no context but the edited one may move at all.
//
// Which sibling gets named when several moved is info.Delta's choice
// and varies between runs. The refusal does not.
func ViaSetIsolated(predicted info.Delta, contextName string) error {
	if key, moved := predicted.OtherContext(contextName); moved {
		return &plan.Decline{Type: plan.UnexpectedChange,
			Detail: fmt.Sprintf("a checksum edit landed in a set variable, and %s moved with it; the carrier is ambiguous", key.Subport)}
	}
	return nil
}

// OwnChanges collects what moved in the named context, across every
// variant frame the delta holds for it.
//
// It matches on the subport name and ignores the variant set, which is
// the only predicate that is correct. Every key in a snapshot carries
// the handle it was taken through, so within one delta the frame is
// constant and says nothing — while indexing the map by a key built
// with the zero frame silently misses every context the moment a
// planner runs under a non-default one, and reports that the edit
// reached nothing.
func OwnChanges(predicted info.Delta, contextName string) []info.FieldChange {
	keys := make([]info.SubportKey, 0, len(predicted.Changed))
	for key := range predicted.Changed {
		if key.Subport == contextName {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b info.SubportKey) int {
		switch {
		case keyLess(a, b):
			return -1
		case keyLess(b, a):
			return 1
		}
		return 0
	})
	var out []info.FieldChange
	for _, key := range keys {
		out = append(out, predicted.Changed[key]...)
	}
	return out
}

// keyLess orders evaluation contexts the way a plan renders them:
// subport first, then variant frame.
func keyLess(a, b info.SubportKey) bool {
	if a.Subport != b.Subport {
		return a.Subport < b.Subport
	}
	return a.Variants < b.Variants
}

// checksumTypes is base's own list, verbatim from
// portchecksum.tcl: `variable checksum_types [list md5 sha1 rmd160
// sha256 size]`. It is what tells a filename token from a digest token
// when a checksums list is read back as the alternation it is.
var checksumTypes = map[string]bool{
	"md5": true, "sha1": true, "rmd160": true, "sha256": true, "size": true,
}

// ChecksumGroup is one distfile's entry in a checksums list: the file it
// names, and the digests recorded for it. A list that names no file —
// the single-distfile shape, `checksums rmd160 x sha256 y size z` — is
// one group with an empty File.
type ChecksumGroup struct {
	File    string
	Digests map[string]string
}

// ChecksumGroups reads a checksums list back into the groups it is. The
// record keeps the declared list's raw shape — "type/value alternation,
// possibly distfile-keyed" — and this is the consumer that shape was
// waiting for.
//
// A token that is not a digest type opens a new group and names its
// file; a digest type consumes the token after it. Nothing here rejects
// a malformed list: a caller comparing two lists wants whatever
// structure is there, and a list that parses oddly compares oddly rather
// than refusing on its own.
func ChecksumGroups(tokens []string) []ChecksumGroup {
	var out []ChecksumGroup
	cur := ChecksumGroup{Digests: map[string]string{}}
	started := false
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if checksumTypes[t] {
			if i+1 < len(tokens) {
				cur.Digests[t] = tokens[i+1]
				i++
			}
			started = true
			continue
		}
		if started {
			out = append(out, cur)
		}
		cur = ChecksumGroup{File: t, Digests: map[string]string{}}
		started = true
	}
	if started {
		out = append(out, cur)
	}
	return out
}

// ChecksumsFollowTheirFiles refuses a prediction in which a distfile's
// name moved and its digests did not.
//
// A checksums list names the file each digest describes. When a version
// edit changes what those files are called and the digests stay where
// they were, the Portfile now says that the NEW file hashes to the OLD
// file's sum — which is a port that fails to fetch, for everyone the
// stale entry serves.
//
// IT HAPPENS BECAUSE ONE EVALUATION FETCHES ONE SET OF FILES. A port
// whose checksums cover several architectures declares them all and
// retrieves only the host's, so a bump re-derives the sums it could
// measure and renames the rest. Measured on terraform: bumping
// terraform-1.16 to 1.16.2 rewrote the arm64 digests, renamed
// terraform_1.16.0_darwin_amd64.zip to _1.16.2_, and left 1.16.0's
// sha256 sitting under it.
//
// The comparison is by POSITION and not by name, because the name is the
// thing that changed. A list whose group count moved is a change this
// cannot read, and it says nothing rather than guessing which group
// became which.
func ChecksumsFollowTheirFiles(predicted info.Delta, contextName string) error {
	for _, ch := range OwnChanges(predicted, contextName) {
		if ch.Field != info.FieldChecksums {
			continue
		}
		before, after := ChecksumGroups(ch.Old), ChecksumGroups(ch.New)
		if len(before) != len(after) {
			continue
		}
		for i := range before {
			b, a := before[i], after[i]
			if b.File == a.File || a.File == "" {
				continue
			}
			moved := false
			for t, v := range a.Digests {
				if b.Digests[t] != v {
					moved = true
					break
				}
			}
			if !moved {
				return &plan.Decline{Type: plan.ChecksumsStale,
					Detail: fmt.Sprintf("%s was renamed from %s and kept its digests; this evaluation fetched %s and not it",
						a.File, b.File, thisOne(after, i))}
			}
		}
	}
	return nil
}

// thisOne names a file whose digests DID move, so the refusal can say
// which one was measured beside the one that was not.
func thisOne(groups []ChecksumGroup, skip int) string {
	for i, g := range groups {
		if i != skip && g.File != "" {
			return g.File
		}
	}
	return "another file in the same list"
}

// ChecksumsAllRewritten refuses an edit set that rewrote some of a
// context's checksums commands and not all of them.
//
// A PORTFILE MAY CARRY SEVERAL CHECKSUMS COMMANDS IN BRANCHES ONLY ONE
// OF WHICH RUNS, and an evaluation sees only the one it took. gh is the
// shape: `if {${os.major} >= 17}` builds from source and checksums a
// tarball, `else` fetches a prebuilt binary and checksums that. A bump
// on a modern host rewrites the source sums, renames the binary through
// ${version}, and leaves the binary's digests describing the release
// before — so the port fails its checksum on every system that takes the
// other branch.
//
// IT CANNOT BE SEEN IN A PREDICTED DELTA, which is why this reads the
// SOURCE and not the evaluation. The untaken branch contributes nothing
// to the values a shadow evaluation reports; there is no renamed file
// and no unmoved digest to compare, because the whole command is absent.
// The parser sees it and the evaluator does not.
//
// It says nothing when only one command exists — there is nothing to
// miss — and nothing when the edit set rewrote no checksums at all,
// which is a revision bump or any other intent that does not touch them.
// A checksum carried in a `set` and edited there lands in no command and
// is left to ViaSetIsolated, whose question that is.
func ChecksumsAllRewritten(src []byte, tree *syntax.Script, contextName string, edits []edit.Edit) error {
	var blocks []text.Span
	for cmd := range tree.Commands(src, portstyle.ScopeOf(src, contextName)) {
		if name, ok := cmd.Name(src); ok && (name == "checksums" || name == "checksums-append") {
			blocks = append(blocks, cmd.Span)
		}
	}
	if len(blocks) < 2 {
		return nil
	}
	inside := func(e edit.Edit, b text.Span) bool {
		return e.Start >= b.Start && e.End <= b.End
	}
	rewritten := make([]bool, len(blocks))
	any := false
	for _, e := range edits {
		if e.Kind != edit.Checksum {
			continue
		}
		for i, b := range blocks {
			if inside(e, b) {
				rewritten[i], any = true, true
			}
		}
	}
	if !any {
		return nil
	}
	for i, ok := range rewritten {
		if !ok {
			return &plan.Decline{Type: plan.ChecksumsUnreached,
				Detail: fmt.Sprintf("%d checksums commands in this port, and the one at line %d was not reached by this evaluation",
					len(blocks), lineOf(src, blocks[i].Start))}
		}
	}
	return nil
}

// lineOf is a byte offset as the line number a person can go and look
// at. A refusal that names a byte is a refusal nobody can act on.
func lineOf(src []byte, at int) int {
	if at > len(src) {
		at = len(src)
	}
	return 1 + bytes.Count(src[:at], []byte{'\n'})
}
