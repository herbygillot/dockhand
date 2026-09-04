package render

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// RepoURL is where the PR body's "dockhand" links point, so a reviewer
// meeting the tool in a PR can see what vouched for the claim.
const RepoURL = "https://github.com/herbygillot/dockhand"

// abbrevLen is the width of a displayed sha: twelve hex digits, enough
// to be unique in a ports tree and short enough for one line.
const abbrevLen = 12

// abbrevSha is a sha as the body prints it — its first twelve
// characters, with anything already shorter coming back whole rather
// than indexed past its end.
//
// It restates git.Abbrev's rule instead of calling it, because a
// renderer that imports the package which shells out to git is a
// renderer that can read a repository, and these bytes are meant to be
// checkable without one. The two must agree on twelve; the golden
// pinning `0123456789ab` is what says so.
func abbrevSha(sha string) string {
	if len(sha) > abbrevLen {
		return sha[:abbrevLen]
	}
	return sha
}

// subjectPrefix names the member an evidence line is about, for a
// cohort, and names nothing for a change with one subject.
//
// A single change's lines already have a subject: the PR is about that
// port and its title says so, and prefixing every line with it would
// be noise in the one place candour is the whole point. A cohort's
// lines need it, because "Sequoia: built in a pristine VM" said nine
// times over is a claim about nine different ports that reads as one
// repeated nine times.
func subjectPrefix(named bool, port string) string {
	if !named {
		return ""
	}
	return port + " on "
}

// lintClause phrases a note's lint record for the evidence line.
func lintClause(lint string) string {
	if lint == "clean" {
		return "clean"
	}
	return "with " + lint
}

// defaultEvidence is what a pass claims when the run carries no phrase
// of its own: a record settled before Run.Evidence existed, or a gate
// proof from a provider that declared nothing.
//
// It is tart's own words because tart was the only provider when they
// were written, and a body that printed nothing here would drop the
// claim rather than weaken it. A record stamped by any provider says
// that provider's sentence instead.
const defaultEvidence = "built in a pristine VM"

// evidenceOf is the claim a run's environment makes for it.
func evidenceOf(r record.Run) string {
	if r.Evidence == "" {
		return defaultEvidence
	}
	return r.Evidence
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
// What render owns is the composition around it. "From source" and
// "tested" are facts about this run and not about the machine, so they
// attach to the act the claim opens with rather than to its tail.
func evidenceClaim(claim string, fromSource, tested bool) string {
	act, where, _ := strings.Cut(claim, " ")
	if fromSource {
		act += " from source"
	}
	if tested {
		act += " and tested"
	}
	if where == "" {
		return act
	}
	return act + " " + where
}

// treeDate is how the body prints the age of a ports tree: the day,
// and deliberately not the hour. A reviewer's question is whether the
// change was written against a current tree or a stale one, which is
// answered in days; a timestamp would invite a precision the field
// cannot support, since a commit's date is when it was made and not
// when the tree was last pulled.
const treeDate = "2006-01-02"

// checkbox is one line of the template's "Have you" list: whether
// dockhand can vouch for it, and the item as upstream words it.
//
// The list is built before it is printed, because whether a line
// appears at all is now part of what the body says. A box that could
// not have been answered is deleted rather than printed unticked, and
// deciding that per line while writing bytes is how the two get out of
// step.
type checkbox struct {
	ok   bool
	item string
}

// PRBodyOpts is what the promoting verb knows and the record does not:
// which dockhand is writing this body, the ticket named at promote
// time, how many of the branch's commits dockhand wrote, and whether
// the duplicate-PR search actually ran.
//
// A struct rather than four more positional arguments. Every field here
// ends up as a claim in a public body, and two adjacent bools that can
// be swapped at a call site with nothing to notice is how a body comes
// to state something that is not true — which is the debt this whole
// step exists to pay.
type PRBodyOpts struct {
	// Version is the running binary's, named in the sign-off. A reader
	// who finds a wrong sentence in a published body can tell from it
	// which build wrote the sentence.
	Version string
	// Head is the commit this promotion is publishing: the tip of the
	// branch being pushed.
	//
	// It is not always the commit the record hangs on. EvidenceFor takes
	// a record found over an IDENTICAL TREE at another sha when the tip
	// carries no note of its own — the reworded-message amend, the
	// rebase — and that record's Sha then names a commit reachable only
	// from the notes ref. Printing it as the branch head sent a reviewer
	// looking up a commit that is not on the branch, so the two are told
	// apart here: the head is what was pushed, and the record's sha is
	// where the verification happened.
	//
	// Empty means the caller did not say, and the record's own sha is
	// the best answer available.
	Head string
	// Closes is the ticket this promotion names, which is not always the
	// one the record carries.
	Closes string
	// OwnCommits is how many commits on the branch are dockhand's.
	OwnCommits int
	// CheckedPRs says the duplicate search ran and answered.
	CheckedPRs bool
}

// PRBody renders the PR body in the shape of macports-ports' own pull
// request template, with the boxes dockhand can honestly vouch for
// checked, the ones it could not have answered deleted, and everything
// else left for the human. Candour is the accepted currency: the run
// set is enumerated in full on both paths, and a promotion that carries
// no verification says which of the several possible reasons is its
// own.
//
// The unverified sentence used to be one fixed string — "no
// verification environment on the submitting machine" — printed
// whatever the record held. That cause is true of exactly one of the
// shapes that reach here, and the sentence stated it in public for a
// promotion whose real cause was a neighbour's failed build. So the
// body reads the record on both paths now: the same walk over the same
// runs, with the header saying whether what it found adds up to a
// verified change.
func PRBody(n record.Record, verified bool, o PRBodyOpts) string {
	var b strings.Builder
	b.WriteString("#### Description\n\n")

	named := verdict.Names(n)
	refs := verdict.Runs(n)
	// Two facts about the whole run set, taken before any line is
	// written because both of them decide what the lines below may
	// leave out.
	//
	// proven names the subjects that actually built. A verified body
	// suppresses the states that are not verdicts, and Promotable() —
	// a pass somewhere and no failure anywhere — is satisfied by a
	// cohort whose second member is merely Blocked. Suppressing that
	// member would publish "verified with dockhand" over a port nobody
	// built, so a subject with no pass of its own is named whatever the
	// header says.
	//
	// executed says some run reached a state that is a judgment about
	// the change. It is what the run-derived boxes are printed for: a
	// promotion whose only run is still queued was no more in a position
	// to lint or install than one with no run at all, and three
	// unchecked boxes over it are the same false implication the
	// deletions below exist to retire.
	proven := map[string]bool{}
	executed := false
	for _, ref := range refs {
		switch ref.Run.State {
		case record.Passed:
			proven[ref.Port] = true
			executed = true
		case record.Failed, record.Unsupported:
			executed = true
		case record.Queued, record.Submitting, record.Running,
			record.Blocked, record.Canceled, record.Superseded, record.Errored:
			// Nothing was answered. A run that never reached a verdict
			// proves no subject and leaves every question the boxes ask
			// exactly as unasked as no run at all.
		}
	}
	onPlatform := map[string]bool{}
	var lines, passed []string
	// environment is the claim each listed platform's guest makes, for
	// the "Tested on" section: the provider's own sentence, kept beside
	// the platform it belongs to rather than restated from a literal.
	environment := map[string]string{}
	tested, linted := false, false
	for _, ref := range refs {
		r := ref.Run
		var what string
		switch r.State {
		case record.Passed:
			// The environment's own claim, with the two qualifiers the
			// record earns and neither of which is assumed. From-source
			// is the run's own field: a bump installs a version whose
			// binary archive does not exist yet, so an ordinary pass
			// proves the port builds from whatever the archive server
			// had, and only a run that was told to ignore the archive
			// may say so. The test suite was asked of the ENVIRONMENT
			// and so is recorded on it: one guest runs one submission's
			// tests, however many subjects installed into it.
			if ref.Job.Test {
				tested = true
			}
			what = evidenceClaim(evidenceOf(r), r.FromSource, ref.Job.Test)
			// The lint claim rides the evidence line, because the
			// checked box below is only honest if the body states
			// what backs it.
			switch {
			case r.Lint != "" && r.Linted:
				what, linted = "linted "+lintClause(r.Lint)+", "+what, true
			case r.Linted:
				what, linted = "linted, "+what, true
			}
			// The "Tested on" section names environments, so a
			// platform appears once however many members passed in
			// it: listing one guest nine times would overstate the
			// evidence by a factor of nine.
			if !onPlatform[ref.Platform] {
				onPlatform[ref.Platform] = true
				passed = append(passed, ref.Platform)
				environment[ref.Platform] = evidenceOf(r)
			}
		case record.Withheld:
			// Said as this build's own act, because it is: nothing about
			// the port stopped it, and a reader who took it as a fault
			// would go looking for a breakage that is not there.
			what = "not built here, and bumped anyway"
		case record.Unsupported:
			what = "the port declines this platform (known_fail)"
		case record.Failed:
			// Stated from what a reader of this pull request can check,
			// and not from the flag that allowed it: --no-verify is never
			// written to the record, so a body claiming it would be
			// reporting an inference about another package's gate. That
			// this failed run reached a published pull request is visible
			// in the artifact itself.
			what = "the build failed, and this was promoted anyway"
		case record.Blocked:
			// Blamed names the neighbour whose failure this run
			// inherited. It is empty for every change with one subject —
			// the ledger writes it only for a blamed port that is itself
			// a member of the cohort — so the unnamed sentence is the one
			// that ships today, and it says what is known rather than
			// guessing a name out of the detail prose.
			what = "blocked before this change was reached"
			if r.Blamed != "" {
				what = "blocked by " + r.Blamed + ", so this change was never reached"
			}
		case record.Queued:
			what = "verification was asked for and is still queued"
		case record.Submitting:
			what = "verification was starting when this was promoted"
		case record.Running:
			what = "verification was still running when this was promoted"
		case record.Canceled:
			what = "verification was canceled before it finished"
		case record.Superseded:
			what = "the branch moved out from under the run, and its verification was abandoned"
		case record.Errored:
			// record.Errored's own rule, kept in the words a reviewer
			// needs: the environment could not answer, and that is never
			// a finding about the port.
			what = "the environment could not answer, which is a fact about the machine and not about the port"
		}
		// A verdict is a fact about the change; the rest is a fact about
		// this machine's afternoon. On a verified body the second kind
		// stays local — a run this very promotion canceled, or one still
		// queued behind it, establishes nothing a reviewer can act on,
		// and promote's own gate is where a failure is answered for. On
		// an unverified body it is the whole answer to the only question
		// the reader has, which is why the promotion is unverified.
		//
		// A subject that never built is the exception on both counts. It
		// is not this machine's afternoon, it is half the change, and the
		// header vouching for the whole of it is exactly the sentence a
		// reviewer would want contradicted.
		//
		// Withheld is named here for the same reason and needs saying
		// twice, because it now passes the proven test: a held-back
		// member counts as answered so the cohort can publish at all, and
		// the price of admitting it is that its line must appear. A
		// reviewer is being asked to take a revision bump on a port
		// nobody rebuilt, and an omission is not how you ask.
		if verified && r.State != record.Passed && r.State != record.Unsupported &&
			r.State != record.Withheld && proven[ref.Port] {
			continue
		}
		if what == "" {
			// A state word this build does not know. It is not evidence
			// and it is not a cause, so it is not narrated.
			continue
		}
		lines = append(lines, subjectPrefix(named, ref.Port)+ref.Platform+": "+what)
	}

	// One verdict per line: GitHub keeps single newlines in PR bodies,
	// so the set reads as the list it is.
	switch {
	case verified:
		fmt.Fprintf(&b, "Verified with [dockhand](%s):\n", RepoURL)
	case len(lines) > 0:
		b.WriteString("Not verified:\n")
	default:
		fmt.Fprintf(&b, "Not verified: %s.\n", unrunCause(n))
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "  — %s.\n", line)
	}

	// The cohort, before the riders and after the evidence: it is part
	// of what this change IS — the dependents it revbumped and the
	// measurement it did so on — where a rider is something the change
	// carried along. Empty for a change with no cohort and no
	// refutation, so an ordinary bump's body is byte-identical to what
	// it always was.
	if cohort := CohortBody(n); cohort != "" {
		fmt.Fprintf(&b, "\n%s", cohort)
	}

	if prov := provenance(n, o.Head); prov != "" {
		fmt.Fprintf(&b, "\n%s\n", prov)
	}
	// The riders under one "Also": housekeeping folded into a commit
	// that was already touching the file. They are the note's own words
	// — the rule names a reader can look up — because the body vouches
	// for what the record remembers and not for what the diff can be
	// re-read to contain.
	if len(n.Riders) > 0 {
		fmt.Fprintf(&b, "\nAlso: %s.\n", strings.Join(n.Riders, ", "))
	}
	if o.Closes != "" {
		fmt.Fprintf(&b, "\nCloses: https://trac.macports.org/ticket/%s\n", o.Closes)
	}

	// The type of change is a question to the person reading, not an
	// attestation about work dockhand did or skipped: dockhand
	// classifies no change as a bugfix, an enhancement or a security
	// fix, and an unticked box here reads as the open question it is.
	// That is what separates it from the "Have you" list below, where an
	// unticked box reads as a step someone declined to take.
	b.WriteString("\n###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n")
	if len(passed) > 0 {
		b.WriteString("\n###### Tested on\n")
		for _, plat := range passed {
			// The guest's own claim, verbatim: this section names
			// environments, and the environment is the one thing here
			// render has no standing to word.
			fmt.Fprintf(&b, "- macOS %s — %s, via dockhand\n", plat, environment[plat])
		}
	}

	// The single minted commit is the one whose message dockhand wrote
	// in project format; a branch the user grew past it is theirs to
	// vouch for.
	single := o.OwnCommits == 1
	// A box is printed when this promotion could have answered its
	// question, and deleted when it could not. An unchecked box under
	// "Have you" says a step was available and not taken; printing one
	// for a step that was never on offer is a false implication that
	// costs a reviewer real attention, and it is the same class of
	// untruth as the fixed unverified sentence above.
	boxes := []checkbox{
		{single, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?"},
		{single, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?"},
		{o.CheckedPRs, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?"},
	}
	// A ticket named nowhere leaves nothing to have referenced. Where
	// one IS named, the box reads the record's own ticket and not the
	// closes argument beside it: a ticket named at plan time is in the
	// minted commit's trailer, with the full URL, and bear() copied it
	// here from the plan that wrote it; a ticket named at promote time
	// reaches this body and nothing else. Checking the box off the
	// record is what keeps the claim true in both directions — the
	// honesty ruling cuts against understating what the commit says as
	// much as against overstating it.
	if n.ClosesTicket != "" || o.Closes != "" {
		boxes = append(boxes, checkbox{n.ClosesTicket != "",
			"referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?"})
	}
	// Lint, tests and the pristine-VM install are all things a
	// verification run does. A promotion where none of them ran — no
	// provider on the machine, a branch minted with --no-verify, a tip
	// with no record at all, a run canceled or still queued when the
	// promotion overtook it — was never in a position to answer them, so
	// the three lines go rather than standing as three unticked
	// accusations. The gate is a run that reached a verdict and not a run
	// RECORD: a queued run is a row in a note, not a step declined.
	if executed {
		boxes = append(boxes,
			checkbox{linted, "checked your Portfile with `port lint`?"},
			checkbox{tested, "tried existing tests with `sudo port test`?"},
			checkbox{len(passed) > 0, "tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM"})
	}
	// The template's last two questions — every binary file's basic
	// functionality, and the port's most important variants — were
	// hard-coded unchecked here from the first body dockhand ever wrote,
	// because dockhand cannot answer either one and never will. Left in,
	// they say the submitter skipped two steps; deleted, they say
	// nothing, which is the truth. The reviewer's checklist is upstream's
	// template, and it is still there.
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
	// Every body signs off, the unverified ones included: a PR with no
	// verification claim still owes the reviewer the fact of how it was
	// made — and which build made it, so a sentence found to be wrong
	// can be traced to the version that wrote it.
	fmt.Fprintf(&b, "\nAutomated by [dockhand](%s)", RepoURL)
	if o.Version != "" {
		fmt.Fprintf(&b, " %s", o.Version)
	}
	b.WriteString("\n")
	return b.String()
}

// unrunCause says why a promotion carries no run at all.
//
// Four shapes reach here and they are four different facts. A tip with
// no record was never minted by this checkout, or its note is gone; a
// record bound for the branch was minted with --no-verify, so nobody
// ever asked for a verdict and the pump steps over it; a record whose
// evidence was earned at another tip has runs — just not its own, which
// is the ordinary shape of an extended branch; and a record bound
// further with no runs at all is the machine having no verification
// environment to submit to. The last of those is the sentence the body
// used to print for all of them.
//
// The evidence arm is not a nicety. A cohort commit inherits the
// headline's verification by design and carries no runs of its own, so
// the body was publishing "nothing was run" directly above an ABI
// measurement — two sentences that cannot both be true, with no sha
// offered for the reader to go and check which one was.
func unrunCause(n record.Record) string {
	switch {
	case n.Sha == "":
		return "there is no verification record for this branch head"
	case n.Destination == record.ToBranch:
		return "this branch was minted with --no-verify, so no verification was ever asked for"
	case n.Evidence != nil && n.Evidence.From != "":
		return "this commit adds to a change that was verified at `" + abbrevSha(n.Evidence.From) +
			"`, and its own verification has not come back"
	default:
		return "no verification environment on the submitting machine, so nothing was run"
	}
}

// provenance is the line that says where the change came from: the
// commit being published, where the verification happened if that is a
// different commit, and how current the ports tree under it was.
//
// head and the record's sha are two facts, and this line said the second
// one under the first one's name. EvidenceFor answers a tip that carries
// no note with a record found over the IDENTICAL TREE at another sha —
// the reworded amend, the rebase — and that sha is reachable from the
// notes ref and from nowhere on the branch. A reviewer who looked it up
// found nothing. So the head is stated as the head, and a record earned
// elsewhere says where, with the tree identity that makes it evidence
// for these bytes at all.
//
// The tree's age is the base commit's date, which is the tree the
// change was WRITTEN against — a reviewer's question is whether they
// are reading a rebase or a month-old branch, and that is what answers
// it. The record declares a second tree age, JobRecord.TreeAsOf, for
// the tree inside the guest; nothing collects it, so nothing here can
// print it, and a line reading an unwritten field would have gone
// silently missing forever.
func provenance(n record.Record, head string) string {
	if head == "" {
		// Nobody said which commit is being published. The record's own
		// is the only sha in hand, and it was the whole line until the
		// promoting verb learned to pass its tip.
		head = n.Sha
	}
	tree := ""
	if !n.Base.CommittedAt.IsZero() {
		tree = "the ports tree as of " + n.Base.CommittedAt.UTC().Format(treeDate)
	}
	if head == "" {
		if tree != "" {
			return "Written against " + tree + "."
		}
		return ""
	}
	at := "Branch head `" + abbrevSha(head) + "`"
	if n.Sha != "" && n.Sha != head {
		at += ", verified at `" + abbrevSha(n.Sha) + "` (identical tree)"
	}
	if tree != "" {
		return at + ", against " + tree + "."
	}
	return at + "."
}
