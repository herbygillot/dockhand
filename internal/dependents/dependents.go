// Package dependents decides which dependents of a changed port need a
// revision bump, and in what order they can be built.
//
// It is dockhand's policy over MacPorts data, not MacPorts knowledge:
// the index says who declares a dependency, and this decides who that
// obliges. It stays at the top level rather than under darwin/ for the
// same reason abi moved there — it reasons about the MacPorts dependency
// graph and knows nothing about Mach-O. The two meet only where an ABI
// verdict becomes an input here.
package dependents

import (
	"sort"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/darwin/abi"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// The cohort proposal: which of a changed library's dependents to put
// forward for a revision bump, in the order they have to be built, and
// what was concluded about every one that is left out.
//
// It proposes and it never executes, and the difference is the whole
// character of this step. A revbump is an edit to somebody else's port,
// and the measurement behind it — an install name that moved, a
// compatibility version that went backwards — is necessary and never
// sufficient: symbols can be removed while the install name sits still,
// and a header-only break is invisible to otool. So nothing here is
// ever included on its own authority. The proposal is a list a human
// accepts by running the cohort verb or dismisses by name, and every
// port examined is recorded beside it with the reason, because a
// decision no reader can see is a decision nobody can disagree with.
//
// It is pure, like everything else in this package. The rows arrive
// from the caller that read the index; a judgment that walked a tree to
// order a cohort would need a tree to be tested, and the ordering is
// the part most worth being able to argue about at a table.
//
// Naming the index's row type is not reading it. From takes the rows a
// caller already has and folds in the two facts the index cannot carry,
// which is a translation over values and nothing else — the ruling that
// kept this package at the top level kept that translation in it, so
// the import says where a Dependent comes from instead of leaving one
// loop per caller to get it right. Nothing here opens an index, and the
// depguard rule on this package is what holds that.

// DependsBuild is the index's own field name, and it is declared here
// rather than read off portindex because of what it is FOR: the word is
// quoted into a candidate's reason — "depends_build only: it links
// nothing this change publishes" — and that sentence is the tree's
// field name as a reviewer greps for it, not a parser's constant that
// could be renamed for the parser's convenience.
//
// The two must not drift all the same, and the test beside this file
// asserts them equal. That is the same discipline darwin/abi applies to
// the baseline-source words: one declaration where the decision needs
// it, and one assertion holding it to the package that owns the
// vocabulary.
const DependsBuild = "depends_build"

// Dependent is an index row plus the two things only dockhand knows:
// whether a change for this port is already in flight, and whether the
// change being planned already carries it. That is why this is a
// separate type from portindex.Dependent rather than an alias — one is
// what the index knows, the other is what the index knows plus what
// dockhand knows.
//
// It is one port that declares a dependency on the headline, carrying
// everything a cohort decision needs to know about it.
//
// Requires is the row's own dependency targets, and it is what makes
// dependency ordering arithmetic over values rather than a second walk
// of the index from inside a function that is not allowed one. Without
// it there is no pure way to build a library before the things that
// link it, and the order is load-bearing: the guest's runner skips a
// member whose prerequisite failed and builds every other one, and it
// can only do that where prerequisites come first — a member built
// ahead of what it needs fails for a reason that has nothing to do
// with the change.
type Dependent struct {
	// Port is the dependent as the index names it, which for a subport
	// is the subport's own name.
	Port string
	// Portdir is where it lives, which for a subport is the PARENT's
	// directory — the unit a cohort stages and edits. Half the indexed
	// names in a real tree match no portdir basename anywhere, so this
	// is the index's own answer and is never derived from the name.
	Portdir string
	// Keys are the depends_* fields carrying the edge, in the index's
	// order. They are quoted into the candidate's reason, so a reviewer
	// reads why a port is in the list rather than being told that it is.
	Keys []string
	// ReplacedBy is the dependent's own replaced_by field. It is the
	// only machine-readable obsolescence marker a real PortIndex carries
	// — there is no obsolete category and no obsolete field, measured —
	// so a filter written against one of those would exclude nobody
	// while claiming to have applied a filter.
	ReplacedBy string
	// KnownFail is the dependent's own known_fail. A revbump of a port
	// the tree already expects to fail rebuilds a failure, and in a
	// cohort it stops every member behind it.
	KnownFail bool
	// Nomaintainer says the port declares the nomaintainer keyword. It
	// is an annotation and never an exclusion: an unmaintained dependent
	// still breaks, and it is the one a reviewer most needs to know
	// there is nobody to ask about.
	Nomaintainer bool
	// Conflicts is the ports this dependent declares it cannot be
	// installed beside, lowercased. It constrains which members can
	// share a guest and nothing else: a conflicting member still needs
	// its revision bumped, because it still links the library that
	// moved.
	Conflicts []string
	// Requires are the port's own dependency targets, lowercased.
	Requires []string

	// The two below are dockhand's own and come from no index. They are
	// grouped apart for that reason: everything above is a fact about
	// the tree that anybody could read, and these two are facts about
	// what this checkout is already doing.

	// InFlight names the branch already carrying a change to this port,
	// and is empty when none does. Two branches revbumping one port is
	// two revisions and a conflict at merge.
	InFlight string
	// Carried says THIS change already has the port as a subject.
	//
	// It is a different fact from InFlight and needs its own exclusion,
	// because the settlement that measures a cohort is the cohort's own
	// verification: the members are subjects of the record being
	// settled, so a second pass over the same tip re-measures, re-reads
	// the same dependents, and would propose revbumping ports this very
	// commit has already revbumped. The identity two findings are merged
	// under would usually hide that — until one dependent's exclusion
	// changes and the set no longer matches, and then a fresh proposal
	// lands on a branch whose cohort is already accepted.
	Carried bool
}

// BuildOnly reports whether the only edge to the headline is a build
// dependency. Such a port links nothing the change publishes, so a
// revbump of it rebuilds a binary that did not change — it is listed,
// with the reason, and never proposed on its own.
func (d Dependent) BuildOnly() bool {
	return len(d.Keys) == 1 && d.Keys[0] == DependsBuild
}

// Unread is one dependency field the reverse index could not read, and
// therefore one port whose edges are missing from it.
//
// It is an input to the proposal rather than a footnote about the tree
// because of what it does to the list: a cohort assembled over an index
// that dropped a field may be short by exactly these ports, and a
// proposal that put a short list forward as a complete one is a
// dependent left broken with nothing said about it — the outcome the
// whole index refuses a partial walk to avoid.
type Unread struct {
	Port    string
	Portdir string
	Field   string
}

// Instruction is a Portfile comment telling whoever updates this port
// to bump something else, with the ports it named.
//
// Quote is verbatim and Source is where it was read, because a finding
// that cannot be traced back to its words is an assertion. Ports is
// empty for the comments that name no port — "all dependents will need
// to be rev-bumped" — and an empty list is exactly the case that must
// add nobody: reading it as "every dependent" would auto-include
// hundreds of ports off one sentence.
type Instruction struct {
	Source string
	Quote  string
	Ports  []string
}

// Cohort is the proposal.
type Cohort struct {
	// Port is the headline — the port whose ABI moved.
	Port string
	// Criterion is the measurement the proposal rests on, verbatim from
	// the ABI finding, so the commit body and the pull request restate
	// the same sentence rather than two paraphrases of it.
	Criterion string
	// Members are the ports proposed for a revision bump, in dependency
	// order. Several members can share a portdir — a real tree collapses
	// gdal's 82 dependents into 39 — and collapsing them is the
	// planner's job, because the cap here counts builds and each subport
	// is one.
	Members []record.Candidate
	// Deferred are the members past the cap: a second cohort, named
	// rather than dropped. They are recorded as examined and not
	// proposed, because this proposal does not put them forward.
	Deferred []record.Candidate
	// Excluded is every port examined and left out, with why.
	Excluded []record.Candidate
	// Declined says why nothing is proposed, and is empty when something
	// is. A proposal that came back empty because the ABI check could
	// not be made and one that came back empty because no port depends
	// on the headline are different answers with different remedies.
	Declined string
}

// Proposes reports whether there is anything to put forward.
func (c Cohort) Proposes() bool { return len(c.Members) > 0 }

// Ports lists the proposed members in dependency order — the roster a
// cohort verification builds, after the headline.
func (c Cohort) Ports() []string {
	out := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		out = append(out, m.Port)
	}
	return out
}

// Candidates is every port examined, in one list: proposed first in the
// order they must be built, then those held for a second cohort, then
// those left out. It is what the finding records, and the order is the
// order a reviewer reads it in.
func (c Cohort) Candidates() []record.Candidate {
	out := make([]record.Candidate, 0, len(c.Members)+len(c.Deferred)+len(c.Excluded))
	out = append(out, c.Members...)
	out = append(out, c.Deferred...)
	out = append(out, c.Excluded...)
	return out
}

// Finding states the proposal as the note records it, and says false
// when there is nothing to propose.
//
// It is the only finding this pair of packages produces. The
// measurement itself used to state one beside it — an abi-change or
// abi-unavailable row carrying the criterion — and the durable enum has
// since closed around the three kinds a note carries, of which a bare
// measurement is not one. Nothing is lost by that: the criterion a
// reader wants is quoted here, on the proposal that rests on it, which
// is where somebody weighing the proposal is already looking.
//
// At is left unset, as everywhere here: a judgment has no clock. The
// disposition is Proposed, and it is the one finding that carries it —
// this is the question a human answers, by running the cohort verb or
// by dismissing it, and the machine gate holds publication until they
// have.
func (c Cohort) Finding() (record.Finding, bool) {
	if !c.Proposes() {
		return record.Finding{}, false
	}
	return record.Finding{
		Kind:        record.KindABIDependents,
		Ports:       c.Ports(),
		Candidates:  c.Candidates(),
		Criterion:   c.Criterion,
		Disposition: record.Proposed,
	}, true
}

// From adapts index rows into Dependents, folding in the two dockhand
// facts.
//
// It lives here because this package knows what a Dependent needs; the
// caller supplies the facts it cannot see. That is the ruling in one
// sentence — dependents stays at the top level and keeps the row
// translation — and it is why this package imports the reverse index
// deliberately rather than by accident: it says "I am built from the
// index", which is true, and it keeps one translation here instead of
// one per caller that happens to hold the two extra facts.
//
// inFlight and carried are keyed by FOLDED port name, because the tree
// is inconsistent about case — real Portfiles declare port:speexDSP
// where the port is speexdsp — and a caller that keyed them as the
// index spells them would miss exactly the rows a case difference
// hides. What is RECORDED is never folded: the index's own spelling is
// what a plan stages and what a person greps for.
//
// The unread rows are carried across unchanged and are not dependents.
// They are the fields the index could not read, and they travel with
// the rows because a proposal assembled over a partial index may be
// short by exactly those ports — a caller that took the rows alone
// would put a short list forward as a complete one.
//
// Both slices are empty rather than nil for an empty input, which
// matters to nobody here and is kept because the loops that filled them
// were written that way in the caller this moved from.
func From(rows []portindex.Dependent, unread []portindex.Unread, inFlight map[string]string, carried map[string]bool) ([]Dependent, []Unread) {
	deps := make([]Dependent, 0, len(rows))
	for _, r := range rows {
		folded := strings.ToLower(r.Name)
		deps = append(deps, Dependent{
			Port: r.Name, Portdir: r.Portdir, Keys: r.Keys,
			ReplacedBy: r.ReplacedBy, KnownFail: r.KnownFail,
			Nomaintainer: r.Nomaintainer,
			Conflicts:    r.Conflicts, Requires: r.Requires,
			InFlight: inFlight[folded],
			Carried:  carried[folded],
		})
	}
	short := make([]Unread, 0, len(unread))
	for _, u := range unread {
		short = append(short, Unread{Port: u.Port, Portdir: u.Portdir, Field: u.Field})
	}
	return deps, short
}

// Propose is the whole decision: a pure function from an ABI verdict,
// the maintainer's comments, the declared dependents and the unreadable
// ones to a cohort. No I/O, no repository, no index.
//
// It proposes the revision bumps a measured ABI change calls for.
//
// Nothing is proposed unless the measurement says something moved. That
// is the refusal, and it is written into this function rather than left
// to a caller: a cohort assembled from declarations alone is a blanket
// revbump of everything that mentions a port, which is the one thing
// this tool must never do. An unavailable check proposes nothing and
// says which unavailability it was; an unchanged one proposes nothing
// and says so, even where a Portfile comment asked for a cohort — the
// comment is a finding of its own and a human weighs it against the
// measurement.
//
// The limit is a cap on builds, not on edits, and what is past it is a
// second cohort rather than a truncation. Zero or less is no cap.
//
// unread names the dependency fields the index could not read. They are
// not dependents and are never proposed; they are recorded beside the
// exclusions so that a reader of the list can see it may be short, and
// by exactly which ports.
func Propose(a abi.Result, quotes []Instruction, deps []Dependent, unread []Unread, limit int) Cohort {
	c := Cohort{Port: a.Port, Criterion: a.Why}
	if a.Verdict != abi.Changed {
		c.Declined = declineOn(a, quotes)
		return c
	}
	if len(deps) == 0 {
		c.Declined = "no port in the index declares a dependency on " + a.Port
		return c
	}

	named := instructed(quotes)
	var proposed []Dependent
	for _, d := range deps {
		note := named[strings.ToLower(d.Port)].Note
		if why, ok := excludes(a, d, note); ok {
			c.Excluded = append(c.Excluded, record.Candidate{
				Port: d.Port, Portdir: d.Portdir, Reason: why})
			continue
		}
		proposed = append(proposed, d)
	}
	// A port a comment named that nothing in the index declares here.
	// It is recorded and never invented into the cohort: there is no
	// portdir for it, and a member with no portdir is a plan that edits
	// nothing.
	for _, n := range unmatched(named, deps) {
		c.Excluded = append(c.Excluded, record.Candidate{Port: n.Port,
			Reason: "named by the comment in " + n.Note.Source +
				", but nothing in the index declares it a dependent here — revbump it by hand"})
	}

	ordered, tangled := dependencyOrder(proposed)
	for _, d := range tangled {
		c.Excluded = append(c.Excluded, record.Candidate{Port: d.Port, Portdir: d.Portdir,
			Reason: cycleReason(tangled)})
	}
	solo := coresident(ordered)
	for _, u := range unread {
		c.Excluded = append(c.Excluded, record.Candidate{Port: u.Port, Portdir: u.Portdir,
			Reason: "its " + u.Field + " field could not be read as a list, so whether it depends on " +
				a.Port + " is unknown — check it by hand"})
	}
	sortCandidates(c.Excluded)

	for i, d := range ordered {
		cand := record.Candidate{Port: d.Port, Portdir: d.Portdir, Proposed: true,
			Reason: proposeReason(d, named[strings.ToLower(d.Port)].Note)}
		if with, cannot := solo[strings.ToLower(d.Port)]; cannot {
			// Bumped, and not built beside the member it conflicts with.
			// It stays proposed because its revision is owed either way:
			// it links a library that moved. The sibling is recorded on
			// its own key as well as in the sentence: the sentence is for
			// the person, and the name is what the engine deactivates if
			// the person overrides the withholding — read from Over, never
			// parsed back out of the prose.
			cand.Solo, cand.Over = true, with
			cand.Reason += "; conflicts with " + with + ", which this cohort builds — bumped here, and not built"
		}
		if limit > 0 && i >= limit {
			// Past the cap. It is recorded as examined and not proposed,
			// because this proposal does not put it forward — and it is
			// named, because a member silently dropped is a dependent left
			// broken with nothing said about it.
			cand.Proposed = false
			cand.Reason += "; beyond the cohort cap of " + strconv.Itoa(limit) + " — a second cohort, after this one lands"
			c.Deferred = append(c.Deferred, cand)
			continue
		}
		c.Members = append(c.Members, cand)
	}
	if !c.Proposes() {
		c.Declined = "every declared dependent of " + a.Port + " was excluded by name"
	}
	return c
}

// declineOn says why a measurement that did not find a break proposes
// nothing, and names a Portfile comment that asked for one anyway.
//
// The comment is not overruled here and it is not obeyed either. It is
// its own finding, with its own verbatim quote, and this sentence tells
// a reader that the two disagree — which is the whole of what a
// mechanical check can honestly contribute to that disagreement.
func declineOn(a abi.Result, quotes []Instruction) string {
	why := "nothing " + a.Port + " publishes moved, so no dependent needs a revision bump on this evidence"
	if a.Verdict == abi.Unavailable {
		why = "nothing is proposed: " + a.Why
	} else if un := a.Unmeasured(); len(un) > 0 {
		// A mixed reading: some libraries compared and some declined. The
		// sentence must claim exactly the first half. "Nothing moved" over
		// a comparison that skipped three files is a stronger statement
		// than what was measured, and it is the statement a reader would
		// act on.
		why = "nothing that could be compared moved, so no dependent needs a revision bump on this evidence — " +
			strconv.Itoa(len(un)) + " " + plural(len(un), "file was", "files were") +
			" not measured (" + declineList(un) + ")"
	}
	var sources []string
	for _, q := range quotes {
		if q.Source != "" {
			sources = append(sources, q.Source)
		}
	}
	if len(sources) == 0 {
		return why
	}
	sort.Strings(sources)
	return why + " — the comment in " + strings.Join(sources, ", ") +
		" asks for one anyway, and is recorded as its own finding for a human to weigh"
}

// plural picks the form for a count, so a decline reads as a sentence.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// declineList names the files a mixed reading skipped, in their own
// clauses, sorted so one measurement reads the same twice.
func declineList(un []abi.Change) string {
	parts := make([]string, 0, len(un))
	for _, c := range un {
		parts = append(parts, c.Subject)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// excludes weighs one dependent against the reasons a cohort leaves a
// port out, in the order they are worth reading: what this change
// already covers, what is not a live port, what somebody else is
// already doing, and what links nothing.
//
// All but the last are hard and a comment does not lift them. A port in
// the headline's own portdir, or already a subject of this change, is
// one this change is already editing; a replaced port, a known-failing
// one and one another branch is carrying are facts about the tree that
// naming a port in a comment does not change. Build-only is the one a
// comment does lift, because there the maintainer is saying something
// the dependency fields do not.
func excludes(a abi.Result, d Dependent, note Instruction) (string, bool) {
	switch {
	case strings.EqualFold(d.Port, a.Port):
		return "it is the port being changed", true
	case d.Portdir != "" && d.Portdir == a.Portdir:
		return "it lives in " + d.Portdir + ", the portdir this change already edits", true
	case d.Carried:
		return "this change already carries it", true
	case d.ReplacedBy != "":
		return "replaced by " + d.ReplacedBy, true
	case d.KnownFail:
		return "marked known_fail in the index — a revbump would rebuild a failure", true
	case d.InFlight != "":
		return "already in flight on " + d.InFlight, true
	case d.BuildOnly() && note.Source == "":
		return "depends_build only: it links nothing this change publishes", true
	}
	return "", false
}

// proposeReason says why a member is in the list, quoting the fields it
// came from and the comment that named it where one did.
func proposeReason(d Dependent, note Instruction) string {
	why := strings.Join(d.Keys, ", ")
	if why == "" {
		why = "a declared dependency"
	}
	if d.BuildOnly() {
		why = "depends_build only, and named by the comment in " + note.Source
	} else if note.Source != "" {
		why += "; named by the comment in " + note.Source
	}
	if d.Nomaintainer {
		// Better than a third of a real tree is nomaintainer, and the
		// annotation is the reviewer's warning that there is nobody to
		// ask about this one — not a reason to leave it out.
		why += "; nomaintainer"
	}
	return why
}

// namedPort is one port a comment named, in the comment's own spelling,
// with the comment that named it.
//
// The spelling is kept because a port a comment named and nothing
// declares is recorded for a human to go and revbump by hand, and the
// name in that row is what they will search the tree for. Matching is
// case-insensitive because the tree is inconsistent about it — real
// Portfiles say port:speexDSP where the port is speexdsp — but
// recording the folded key would hand a reader a name the tree does not
// spell that way.
type namedPort struct {
	Port string
	Note Instruction
}

// instructed maps every port a comment named, keyed by its folded name,
// to that port and the comment. A comment that named no port
// contributes nobody: the unnamed form — "all dependents will need to
// be rev-bumped" — is a criterion and not a roster, and reading it as
// one would auto-include every dependent there is.
func instructed(quotes []Instruction) map[string]namedPort {
	named := map[string]namedPort{}
	for _, q := range quotes {
		for _, p := range q.Ports {
			if p == "" {
				continue
			}
			if _, seen := named[strings.ToLower(p)]; !seen {
				named[strings.ToLower(p)] = namedPort{Port: p, Note: q}
			}
		}
	}
	return named
}

// unmatched lists the comment-named ports no dependent row answers to,
// in a stable order.
func unmatched(named map[string]namedPort, deps []Dependent) []namedPort {
	have := make(map[string]bool, len(deps))
	for _, d := range deps {
		have[strings.ToLower(d.Port)] = true
	}
	var out []namedPort
	for key, n := range named {
		if !have[key] {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// dependencyOrder sorts the proposed members so that a member comes
// after everything in the cohort it needs, and names the ones that
// cannot be ordered at all.
//
// The graph is the INDUCED subgraph — only edges between proposed
// members count. Ordering the whole tree's graph would be a walk of the
// index from inside a pure function, which is the thing this package is
// not allowed to do and, more to the point, the thing that would make
// this decision impossible to argue about without a checkout.
//
// Ties break by name, so two runs over one tree propose the same cohort
// in the same order and the goldens do not move under a map's
// iteration.
//
// A member in or behind a dependency cycle is declined rather than
// placed. Picking one to go first would be a guess with a build behind
// it, and the honest answer — these N cannot be ordered, do them by
// hand — is one a person can act on.
func dependencyOrder(deps []Dependent) (ordered, tangled []Dependent) {
	index := make(map[string]int, len(deps))
	for i, d := range deps {
		index[strings.ToLower(d.Port)] = i
	}
	// needs[i] is how many members i must wait for; feeds[j] is who
	// waits on j.
	needs := make([]int, len(deps))
	feeds := make([][]int, len(deps))
	for i, d := range deps {
		for _, req := range d.Requires {
			j, ok := index[strings.ToLower(req)]
			if !ok || j == i {
				continue
			}
			needs[i]++
			feeds[j] = append(feeds[j], i)
		}
	}

	done := make([]bool, len(deps))
	for range deps {
		pick := -1
		for i := range deps {
			if done[i] || needs[i] > 0 {
				continue
			}
			if pick < 0 || deps[i].Port < deps[pick].Port {
				pick = i
			}
		}
		if pick < 0 {
			break
		}
		done[pick] = true
		ordered = append(ordered, deps[pick])
		for _, next := range feeds[pick] {
			needs[next]--
		}
	}
	for i, d := range deps {
		if !done[i] {
			tangled = append(tangled, d)
		}
	}
	sort.Slice(tangled, func(i, j int) bool { return tangled[i].Port < tangled[j].Port })
	return ordered, tangled
}

// cycleReason names the whole knot rather than one edge of it. A member
// behind a cycle is as unorderable as one inside it, and telling a
// reader which ports to look at together is what makes the remedy
// actionable.
func cycleReason(tangled []Dependent) string {
	names := make([]string, 0, len(tangled))
	for _, d := range tangled {
		names = append(names, d.Port)
	}
	sort.Strings(names)
	return "cannot be ordered: it is in or behind a dependency cycle among " +
		strings.Join(names, ", ") + " — revbump these by hand"
}

// sortCandidates puts the examined-and-excluded rows in one stable
// order, by port, so the note reads the same twice.
func sortCandidates(all []record.Candidate) {
	sort.SliceStable(all, func(i, j int) bool { return all[i].Port < all[j].Port })
}

// coresident names the members that cannot be installed beside a
// member already in the build, mapping each to the one it lost to.
//
// MacPorts refuses to activate two ports that declare a conflict, so a
// cohort holding both would spend a guest discovering that the second
// will not install — and skip every member that depends on it, for a
// failure that was never the change's. Measured live: gegl and
// gegl-devel, then libheif and libheif-devel one candidate later. Two of
// the two cohorts examined carried such a pair, so this is the ordinary
// case and not a corner.
//
// Nothing is removed from the change. A named member is still proposed,
// still planned and still committed with its revision bumped — a
// dependent that links a library which moved needs rebuilding whether
// or not one guest could hold it beside its sibling. What it loses is a
// seat in THIS build, and it is told so by name.
//
// Which one keeps the seat, when two conflict: the one whose name does
// not end in -devel, and otherwise whichever is already earlier in
// build order. The suffix is a maintainer's ruling and it settles the
// case that actually arrives — a stable port and its development twin.
// It decides about a fifth of the tree's conflict pairs on its own;
// build order carries the rest, which keeps the outcome from depending
// on which member a map happened to yield first.
func coresident(deps []Dependent) map[string]string {
	solo := map[string]string{}
	claimed := make(map[string]string, len(deps))
	seated := make([]string, 0, len(deps))
	for _, d := range deps {
		lower := strings.ToLower(d.Port)
		if with, taken := conflictWith(d, claimed); taken {
			if !preferred(d.Port, with) {
				solo[lower] = with
				continue
			}
			// This member takes the seat; the one holding it gives it up.
			solo[strings.ToLower(with)] = d.Port
			for name, holder := range claimed {
				if strings.EqualFold(holder, with) {
					delete(claimed, name)
				}
			}
			for i, s := range seated {
				if strings.EqualFold(s, with) {
					seated = append(seated[:i], seated[i+1:]...)
					break
				}
			}
		}
		delete(solo, lower)
		seated = append(seated, d.Port)
		claimed[lower] = d.Port
		for _, c := range d.Conflicts {
			claimed[c] = d.Port
		}
	}
	return solo
}

// conflictWith names the member already in the build that this one
// cannot join, reading the declaration from either side: a conflict is
// symmetric in MacPorts and both halves are usually written, but a
// cohort must not depend on both being.
func conflictWith(d Dependent, claimed map[string]string) (string, bool) {
	if with, ok := claimed[strings.ToLower(d.Port)]; ok && !strings.EqualFold(with, d.Port) {
		return with, true
	}
	for _, c := range d.Conflicts {
		if with, ok := claimed[c]; ok && !strings.EqualFold(with, d.Port) {
			return with, true
		}
	}
	return "", false
}

// preferred says whether candidate should take the seat from holder.
//
// A -devel port is the development twin of the port it conflicts with,
// so the stable one is what the tree is standing on and the one worth
// the guest. Where the suffix does not tell them apart, the holder
// keeps the seat, which leaves build order deciding.
func preferred(candidate, holder string) bool {
	return isDevel(holder) && !isDevel(candidate)
}

// isDevel reports the -devel suffix, case-insensitively.
func isDevel(port string) bool {
	return len(port) > len(develSuffix) && strings.EqualFold(port[len(port)-len(develSuffix):], develSuffix)
}

const develSuffix = "-devel"
