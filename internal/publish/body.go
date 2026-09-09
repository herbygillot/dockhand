package publish

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// RepoURL is where the PR body's "dockhand" links point, so a reviewer
// meeting the tool in a pull request can see what vouched for the claim.
const RepoURL = "https://github.com/herbygillot/dockhand"

// tracTicket is where a MacPorts ticket lives. change.Message spells the
// same URL into the commit's trailer from the same number; the two are
// one fact stated to two audiences, and the ticket travels as a NUMBER
// on record.Change so neither has to parse the other's sentence.
const tracTicket = "https://trac.macports.org/ticket/"

// treeDate is how the body prints the age of a ports tree: the day, and
// deliberately not the hour. A reviewer's question is whether the change
// was written against a current tree or a stale one, which is answered
// in days; a timestamp would invite a precision the field cannot
// support, since a commit's date is when it was made and not when the
// tree was last pulled.
const treeDate = "2006-01-02"

// defaultEvidence is what a pass claims when the run carries no phrase
// of its own: a verdict settled before Run.Evidence existed, or one from
// a provider that declared nothing.
//
// It is tart's own words because tart was the only provider when they
// were written, and a body that printed nothing here would drop the
// claim rather than weaken it. A run stamped by any provider says that
// provider's sentence instead.
const defaultEvidence = "built in a pristine VM"

// checkbox is one line of the template's "Have you" list: whether
// dockhand can vouch for it, and the item as upstream words it.
//
// The list is built before it is printed, because whether a line appears
// at all is part of what the body says. A box that could not have been
// answered is deleted rather than printed unticked, and deciding that
// per line while writing bytes is how the two get out of step.
type checkbox struct {
	ok   bool
	item string
}

// body renders the pull request body in the shape of macports-ports'
// own template, with the boxes dockhand can honestly vouch for checked,
// the ones it could not have answered deleted, and everything else left
// for the human. Candour is the accepted currency: the run set is
// enumerated in full on both paths, and a publication that carries no
// verification says which of the several possible reasons is its own.
//
// IT IS RENDERED BY Gather AND CARRIED ON THE FACTS, which is what makes
// `--body` a read and nothing else: the preview a person asks for is the
// same bytes the publication would send, produced by the same call,
// rather than a second rendering that could drift from it. Authorize
// measures it against the forge's limit, and Apply sends what it was
// handed.
//
// IT READS THE STATE REF AND NEVER A NOTE. The shipped render.PRBody
// took a record.Record and walked its Runs map, which is the exported
// projection; this walks the change's attempts, which are the authority.
// The one visible consequence is the unverified sentence: a body can now
// tell "nothing was ever run" from "this commit adds to a change that
// was verified at another tip", because content identity says so.
//
// The ticket comes off record.Change.ClosesTicket and from nowhere else
// (R16). Asks.Closes is deleted: the ticket is named once, on the bump,
// where it lands in the commit's trailer, and a second flag at
// publication time was only a chance to say two different numbers about
// one change.
func body(f Facts, version string) string {
	c := f.Change
	vs := verdicts(c, f.Attempts)
	verified := promotable(c, vs)

	var b strings.Builder
	b.WriteString("#### Description\n\n")

	// Two facts about the whole run set, taken before any line is written
	// because both of them decide what the lines below may leave out.
	//
	// proven names the subjects that actually built. A verified body
	// suppresses the states that are not verdicts, and the gate — a pass
	// somewhere and no failure anywhere — is satisfied by a cohort whose
	// second member is merely Blocked. Suppressing that member would
	// publish "verified with dockhand" over a port nobody built, so a
	// subject with no pass of its own is named whatever the header says.
	//
	// executed says some run reached a state that is a judgment about the
	// change. It is what the run-derived boxes are printed for: a
	// publication whose only run is still queued was no more in a position
	// to lint or install than one with no run at all, and three unchecked
	// boxes over it are the same false implication the deletions below
	// exist to retire.
	proven := map[string]bool{}
	executed := false
	for _, v := range vs {
		switch v.Run.State {
		case record.Passed:
			proven[v.Port] = true
			executed = true
		case record.Failed, record.Unsupported:
			executed = true
		case record.Queued, record.Submitting, record.Running,
			record.Blocked, record.Canceled, record.Superseded, record.Errored,
			record.Faulted, record.Withheld:
			// Nothing was answered. A run that never reached a verdict proves
			// no subject and leaves every question the boxes ask exactly as
			// unasked as no run at all. A withheld run belongs here too: it is
			// an outcome about the port, and it executed nothing.
		}
	}

	onPlatform := map[string]bool{}
	var lines, passed []string
	// environment is the claim each listed platform's guest makes, for the
	// "Tested on" section: the provider's own sentence, kept beside the
	// platform it belongs to rather than restated from a literal.
	environment := map[string]string{}
	tested, linted := false, false
	for _, v := range vs {
		r := v.Run
		what := ""
		switch r.State {
		case record.Passed:
			// The environment's own claim, with the two qualifiers the record
			// earns and neither of which is assumed. From-source is the run's
			// own ask: a bump installs a version whose binary archive does not
			// exist yet, so an ordinary pass proves the port builds from
			// whatever the archive server had, and only a run that was told to
			// ignore the archive may say so. The test suite was asked of the
			// ENVIRONMENT, and record.Ask carries it per run because a queued
			// attempt has no environment to carry it on.
			ev := testEvidenceOf(r)
			if ev == testsRan {
				tested = true
			}
			what = evidenceClaim(evidenceOf(r), r.Ask.FromSource, ev)
			// The lint claim rides the evidence line, because the checked box
			// below is only honest if the body states what backs it. Lint is a
			// pointer: nil is "no lint ran" and a pointer to the empty string
			// is "one ran and said nothing", which is one field where the
			// shipped record needed two.
			if r.Lint != nil {
				linted = true
				what = "linted " + lintClause(*r.Lint) + ", " + what
			}
			if r.Ask.Forced != "" {
				// Appended after the environment's own claim, never spliced into
				// it: the provider worded what a pass proves, and the forced
				// clause is this body's own — the sibling this member's build
				// deactivated (the D24 override), so a reviewer sees that the
				// environment it was proven in is not the cohort's.
				what += ", with " + r.Ask.Forced + " deactivated at the maintainer's request"
			}
			// The "Tested on" section names environments, so a platform
			// appears once however many members passed in it: listing one guest
			// nine times would overstate the evidence by a factor of nine.
			if !onPlatform[v.Platform] {
				onPlatform[v.Platform] = true
				passed = append(passed, v.Platform)
				environment[v.Platform] = evidenceOf(r)
			}
		case record.Withheld:
			// Said as this build's own act, because it is: nothing about the
			// port stopped it, and a reader who took it as a fault would go
			// looking for a breakage that is not there.
			what = "not built here, and bumped anyway"
		case record.Unsupported:
			what = "the port declines this platform (known_fail)"
		case record.Failed:
			// Stated from what a reader of this pull request can check, and not
			// from the flag that allowed it: --ignore is never written to the
			// record, so a body claiming it would be reporting an inference
			// about another package's gate. That this failed run reached a
			// published pull request is visible in the artifact itself.
			what = "the build failed, and this was published anyway"
			if r.Ask.Forced != "" {
				// A forced member that failed: the environment it failed in is
				// not the cohort's, and the reviewer is owed that on the line the
				// failure is on. Worded as the ask and not the outcome, because
				// the deactivate is a step of the build and may be the step that
				// failed.
				what = "the build failed — it was to be built with " + r.Ask.Forced +
					" deactivated, at the maintainer's request — and this was published anyway"
			}
		case record.Blocked:
			// Blamed names the neighbour whose failure this run inherited. It
			// is empty for every change with one subject — the judge writes it
			// only for a blamed port that is itself a member of the cohort — so
			// the unnamed sentence is the one that ships today, and it says
			// what is known rather than guessing a name out of the detail
			// prose.
			what = "blocked before this change was reached"
			if r.Blamed != "" {
				what = "blocked by " + r.Blamed + ", so this change was never reached"
			}
		case record.Queued:
			what = "verification was asked for and is still queued"
		case record.Submitting:
			what = "verification was starting when this was published"
		case record.Running:
			what = "verification was still running when this was published"
		case record.Canceled:
			what = "verification was canceled before it finished"
		case record.Superseded:
			what = "the branch moved out from under the run, and its verification was abandoned"
		case record.Errored:
			// record.Errored's own rule, kept in the words a reviewer needs:
			// the environment could not answer, and that is never a finding
			// about the port.
			what = "the environment could not answer, which is a fact about the machine and not about the port"
		case record.Faulted:
			// And the reviewer is told WHOSE tooling, because "the
			// environment could not answer" would be a false statement
			// about a machine that answered everything it was asked.
			what = "dockhand's own tooling did not produce an answer, which is a fact about dockhand and not about the port"
		}
		// A verdict is a fact about the change; the rest is a fact about this
		// machine's afternoon. On a verified body the second kind stays local
		// — a run this very publication overtook, or one still queued behind
		// it, establishes nothing a reviewer can act on, and the gate is
		// where a failure is answered for. On an unverified body it is the
		// whole answer to the only question the reader has, which is why the
		// publication is unverified.
		//
		// A subject that never built is the exception on both counts. It is
		// not this machine's afternoon, it is half the change, and the header
		// vouching for the whole of it is exactly the sentence a reviewer
		// would want contradicted. A FAILURE is never local either: with the
		// dependents best effort, a member can be proven on one platform and
		// failed on another and the change still publish — and the failure is
		// then the one line a reviewer most needs, on the body that is
		// otherwise vouching. Found live: a body that listed gegl's pass on
		// Sonoma and simply omitted its failure on Sequoia.
		if verified && proven[v.Port] && localToThisMachine(r.State) {
			continue
		}
		if what == "" {
			// A state word this build does not know. It is not evidence and it
			// is not a cause, so it is not narrated.
			continue
		}
		lines = append(lines, subjectPrefix(named(c), v.Port)+v.Platform+": "+what+earnedAt(v, f.Tip))
	}

	// One verdict per line: GitHub keeps single newlines in pull request
	// bodies, so the set reads as the list it is.
	switch {
	case verified:
		fmt.Fprintf(&b, "Verified with [dockhand](%s):\n", RepoURL)
	case len(lines) > 0:
		b.WriteString("Not verified:\n")
	default:
		b.WriteString(unrunLine(f))
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "  — %s.\n", line)
	}

	// The cohort, before the riders and after the evidence: it is part of
	// what this change IS — the dependents it revbumped and the
	// measurement it did so on — where a rider is something the change
	// carried along. Empty for a change with no cohort and no refutation,
	// so an ordinary bump's body is what it always was.
	if cohort := cohortBody(c, vs); cohort != "" {
		fmt.Fprintf(&b, "\n%s", cohort)
	}

	if prov := provenance(c, f.Tip); prov != "" {
		fmt.Fprintf(&b, "\n%s\n", prov)
	}
	// The riders under one "Also": housekeeping folded into a commit that
	// was already touching the file. They are the record's own words — the
	// rule names a reader can look up — because the body vouches for what
	// the record remembers and not for what the diff can be re-read to
	// contain.
	if len(c.Riders) > 0 {
		fmt.Fprintf(&b, "\nAlso: %s.\n", strings.Join(c.Riders, ", "))
	}
	if c.ClosesTicket != "" {
		fmt.Fprintf(&b, "\nCloses: %s%s\n", tracTicket, c.ClosesTicket)
	}

	// The type of change is a question to the person reading, not an
	// attestation about work dockhand did or skipped: dockhand classifies
	// no change as a bugfix, an enhancement or a security fix. That is
	// what separates it from the "Have you" list below, where an unticked
	// box reads as a step someone declined to take.
	//
	// IT DID NOT READ AS AN OPEN QUESTION, and this doc used to claim it
	// would. A field run put a version bump in front of a reader and three
	// blank boxes read as an unfilled template — a submitter who did not
	// bother rather than a tool that declined to classify. The boxes stay,
	// because deleting them would take the categories away from the person
	// who has to pick one; the sentence is what makes the blank a choice.
	//
	// It says WHO, because the answer is a judgment and the person running
	// dockhand is the one who has it. Ticking one here would be dockhand
	// asserting a classification it never determined, which is rule 7
	// pointed the other way.
	b.WriteString("\n###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n")
	b.WriteString("\ndockhand does not classify changes; the maintainer ticks the one that applies.\n")
	if len(passed) > 0 {
		b.WriteString("\n###### Tested on\n")
		for _, plat := range passed {
			// The guest's own claim, verbatim: this section names
			// environments, and the environment is the one thing here the body
			// has no standing to word.
			fmt.Fprintf(&b, "- macOS %s — %s, via dockhand\n", plat, environment[plat])
		}
	}

	// The single minted commit is the one whose message dockhand wrote in
	// project format; a branch the user grew past it is theirs to vouch
	// for.
	single := len(f.Own) == 1
	// A box is printed when this publication could have answered its
	// question, and deleted when it could not. An unchecked box under
	// "Have you" says a step was available and not taken; printing one for
	// a step that was never on offer is a false implication that costs a
	// reviewer real attention, and it is the same class of untruth as a
	// fixed unverified sentence.
	boxes := []checkbox{
		{single, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?"},
		{single, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?"},
		{f.Forge.Asked, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?"},
	}
	// A ticket named nowhere leaves nothing to have referenced. Where one
	// IS named it is in the minted commit's trailer, with the full URL,
	// because change.Message wrote it there from the same field this body
	// reads — so the box is checked from the record and the claim is true
	// in both directions. The honesty ruling cuts against understating
	// what the commit says as much as against overstating it.
	if c.ClosesTicket != "" {
		boxes = append(boxes, checkbox{true,
			"referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?"})
	}
	// Lint, tests and the pristine-VM install are all things a
	// verification run does. A publication where none of them ran — no
	// provider on the machine, a branch minted with --no-verify, a tip
	// with no attempt at all, a run canceled or still queued when the
	// publication overtook it — was never in a position to answer them, so
	// the three lines go rather than standing as three unticked
	// accusations. The gate is a run that reached a verdict and not a run
	// RECORD: a queued run is a row in the store, not a step declined.
	if executed {
		boxes = append(boxes,
			checkbox{linted, "checked your Portfile with `port lint`?"},
			checkbox{tested, "tried existing tests with `sudo port test`?"},
			checkbox{len(passed) > 0, "tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM"})
	}
	// The template's last two questions — every binary file's basic
	// functionality, and the port's most important variants — were
	// hard-coded unchecked in the first body dockhand ever wrote, because
	// dockhand cannot answer either one and never will. Left in, they say
	// the submitter skipped two steps; deleted, they say nothing, which is
	// the truth. The reviewer's checklist is upstream's template, and it is
	// still there.
	if len(boxes) > 0 {
		b.WriteString("\n###### Verification\nHave you\n\n")
		for _, box := range boxes {
			mark := " "
			if box.ok {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", mark, box.item)
		}
	}
	// Every body signs off, the unverified ones included: a pull request
	// with no verification claim still owes the reviewer the fact of how
	// it was made — and which build made it, so a sentence found to be
	// wrong can be traced to the version that wrote it.
	fmt.Fprintf(&b, "\nAutomated by [dockhand](%s)", RepoURL)
	if version != "" {
		fmt.Fprintf(&b, " %s", version)
	}
	b.WriteString("\n")
	return b.String()
}

// subjectPrefix names the member an evidence line is about, for a
// cohort, and names nothing for a change with one subject.
//
// A single change's lines already have a subject: the pull request is
// about that port and its title says so, and prefixing every line with
// it would be noise in the one place candour is the whole point. A
// cohort's lines need it, because "Sequoia: built in a pristine VM" said
// nine times over is a claim about nine different ports that reads as
// one repeated nine times.
func subjectPrefix(cohort bool, port string) string {
	if !cohort {
		return ""
	}
	return port + " on "
}

// lintClause phrases a run's lint record for the evidence line. The
// empty string is a lint that ran and said nothing, which is what a
// pointer to it means on record.Run.
func lintClause(lint string) string {
	if lint == "" || lint == "clean" {
		return "clean"
	}
	return "with " + lint
}

// evidenceOf is the claim a run's environment makes for it.
func evidenceOf(r record.Run) string {
	if r.Evidence == "" {
		return defaultEvidence
	}
	return r.Evidence
}

// testEvidence is what a run can honestly say about a test suite, and it
// has four values because the record carries two facts and either can be
// missing.
//
// "built and tested in a pristine VM" used to come from Ask.Test alone —
// the REQUEST. MacPorts' test phase executes nothing unless the Portfile
// sets test.run, which most ports do not, so a body could tell reviewers
// a suite had passed when the phase ran no command at all. Measured on
// repgrep 0.17.1: the log goes straight from "Executing
// org.macports.test" to the next note.
type testEvidence int

const (
	testsNotAsked testEvidence = iota // --test was not given
	testsRan                          // asked, and the port enables one
	testsNone                         // asked, and the port enables none
	testsUnknown                      // asked, and the preflight could not say
)

func testEvidenceOf(r record.Run) testEvidence {
	switch {
	case !r.Ask.Test:
		return testsNotAsked
	case r.HasTests == nil:
		return testsUnknown
	case *r.HasTests:
		return testsRan
	}
	return testsNone
}

// evidenceClaim composes one pass's line: what the environment says a
// pass proves, with the two qualifiers this run earned spliced in.
//
// The environment words the claim because only the provider knows what
// its environment guarantees — a clone of a prepared base proves
// something a warm CI runner cannot — and the sentence a reviewer reads
// has to move when the machine does. This body used to state the phrase
// from a literal of its own, which was true of the only provider that
// ships and would have gone on being printed over the first one that
// proves less.
//
// What this owns is the composition around it. "From source" and
// "tested" are facts about this run and not about the machine, so they
// attach to the act the claim opens with rather than to its tail.
func evidenceClaim(claim string, fromSource bool, tested testEvidence) string {
	act, where, _ := strings.Cut(claim, " ")
	if fromSource {
		act += " from source"
	}
	// "and tested" belongs to the ACT and reads inside the sentence;
	// the two qualifications are about the port rather than the build,
	// and follow the whole clause.
	if tested == testsRan {
		act += " and tested"
	}
	out := act
	if where != "" {
		out += " " + where
	}
	switch tested {
	case testsNone:
		out += ", and the port enables no test command"
	case testsUnknown:
		out += ", and whether a test suite ran was not recorded"
	case testsRan, testsNotAsked:
	}
	return out
}

// earnedAt names the commit a verdict was earned at, and says nothing
// when that is the commit being published.
//
// EVIDENCE IS GATHERED BY CONTENT, so a verdict can be a build of these
// exact bytes under a different sha: a rebase, a reworded amend, or a
// change that adopted another's passing attempt. Ruled: inheriting a
// verdict is fine as long as the reader is told which commit earned it —
// a reviewer looking at a body that vouches for a build must be able to
// go and find the build. Silence would make the two cases
// indistinguishable, and only one of them is checkable.
//
// It stays quiet in the ordinary case on purpose. Almost every line is
// earned at the tip it is published for, and appending "(at <the same
// sha>)" to all of them would bury the one line where it means
// something.
func earnedAt(v Verdict, tip string) string {
	if v.At == "" || v.At == tip {
		return ""
	}
	return " (at " + git.Abbrev(v.At) + ", identical tree)"
}

// unrunLine is the whole line a publication with no run at all carries.
//
// A BRANCH-BOUND CHANGE STATES ITS PROVENANCE RATHER THAN A CAUSE, and
// that is the one arm here that is a sentence instead of a reason.
//
// It used to be a cause like the others — "this branch was minted with
// --no-verify, so no verification was ever asked for" — and it was not
// reliably true. record.ToBranch's own doc says it is --no-verify, but
// app.destination returns it for an ordinary Enqueue too, so the arm
// fired for any branch-bound change with no runs. Measured: a bump on a
// machine with no tart, which nobody had asked to skip verification,
// published that it had been minted with a flag that was never typed.
// It also shadowed the arm below, which is the honest sentence for that
// case and could never be reached by a branch-bound change.
//
// Saying what dockhand DID and what the branch IS asserts neither
// reason, and is true of both: the person who asked for no verification
// and the machine that had none both end up here, and a reviewer's
// question — has anybody built this? — gets the same answer either way.
//
// THE ASK IS ASKED FIRST, and the destination no longer answers it. When
// --no-verify wrote ToBranch, the first arm caught every change nobody
// had asked to build. Once --no-verify and --to-pr composed, such a
// change became ToPublished and fell through to unrunCause's first arm,
// which published "no verification environment on the submitting
// machine" — as a fact, to reviewers, about a host with two provisioned
// bases and both of them free. Measured in the field on
// macports-ports#34584.
//
// record.Change.Unverified is the ask itself, so this asks it rather
// than inferring it from where the change was going.
func unrunLine(f Facts) string {
	if f.Change.Unverified || f.Change.Destination == record.ToBranch {
		return fmt.Sprintf("Generated by [dockhand](%s) - not pre-verified\n", RepoURL)
	}
	return fmt.Sprintf("Not verified: %s.\n", unrunCause(f))
}

// unrunCause says why a publication carries no run at all, for the
// changes that are not branch-bound: see unrunLine, which handles those
// and calls this for the rest.
//
// A change with attempts, none of them over
// this content, is the ordinary shape of an EXTENDED branch: a cohort
// commit inherits the headline's verification by design and carries no
// runs of its own, and the body used to publish "nothing was run"
// directly above an ABI measurement — two sentences that cannot both be
// true, with no sha offered for the reader to go and check which one
// was. A change with no attempts at all is the machine having had no
// verification environment to submit to, which is the sentence the
// shipped body printed for every one of these. That arm is sound only
// because unrunLine takes every change whose ASK was --no-verify before
// this is reached: absence of a run has two causes, and this function
// may only speak for the one it can still tell apart.
//
// The remaining arm is a change with no subjects and nothing recorded,
// which is a record something other than a mint wrote.
func unrunCause(f Facts) string {
	switch {
	case len(f.Attempts) == 0:
		// IT DOES NOT SAY WHAT THE MACHINE LACKS, because it does not
		// know. Nothing gathered here measures a verifier: zero attempts
		// is reached by a host with no tart, by a person who asked for no
		// build, and — until evidence was gathered by content — by a
		// change whose passing attempt was adopted and therefore invisible
		// to it. The old sentence named the first cause as a fact and was
		// published, twice, on a machine holding two provisioned bases
		// (macports-ports#34584, #34586). Rule 7: say what is known.
		return "nothing has been run for this commit's content"
	case !evidenceAt(f.Change, f.Attempts):
		at := ""
		for _, a := range f.Attempts {
			if a.Settled() {
				at = a.Sha
			}
		}
		if at == "" {
			return "this change's verification has not come back"
		}
		return "this commit adds to a change that was verified at `" + git.Abbrev(at) +
			"`, and its own verification has not come back"
	}
	return "no verification environment on the submitting machine, so nothing was run"
}

// provenance is the line that says where the change came from: the
// commit being published, and how current the ports tree under it was.
//
// The tree's age is the base commit's date, which is the tree the change
// was WRITTEN against — a reviewer's question is whether they are
// reading a rebase or a month-old branch, and that is what answers it.
//
// THE "verified at another sha" CLAUSE IS GONE, and its absence is a
// consequence of the state ref rather than an omission. The shipped
// line existed because the note was keyed by COMMIT, so evidence for a
// reworded amend lived on a sha reachable only from the notes ref and
// the body had to name it or send a reviewer after a commit that is not
// on the branch. Evidence is keyed by CONTENT now: an attempt over the
// same file set is evidence for these bytes whatever commit carried it,
// so there is no second sha to disclose and no reader to send anywhere.
func provenance(c record.Change, head string) string {
	tree := ""
	if !c.Base.CommittedAt.IsZero() {
		tree = "the ports tree as of " + c.Base.CommittedAt.UTC().Format(treeDate)
	}
	if head == "" {
		if tree != "" {
			return "Written against " + tree + "."
		}
		return ""
	}
	at := "Branch head `" + git.Abbrev(head) + "`"
	if tree != "" {
		return at + ", against " + tree + "."
	}
	return at + "."
}

// localToThisMachine names the run states a verified body keeps to
// itself for a subject already proven elsewhere: this publication's own
// cancellations, runs still queued or building, a branch that moved, an
// environment that could not answer, and a platform where a stranger's
// build stopped the change before it was reached. None of those is a
// verdict about the change.
//
// What is NOT here, and must never be: a failure. A subject can be
// proven on one platform and failed on another and the change still
// publish, since the dependents are best effort — and then the failure
// is the one line the reviewer most needs, on the body that is otherwise
// vouching. Withheld and unsupported are outcomes about the port too,
// and are shown for the same reason.
func localToThisMachine(s record.RunState) bool {
	switch s {
	case record.Queued, record.Submitting, record.Running,
		record.Canceled, record.Superseded, record.Errored, record.Faulted,
		record.Blocked:
		return true
	case record.Passed, record.Failed, record.Unsupported, record.Withheld:
		return false
	}
	// An unknown state is shown rather than hidden: a word this build
	// cannot read is not something to keep from a reviewer.
	return false
}
