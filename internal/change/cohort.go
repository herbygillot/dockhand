package change

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// excludedReason is the candidate reason an excluded member carries. It
// is matched as well as written, so it is one constant.
const excludedReason = "excluded by --exclude: not bumped by this change"

// withheldTail is how a measurement words a member it bumped and left
// out of the guest. Forcing one rewrites the tail rather than the whole
// sentence, because the base of it — the depends_* fields, the conflict
// it names — is still true.
const withheldTail = " — bumped here, and not built"

// Cohort is the pure amendment Accept performs over a Proposed cohort
// finding's candidates: --exclude removes, --force-withheld seats a
// withheld member LAST with the sibling it deactivates named on the
// candidate. Five declines, all sentinels, all exit 10. It reads the
// finding off record.Change.Findings — the one Proposed KindABIDependents
// finding — and returns the candidates AnswerIn will record as Accepted,
// which run.Roster later reads back to seat the guest identically on
// this try, on cycle's drain and on a later verify.
//
// PURE, over the record and two lists of names, which is what makes the
// whole amendment testable with no tree and no index. The shipped
// forcedConflict asked the ports tree's reverse index whether two forced
// members conflict with EACH OTHER; that question needs a tree, and the
// half of it this function can answer without one is the half the
// sentinel actually names — two forced members that deactivate each
// other, which the candidates themselves record in Over. A pair that
// conflicts by an edge neither candidate names is a fact about the tree,
// and the road that holds a tree asks it there.
//
// Matching is case-folded, deliberately: a person naming "GEGL-devel"
// means the port, and a verb that seated nothing because the case
// differed would have taken the instruction and ignored it. The
// candidate's own spelling is what is recorded.
func Cohort(c record.Change, exclude, force []string) ([]record.Candidate, error) {
	f, ok := proposal(c)
	if !ok {
		return nil, ErrNoProposal
	}
	cands, err := excludeMembers(f.Candidates, exclude)
	if err != nil {
		return nil, err
	}
	return forceMembers(cands, force)
}

// proposal is the one Proposed cohort finding a change carries. A
// finding already answered is not a proposal — an answer is given once —
// and a change with none is ErrNoProposal, which is what a second
// `accept` over an accepted cohort meets.
func proposal(c record.Change) (record.Finding, bool) {
	for _, f := range c.Findings {
		if f.Kind == record.KindABIDependents && f.Disposition == record.Proposed {
			return f, true
		}
	}
	return record.Finding{}, false
}

// excludeMembers takes the named ports out of a proposal, returning the
// candidates as the commit should record them.
//
// A name that matches no proposed member is an error and not a shrug. A
// person excluding "imagemagick" from a proposal holding "ImageMagick"
// means to exclude it, and a verb that silently bumped it anyway would
// have taken an instruction and done the opposite while reporting
// success.
//
// Excluding everything is its own refusal: a cohort with no members is
// not a smaller cohort, it is no commit at all, and a person who meant
// to drop the whole proposal has `dismiss` for it.
func excludeMembers(in []record.Candidate, exclude []string) ([]record.Candidate, error) {
	out := append([]record.Candidate(nil), in...)
	if len(exclude) == 0 {
		return out, nil
	}
	want, hit := folded(exclude), map[string]bool{}
	kept := 0
	for i := range out {
		c := &out[i]
		if c.Proposed && want[strings.ToLower(c.Port)] {
			hit[strings.ToLower(c.Port)] = true
			c.Proposed, c.Solo, c.Forced, c.Reason = false, false, false, excludedReason
			continue
		}
		if c.Proposed {
			kept++
		}
	}
	if unknown := missing(want, hit); len(unknown) > 0 {
		return nil, fmt.Errorf("%w: %s — the proposal puts forward %s",
			ErrUnknownMember, strings.Join(unknown, ", "), strings.Join(named(in, isProposed), ", "))
	}
	if kept == 0 {
		return nil, ErrEmptyCohort
	}
	return out, nil
}

// forceMembers turns the named withheld members into forced ones.
//
// The override names what it overrides, so every name is checked. A name
// the proposal withholds becomes a seat and its reason is reworded from
// "bumped here, and not built" to the forced sentence, so the commit
// body and the pull request read the seat rather than the withholding. A
// name the proposal knows but does not withhold — one it proposes to
// build outright, or one it examined and left out — is ErrNotWithheld:
// there is nothing to force, and forcing it would be a flag doing
// nothing to a name a person typed. A name the proposal does not carry
// at all is ErrUnknownMember.
//
// Three refusals guard the seat itself, and all three are the same
// shape: a seat that cannot be made room for is declined rather than
// half-made.
//
//   - A withheld member whose record does not say WHICH sibling it lost
//     its seat to would be seated with nothing to deactivate — built
//     beside the active sibling, and recorded as forced.
//   - One whose sibling --exclude has taken out of the change would be
//     told to deactivate a port the guest never installs, and fail by
//     construction on a build that had nothing to make room for.
//   - Two forced members that deactivate each other cannot both be
//     seated: deactivating one sibling makes room for one seat.
func forceMembers(in []record.Candidate, force []string) ([]record.Candidate, error) {
	out := append([]record.Candidate(nil), in...)
	if len(force) == 0 {
		return out, nil
	}
	want, hit := folded(force), map[string]bool{}
	excluded := map[string]bool{}
	for _, c := range out {
		if c.Reason == excludedReason {
			excluded[strings.ToLower(c.Port)] = true
		}
	}
	for i := range out {
		c := &out[i]
		lower := strings.ToLower(c.Port)
		if !want[lower] {
			continue
		}
		hit[lower] = true
		switch {
		case !isWithheld(*c):
			// Known to the proposal — proposed outright, or examined and left
			// out — but not a withheld member. There is nothing to force.
			return nil, fmt.Errorf("%w: %s", ErrNotWithheld, c.Port)
		case c.Over == "":
			return nil, fmt.Errorf("%w: %s — the record does not say which member it conflicts with, so nothing can be deactivated for it; `dockhand verify` measures the branch again",
				ErrCannotForce, c.Port)
		case excluded[strings.ToLower(c.Over)]:
			return nil, fmt.Errorf("%w: %s conflicts with %s, which --exclude leaves out of this change, so there is nothing to deactivate; drop one of the two flags",
				ErrCannotForce, c.Port, c.Over)
		}
		c.Forced = true
		c.Reason = forcedReason(*c)
	}
	if unknown := missing(want, hit); len(unknown) > 0 {
		return nil, fmt.Errorf("%w: %s — this proposal withholds %s",
			ErrUnknownMember, strings.Join(unknown, ", "), strings.Join(named(in, isWithheld), ", "))
	}
	if a, b, clash := deactivateEachOther(out); clash {
		return nil, fmt.Errorf("%w: %s and %s", ErrForcedConflict, a, b)
	}
	return out, nil
}

// deactivateEachOther finds two forced members each of which is the
// other's Over: seating either one deactivates the other, so no
// arrangement seats both. Over names the SEATED member a withheld
// candidate lost to, so this is the pair the candidates themselves
// record — the case the sentinel names, answered from the record alone.
func deactivateEachOther(cands []record.Candidate) (a, b string, clash bool) {
	over := map[string]string{}
	for _, c := range cands {
		if c.Forced {
			over[strings.ToLower(c.Port)] = strings.ToLower(c.Over)
		}
	}
	names := make([]string, 0, len(over))
	for name := range over {
		names = append(names, name)
	}
	slices.Sort(names)
	spell := map[string]string{}
	for _, c := range cands {
		spell[strings.ToLower(c.Port)] = c.Port
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			x, y := names[i], names[j]
			if over[x] == y && over[y] == x {
				return spell[x], spell[y], true
			}
		}
	}
	return "", "", false
}

// forcedReason rewords a withheld candidate's reason for the seat it is
// being given. The measurement wrote "… — bumped here, and not built";
// the seat is "… — forced into the build at the maintainer's request,
// with <sibling> deactivated first", which the commit body and the pull
// request both read.
func forcedReason(c record.Candidate) string {
	forced := " — forced into the build at the maintainer's request, with " + c.Over + " deactivated first"
	return strings.TrimSuffix(c.Reason, withheldTail) + forced
}

// folded is a person's list of names as a lookup: trimmed, case-folded,
// and without the empty strings a shell splits out of an empty flag.
func folded(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			out[strings.ToLower(name)] = true
		}
	}
	return out
}

// missing is the names a person gave that nothing answered to, sorted so
// a refusal reads the same twice.
func missing(want, hit map[string]bool) []string {
	var out []string
	for name := range want {
		if !hit[name] {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// named lists the candidates a predicate picks out, for a refusal that
// has to say what the person could have named instead.
func named(cands []record.Candidate, pick func(record.Candidate) bool) []string {
	var out []string
	for _, c := range cands {
		if pick(c) {
			out = append(out, c.Port)
		}
	}
	slices.Sort(out)
	return out
}

func isProposed(c record.Candidate) bool { return c.Proposed }

// isWithheld is the withheld member: proposed — it IS bumped by the
// change, and must keep its revision — and Solo, so it is left out of
// the cohort's own guest. The pair is the whole definition, and
// --force-withheld's names are measured against it.
func isWithheld(c record.Candidate) bool { return c.Proposed && c.Solo }
