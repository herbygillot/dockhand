package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// The words a cohort is published in: the second commit's message, the
// pull request's cohort section, and the proposal line status prints
// under a branch that is still carrying a question.
//
// One sentence appears in all three, and it appears VERBATIM: the
// criterion the measurement produced. That is deliberate and it is the
// point of the criterion being made once, in the judgment, rather than
// reworded per audience. A commit body, a pull request and a terminal
// line that each paraphrased "install name libwidget.2.dylib →
// libwidget.3.dylib" would be three claims a reviewer has to reconcile,
// and the whole argument for a proposal is that a person can check the
// one claim behind it with otool by hand.
//
// The revbump kinds a record carries, spelled here because these
// functions read a note and record's Kind is a plain string: the
// vocabulary is the note's, and a renderer that invented its own names
// for what it found would be a second vocabulary nobody could grep.
//
// The caveat beside a measurement goes the other way and is IMPORTED
// from the judgment that made it. It is not a note field and never was:
// it is one sentence about what otool cannot see, true of every
// reading, and the whole reason it is a constant is that a commit body,
// a pull request and a terminal line must not be able to word it three
// ways.
const (
	// KindCohort is the proposal: these dependents need a revision bump.
	// It is the one finding that carries record.Proposed from a
	// measurement, and the one the cohort verb answers.
	KindCohort = "dependent-revbump"
	// KindInstruction is a Portfile comment telling whoever updates this
	// port to bump something else.
	KindInstruction = "instruction-comment"
	// KindABIChanged, KindABIUnchanged and KindABIUnavailable are the
	// three answers the measurement can give. The middle one is a real
	// result and not an absence — an up-front cohort refuted by
	// measurement rests on it, and a body that carried the cohort and
	// not the refutation would be publishing the wrong claim.
	KindABIChanged     = "abi-change"
	KindABIUnchanged   = "abi-unchanged"
	KindABIUnavailable = "abi-unavailable"
)

// CohortMember is one port a cohort revbumped, with what the
// environment said about it afterwards.
type CohortMember struct {
	Port    string
	Portdir string
	// Reason is why it is in the list, in the words the proposal used —
	// the depends_* fields the edge came from, and the comment that
	// named it where one did.
	Reason string
	// Links are the link-proof lines from this member's own run: which
	// of the files it installed bind to the library that moved.
	//
	// Nil and empty are different answers and both are printed as
	// themselves. Nil is nobody looked; empty is the sweep ran and found
	// no binding, which makes the port build-only in fact whatever its
	// depends_* fields said — and that is worth saying rather than
	// quietly dropping, because the revbump was still spent.
	Links []string
	// Unmeasured is why there is no proof, where Links is nil because
	// the member's own run never reached the sweep: the build failed, or
	// it was blocked before it was reached, or this build withheld it.
	// It is printed in the proof's place, so a member the body claims a
	// bump for is never listed with nothing beside it — the reviewer
	// reading "Revision bumped in this change" is owed either the
	// evidence or the reason there is none, on the same line.
	//
	// Empty where nobody looked for a reason that is not the member's:
	// the commit message, written before any run exists; or a
	// measurement that could not be made, which the body says once for
	// the whole change rather than under every member.
	Unmeasured string
}

// CohortDecline is a member the cohort could not plan: the shape its
// Portfile is in, and what a person does about it.
type CohortDecline struct {
	Port    string
	Portdir string
	Reason  string
}

// CohortCommit is everything the extend commit's message says.
//
// Criterion arrives already worded by the measurement and is written
// out unchanged. Limits is the caveat that travels with it — a
// mechanical criterion is necessary and never sufficient — and it is a
// field rather than a constant here because a check that could not run
// has nothing to be insufficient about, and the judgment is what knows
// which of the two this was.
type CohortCommit struct {
	// Port and Target are the headline and what this change moved it to,
	// for the subject line.
	Port, Target string
	Criterion    string
	Limits       string
	// Quotes are the instruction comments this cohort also rests on,
	// verbatim.
	Quotes []CohortQuote
	// Members are the ports the commit revbumps, in the order they must
	// be built.
	Members []CohortMember
	// Declined are the members the planner could not plan, with the
	// remedy: the cohort proceeds with the rest and names these.
	Declined []CohortDecline
	// Listed are the ports the proposal examined and did not put
	// forward, with why — build-only dependents, replaced ports, ones
	// another branch is already carrying.
	Listed []record.Candidate
}

// CohortQuote is one instruction comment as it is restated: where it
// was read, and what it actually said.
type CohortQuote struct {
	Source string
	Quote  string
}

// CohortMessage is the extend commit's message: one logical change per
// commit, so N dependents are one commit and its body says why all N
// moved.
//
// The subject is the project's own shape — a scope, a colon, an
// imperative — with the cohort as the scope, because the change is not
// about any one of the members. The body opens with the criterion
// because that is the sentence a reviewer checks, carries the caveat
// beside it so the claim is never read as more than it is, and lists
// every port with the reason it is there.
func CohortMessage(c CohortCommit) string {
	var b strings.Builder
	b.WriteString(cohortSubject(c.Port, c.Target, len(c.Members)))
	if c.Criterion != "" {
		fmt.Fprintf(&b, "\n\n%s\n", c.Criterion)
	}
	if c.Limits != "" {
		fmt.Fprintf(&b, "\n%s.\n", c.Limits)
	}
	for _, q := range c.Quotes {
		fmt.Fprintf(&b, "\nThe comment in %s asks for this:\n\n%s\n", q.Source, indent(q.Quote))
	}
	if len(c.Members) > 0 {
		b.WriteString("\nRevision bumped:\n")
		for _, m := range c.Members {
			fmt.Fprintf(&b, "  %s\n", memberLine(m))
		}
	}
	if len(c.Declined) > 0 {
		b.WriteString("\nNot bumped here — do these by hand:\n")
		for _, d := range c.Declined {
			fmt.Fprintf(&b, "  %s: %s\n", d.Port, d.Reason)
		}
	}
	for _, line := range listedLines(c.Listed) {
		if line == listedHeader {
			fmt.Fprintf(&b, "\n%s\n", line)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}

// cohortSubject is the second commit's subject line.
//
// It names the headline and its target rather than any member, because
// that is what the cohort is FOR: a reader of `git log` meeting
// "revbump 4 dependents" alone would have to open the commit to learn
// what moved underneath them. The count is there because the members
// are in the body and a subject that listed four port names would be
// past every line-length guideline the project has.
func cohortSubject(port, target string, members int) string {
	what := "dependents"
	if members == 1 {
		what = "dependent"
	}
	subject := fmt.Sprintf("%s: revbump %d %s", port, members, what)
	if target != "" {
		subject += " of " + port + " " + target
	}
	return subject
}

// memberLine is one revbumped port with its portdir and its reason, and
// the link proof where the environment took one — or, where the
// member's own run is why there is none, the reason in its place.
func memberLine(m CohortMember) string {
	line := m.Port
	if m.Portdir != "" {
		line += " (" + m.Portdir + ")"
	}
	if m.Reason != "" {
		line += ": " + m.Reason
	}
	switch {
	case m.Links == nil:
		// Nobody looked. Where that is the member's own doing the line
		// says which — a failed member listed under "Revision bumped"
		// with nothing beside it reads as evidence that was forgotten,
		// not as a build that never got there. Where it is not (a check
		// that could not be made, a commit message written before any
		// run) the silence stands, because the reason is stated once
		// elsewhere and not per member.
		if m.Unmeasured != "" {
			line += "; " + m.Unmeasured
		}
	case len(m.Links) == 0:
		// "That moved" and not "that this change publishes": the proof is
		// taken against the install names the measurement says a
		// dependent can no longer rely on, so a member that links an
		// untouched library of the headline's reads as nothing here, and
		// the sentence has to be the one that is true of it.
		line += "; links nothing that moved"
	default:
		line += "; " + strings.Join(m.Links, ", ")
	}
	return line
}

// listedHeader opens the examined-and-not-proposed block, in both the
// commit body and the pull request.
const listedHeader = "Examined and not bumped:"

// listedLines is the examined-and-left-out block: the header, then one
// line per port with the reason the proposal gave.
//
// The rows the proposal put forward are skipped here — they are the
// members, and they have their own block — so what is left is exactly
// the decisions a reviewer has to check by hand. A decision no reader
// can see is a decision nobody can disagree with, which is why these
// are printed rather than counted.
func listedLines(all []record.Candidate) []string {
	var out []string
	for _, c := range all {
		if c.Proposed {
			continue
		}
		line := c.Port
		if c.Portdir != "" {
			line += " (" + c.Portdir + ")"
		}
		if c.Reason != "" {
			line += ": " + c.Reason
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil
	}
	return append([]string{listedHeader}, out...)
}

// indent puts a quoted comment two spaces in, so a verbatim Portfile
// comment inside a commit body reads as a quotation rather than as the
// body's own prose. The bytes are otherwise untouched: a quote that was
// reflowed is not verbatim.
func indent(quote string) string {
	lines := strings.Split(strings.TrimRight(quote, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// ProposalLines is what status says under a branch that is still
// carrying a question, and what the measurement concluded about it.
//
// The proposal is advisory and human-gated, which is why these are
// lines and not a refusal: the verb that produced them exits zero. What
// they owe the reader is the criterion the proposal rests on and the
// two verbs that answer it, because a proposal a person cannot act on
// from the line in front of them is a proposal they will scroll past.
//
// The branch is a parameter because the remedies name it, and because a
// record does not know what branch it is on: a note is keyed by a
// commit, and the same commit can be reached from more than one.
func ProposalLines(n *record.Record, branch string) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, f := range n.Findings {
		switch f.Kind {
		case KindABIChanged, KindABIUnchanged, KindABIUnavailable:
			if line := abiLine(f); line != "" {
				out = append(out, line)
			}
		}
	}
	for _, f := range n.Findings {
		if f.Disposition != record.Proposed {
			continue
		}
		switch f.Kind {
		case KindCohort:
			out = append(out, fmt.Sprintf(
				"proposal: %s — `dockhand bump-revision --for %s` builds the cohort, `dockhand dismiss %s` records that you looked and said no",
				proposedPorts(f), branch, branch))
		case KindInstruction:
			// What to offer depends on whether a measurement exists and
			// what it said. The cohort verb only ever bumps what an
			// abi-change supports: with nothing measured it sends the
			// reader to verify, and with a measurement that found nothing
			// or could not be made it REFUSES — so offering it there would
			// be routing a person into a loop, which is what this line used
			// to do.
			out = append(out, fmt.Sprintf(
				"proposal: the comment in %s asks for revision bumps — %s — %s, `dockhand dismiss %s` records that you looked and said no",
				f.Source, firstLine(f.Quote), instructionStep(n, branch), branch))
		default:
			// A finding kind this build does not know, still proposed. It
			// is named rather than narrated: whatever it is, a person has
			// to answer it before an unattended publication will proceed.
			out = append(out, fmt.Sprintf(
				"proposal: %s — unanswered; `dockhand dismiss %s` records that you looked and said no", f.Kind, branch))
		}
	}
	return out
}

// instructionStep is the step to offer beside a maintainer's comment,
// decided by what the note already knows.
//
// Three answers, because the verb behind them has three behaviours. No
// ABI finding at all: nothing has measured, and `verify` is the step
// that would. A measurement that found nothing or could not be made:
// the verb refuses by name, so the honest offer is the hand and the
// dismissal. A measured change: the cohort proposal is its own line
// above this one and carries the verb, so this one does not repeat it.
func instructionStep(n *record.Record, branch string) string {
	switch measuredKind(n) {
	case "":
		return "`dockhand verify " + branch + "` measures whether anything moved"
	case KindABIChanged:
		return "the measurement above says what moved"
	}
	return "the measurement above found nothing to bump on, so this one is a judgement call by hand"
}

// measuredKind is the ABI finding a record carries, or empty where
// nothing has measured. The first wins: a change verified on two
// releases can carry two, and what this answers is whether a
// measurement exists at all.
func measuredKind(n *record.Record) string {
	for _, f := range n.Findings {
		switch f.Kind {
		case KindABIChanged, KindABIUnchanged, KindABIUnavailable:
			return f.Kind
		}
	}
	return ""
}

// abiLine states what the measurement concluded, in the criterion's own
// words.
//
// The unavailable case is a line of its own rather than silence,
// because "the check could not be made" and "the check found nothing"
// are the two answers a reader is most likely to confuse, and the
// second one is what an absent line would be read as. It is also the
// one criterion that already opens with its own verdict — a refusal has
// to say what it refused wherever it is quoted, so the judgment writes
// that in — and prefixing it here would say it twice.
func abiLine(f record.Finding) string {
	if f.Criterion == "" {
		return ""
	}
	switch f.Kind {
	case KindABIChanged:
		return "ABI changed: " + f.Criterion
	case KindABIUnchanged:
		return "ABI unchanged: " + f.Criterion
	case KindABIUnavailable:
		return f.Criterion
	}
	return ""
}

// proposedPorts says what a cohort proposal puts forward, naming the
// ports rather than counting them where there are few enough to read.
func proposedPorts(f record.Finding) string {
	var ports []string
	for _, c := range f.Candidates {
		if c.Proposed {
			ports = append(ports, c.Port)
		}
	}
	if len(ports) == 0 {
		ports = f.Ports
	}
	what := "dependents need a revision bump"
	if len(ports) == 1 {
		what = "dependent needs a revision bump"
	}
	if len(ports) > 6 {
		// Past a handful the names stop being a list and start being a
		// paragraph. The note carries all of them, and `status --json`
		// publishes it.
		return fmt.Sprintf("%d %s", len(ports), what)
	}
	return fmt.Sprintf("%d %s (%s)", len(ports), what, strings.Join(ports, ", "))
}

// firstLine is a quote reduced to one terminal line: the comment's own
// first line with its marker and indentation stripped.
//
// A terminal line is not the place for a verbatim multi-line quote —
// the branch listing is a column of standings — so the whole of it goes
// to the commit body and the pull request, and this says enough for a
// reader to recognize which comment is meant.
func firstLine(quote string) string {
	line, _, _ := strings.Cut(quote, "\n")
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if line == "" {
		return "(the comment is quoted in full on the note)"
	}
	return `"` + line + `"`
}

// CohortBody is the pull request's cohort section, derived from the
// note and from nothing else.
//
// Everything it says is a fact the record already carries, which is the
// whole reason it takes a record: the body vouches for what the note
// remembers and not for what the diff can be re-read to contain. The
// criterion is restated verbatim from the commit that carried it, the
// members come with the link proof their own runs recorded, and the
// ports the proposal examined and left out are printed with the reason
// — including a cohort the measurement REFUTED, which is the one
// sentence a reader would otherwise never see.
//
// Empty for a change with no cohort and no refutation, so a body for an
// ordinary bump is byte-identical to what it always was.
func CohortBody(n record.Record) string {
	var b strings.Builder
	for _, f := range n.Findings {
		switch f.Kind {
		case KindABIUnavailable:
			if line := abiLine(f); line != "" {
				fmt.Fprintf(&b, "%s.\n", line)
			}
		case KindABIChanged, KindABIUnchanged:
			// The measured cases, and the caveat travels with them. It used
			// to reach a reader only through the extend commit's body,
			// which means it reached nobody in the two cases that need it
			// most: a proposal published while still Proposed — a person
			// promoting past their own advisory — and a dismissed one,
			// because neither writes a cohort commit at all. There the
			// criterion went out as a bare mechanical claim with the
			// sentence saying what it cannot see nowhere the reviewer would
			// be. The unavailable case above keeps its silence: a check
			// that could not run has nothing to be insufficient about.
			//
			// KindABIUnchanged is printed whether or not a cohort was
			// carried, because an up-front cohort the measurement refuted
			// is exactly the claim a reviewer must be told about — and a
			// bump whose dependents were examined and found unaffected has
			// earned the sentence too.
			if line := abiLine(f); line != "" {
				fmt.Fprintf(&b, "%s.\n%s.\n", line, verdict.ABILimits)
			}
		case KindInstruction:
			fmt.Fprintf(&b, "\nThe comment in %s says:\n\n%s\n", f.Source, indent(f.Quote))
			if f.Disposition == record.Dismissed {
				b.WriteString("\nDismissed by hand: no revision bumps were made on it.\n")
			}
		}
	}
	for _, f := range n.Findings {
		if f.Kind != KindCohort {
			continue
		}
		if f.Criterion != "" && !statedCriterion(n, f.Criterion) {
			fmt.Fprintf(&b, "\n%s.\n", f.Criterion)
		}
		switch f.Disposition {
		case record.Proposed:
			// A proposal still open in a published body is a person having
			// promoted past their own advisory, which is theirs to do and
			// worth saying out loud rather than dressing up as a cohort.
			fmt.Fprintf(&b, "\n%s was proposed and is not in this change.\n", proposedPorts(f))
		case record.Dismissed:
			fmt.Fprintf(&b, "\n%s was proposed and dismissed by hand.\n", proposedPorts(f))
		case record.Accepted:
			b.WriteString("\nRevision bumped in this change:\n")
			for _, m := range cohortMembers(n, f) {
				fmt.Fprintf(&b, "  — %s\n", memberLine(m))
			}
		}
		for _, line := range listedLines(f.Candidates) {
			if line == listedHeader {
				fmt.Fprintf(&b, "\n%s\n", line)
				continue
			}
			fmt.Fprintf(&b, "  — %s\n", line)
		}
	}
	return b.String()
}

// statedCriterion reports whether an ABI finding already printed this
// sentence, so the body says it once. The cohort's criterion is copied
// verbatim from the measurement's on purpose — that is what makes them
// one claim — and printing it twice would read as two.
func statedCriterion(n record.Record, criterion string) bool {
	for _, f := range n.Findings {
		switch f.Kind {
		case KindABIChanged, KindABIUnchanged, KindABIUnavailable:
			if f.Criterion == criterion {
				return true
			}
		}
	}
	return false
}

// cohortMembers pairs each proposed candidate with the link proof its
// own run recorded, in the proposal's own order — which is the order
// the members had to be built in.
//
// The runs are read per port and not per platform: a member verified on
// two releases has two sets of lines, and what the body claims is that
// the binding was observed, not that it was observed twice. The lines
// are unioned and sorted so the same record renders the same bytes.
func cohortMembers(n record.Record, f record.Finding) []CohortMember {
	links := map[string][]string{}
	looked := map[string]bool{}
	states := map[string][]record.RunState{}
	for key, run := range n.Runs {
		port := runPortOf(key)
		states[port] = append(states[port], run.State)
		if run.Links == nil {
			continue
		}
		looked[port] = true
		links[port] = append(links[port], run.Links...)
	}
	out := make([]CohortMember, 0, len(f.Candidates))
	for _, c := range f.Candidates {
		if !c.Proposed {
			continue
		}
		m := CohortMember{Port: c.Port, Portdir: c.Portdir, Reason: c.Reason}
		if looked[c.Port] {
			m.Links = dedupe(links[c.Port])
		} else {
			m.Unmeasured = unmeasured(states[c.Port], c.Solo)
		}
		out = append(out, m)
	}
	return out
}

// unmeasured is the sentence a member carries in the proof's place,
// where its own runs are why no proof was taken. Empty where they are
// not: a member that passed and was still not swept had a measurement
// that could not be made, and that is said once for the whole change.
//
// The three states named are exactly the outcomes best effort publishes
// over without a pass — the population D24 made routine and D26 counts
// on the audit row — so each has a sentence where the bump is claimed.
// A failure outranks a block, which outranks a withholding, because a
// member can carry one of each across platforms and the line says the
// strongest fact about it; a non-outcome (still running, canceled, the
// machine's silence) is not published over and is the verification
// block's to name, so it earns nothing here.
//
// The withheld sentence is the one the proposal usually wrote already.
// A candidate the proposal marked Solo carries "bumped here, and not
// built" in its own reason, which is how the withheld member came to
// explain itself inline before the others did, and the line does not
// say it twice. Solo is what says so: the flag is the record's, and a
// renderer sniffing the reason's prose for the sentence would be
// coupled to its wording rather than its meaning.
func unmeasured(states []record.RunState, solo bool) string {
	failed, blocked, withheld := false, false, false
	for _, s := range states {
		switch s {
		case record.Failed:
			failed = true
		case record.Blocked:
			blocked = true
		case record.Withheld:
			withheld = true
		case record.Passed, record.Unsupported, record.Queued, record.Submitting,
			record.Running, record.Canceled, record.Superseded, record.Errored:
		}
	}
	switch {
	case failed:
		return "the build failed, so nothing was measured"
	case blocked:
		return "blocked before it was reached, so nothing was measured"
	case withheld && !solo:
		return "not built here"
	}
	return ""
}

// runPortOf is the subject half of a run key. It restates record's own
// join rather than importing a reader for it: neither a port name nor a
// release name can carry an "@", so the first one is the separator, and
// the rule is one line.
func runPortOf(key string) string {
	port, _, _ := strings.Cut(key, "@")
	return port
}

// dedupe is a line set said once each, sorted, so two platforms
// reporting the same binding read as one binding.
func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
