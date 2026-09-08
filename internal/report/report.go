// Package report is where dockhand's OPERATOR projections are
// composed: the lines a bump prints when it has minted something, the
// standings `status` lists, the summary a pass leaves behind, and the
// one remedy line every one of them ends on. It is what remained of
// internal/render once the two PUBLISHED renderings moved to the
// lifecycles that publish them — the pull request body to publish and
// the cohort commit message to change — so that the words upstream
// reads are written by the package that sends them, and this package
// holds only the words an operator reads.
//
// NOTHING OUTSIDE THIS PACKAGE RENDERS, and nothing outside cli imports
// this package: those two sentences are one done-criterion said from
// both ends. An operation states what happened in a typed result and
// narrates through progress.Sink; the sentence for that result is
// chosen here, once, so a phrase can be pinned by a golden instead of
// by a run.
//
// Nothing here looks at anything. Elapsed time arrives as a clock the
// caller already read, a settled attempt arrives already settled, and a
// residency arrives already probed. That is not tidiness: a projection
// that could poll a worker or open a repository would put these bytes
// back behind I/O, where they can only be checked by reproducing the
// world that produced them. .golangci.yml names the edges that would
// end it.
//
// The promise is about this package's own code, not its import closure:
// report imports plan in order to print one, and plan's dependencies
// reach the Tcl shell. Nothing in these files calls any of it.
package report

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
)

// BranchLine is how a branch and its standing share one line: the name
// padded into a fixed column, then a single space, then the standing.
// status and a pass both list branches and a reader scans the two
// listings as one, so the column is a single number rather than two
// that happen to agree today.
const BranchLine = "%-32s %s\n"

// Remedy is THE RESIDENCY-DERIVED REMEDY LINE, and it is the reason
// this function exists rather than the sentence being written at each
// of the four call sites that need one.
//
// docs/todo.md records the shipped defect: `cycle`'s own report told a
// reader of stdout to run `dockhand cycle`, the verb they had just run.
// The fix is not a better sentence — it is deriving the sentence from a
// fact the invocation cannot lie about. A `cycle` invocation IS the
// thing that starts what is queued, and a resident `dispatch` IS the
// thing that keeps starting it, so the only honest question is whether
// a scheduler holds $GIT_COMMON_DIR/.dockhand-dispatch.lock. With the
// answer in hand there are exactly TWO sentences, not the four
// spellings the same todo counted:
//
//	resident      — information: the scheduler will start it, and who it is
//	not resident  — an instruction naming the two verbs that would
//
// The third state is not a third sentence. Under ResidencyUnknown this
// process does not know whether a scheduler is running here, and one
// that answered "nobody is resident" would appoint itself judge beside a
// scheduler it could not see (rule 7). So it says what it does not know
// and instructs nothing.
//
// WHAT IT MUST NOT SAY IS WHY. ResidencyUnknown is reached two ways —
// a probe that failed (a permission error, a filesystem with no flock)
// and `status --no-update`, which takes no lock at all and is Unknown BY
// CONSTRUCTION — and the value cannot tell them apart. The shipped
// sentence named the first cause unconditionally, so a healthy
// `status --no-update` on a local disk announced that the dispatch lock
// could not be read when nothing had opened it, and sent operators
// hunting a filesystem fault that did not exist. A projection may render
// only what it was handed: the fact here is "unknown", the cause is not
// on the value, and inventing one is the same rule-6 mistake as
// recovering a fact by reading words.
//
// It returns "" for the states with nothing to say, so a caller may
// print it unconditionally and get no blank line for its trouble.
func Remedy(r app.Residency) string {
	switch r.State {
	case app.DispatcherResident:
		return "  " + who(r) + " will start it"
	case app.NoDispatcher:
		return "  nothing here will start it yet: `dockhand dispatch` keeps them moving, or `dockhand cycle` starts them once"
	case app.ResidencyUnknown:
		return "  whether a scheduler is resident on this checkout was not established, so nothing here promises to start it"
	}
	return ""
}

// Residency is the standing line `status` prints about the scheduler
// itself — the fact the surface calls the biggest of what status must
// newly report, because a resident dispatcher publishes by default and
// a standing grant is only inspectable if this line exists.
//
// Its Unknown branch says what is unknown and never why, for the reason
// Remedy's does: the two roads into that state are a probe that failed
// and a `--no-update` that never probed, the value does not distinguish
// them, and the shipped line asserted the first for both.
func Residency(r app.Residency) string {
	switch r.State {
	case app.DispatcherResident:
		since := ""
		if !r.Since.IsZero() {
			since = ", since " + r.Since.Local().Format("15:04")
		}
		return "dispatch resident (" + strings.TrimPrefix(who(r), "dispatch ") + since + ")"
	case app.NoDispatcher:
		return "no dispatcher on this checkout"
	case app.ResidencyUnknown:
	}
	return "dispatcher residency not established"
}

// who names the resident dispatcher as tersely as the stamp allows. A
// stamp that could not be read leaves "a dispatcher", which is the
// honest answer and still a true subject for the sentences above: the
// lock is held, and by something.
func who(r app.Residency) string {
	if r.Holder.PID == 0 {
		return "a dispatcher"
	}
	if r.Holder.Host != "" && r.Holder.Host != hostUnset {
		return fmt.Sprintf("dispatch (pid %d on %s)", r.Holder.PID, r.Holder.Host)
	}
	return fmt.Sprintf("dispatch (pid %d)", r.Holder.PID)
}

// hostUnset is the one host value that says nothing — an OwnerID whose
// hostname lookup failed. Naming it keeps `who` from printing "on ".
const hostUnset = ""

// Change writes what one change road did, and it is the projection the
// exit band is derived beside rather than from: app.Result.Exit() owns
// the code and this owns the sentence, so the two cannot drift by one
// being edited without the other.
//
// The order is the order a reader needs it: what exists now (a branch),
// then what is happening to it (an attempt), then what they should do
// (the remedy, and only where something is waiting).
func Change(w io.Writer, res app.Result, r app.Residency) {
	switch res.Did {
	case app.Shown, app.Edited, app.NotRealized, app.NothingToDo:
		return
	case app.Minted, app.Queued, app.Started, app.Stood:
	}
	if b := res.Ref.Branch(); b != "" {
		fmt.Fprintf(w, "minted %s\n", b)
	}
	switch res.Did {
	case app.Minted:
		if res.Deferred != nil && res.Deferred.Reason == app.NoProvider {
			fmt.Fprintf(w, "  %s\n", res.Deferred.Detail)
		}
	case app.Queued:
		fmt.Fprintf(w, "  attempt %s queued\n", res.Attempt)
		if res.Deferred != nil {
			fmt.Fprintf(w, "  %s\n", deferral(*res.Deferred))
			return
		}
		fmt.Fprintln(w, Remedy(r))
	case app.Started:
		fmt.Fprintf(w, "  attempt %s started%s\n", res.Attempt, leaseNote(res.Lease))
	case app.Stood:
		fmt.Fprintf(w, "  attempt %s %s\n", res.Attempt, verdictWord(res.Verdict))
	case app.Shown, app.Edited, app.NotRealized, app.NothingToDo:
	}
	for _, o := range res.Owed {
		fmt.Fprintf(w, "  owed: %s (%s)\n", o.What, o.Why)
	}
	for _, s := range res.Superseded {
		fmt.Fprintf(w, "  superseded %s\n", s)
	}
}

// leaseNote is the " (lease 7f2a)" suffix, absent when nothing started.
func leaseNote(token string) string {
	if token == "" {
		return ""
	}
	return " (lease " + short(token) + ")"
}

// deferral is the sentence a typed Deferral earns. It branches on the
// REASON and never on the Detail (rule 6): the remedy differs per reason
// — install a provider, provision a base image, ask for a platform this
// backend answers — and the Detail is what the provider said, printed
// after the remedy rather than instead of it.
func deferral(d app.Deferral) string {
	switch d.Reason {
	case app.NoProvider:
		return d.Detail
	case app.NoEnvironment:
		return "no environment for this release yet: `dockhand provision tart --macos <release>` — " + d.Detail
	case app.Unsupported:
		return "the provider cannot run this: " + d.Detail
	case app.ProviderError:
		return "the provider refused: " + d.Detail
	case app.DeferredUnknown:
	}
	return d.Detail
}

// Sweep writes the plural road's rows and its census. Declines are
// QUIET on a sweep — the exit partition says so and this listing has to
// agree — so a decline is one line among the rest rather than a
// paragraph, and only the census at the end is addressed to a person.
func Sweep(w io.Writer, sw app.Sweep, r app.Residency) {
	var minted, started, queued, withheld, declined, hard int
	for _, row := range sw.Rows {
		switch {
		case row.Hard != nil:
			hard++
			fmt.Fprintf(w, BranchLine, row.Target, "error: "+row.Hard.Error())
			continue
		case row.Withheld != nil:
			withheld++
			fmt.Fprintf(w, BranchLine, row.Target, "withheld: "+row.Withheld.Detail)
			continue
		case row.Decline != nil:
			declined++
			fmt.Fprintf(w, BranchLine, row.Target, "declined: "+row.Decline.Error())
			continue
		}
		switch row.Result.Did {
		case app.Started:
			minted, started = minted+1, started+1
			fmt.Fprintf(w, BranchLine, branchOf(row), "started"+leaseNote(row.Result.Lease))
		case app.Queued:
			minted, queued = minted+1, queued+1
			fmt.Fprintf(w, BranchLine, branchOf(row), "queued")
		case app.Minted:
			minted++
			fmt.Fprintf(w, BranchLine, branchOf(row), "minted")
		case app.Stood:
			fmt.Fprintf(w, BranchLine, row.Target, "stands already")
		case app.Shown, app.Edited, app.NothingToDo, app.NotRealized:
		}
	}
	fmt.Fprintf(w, "%d minted · %d started · %d queued · %d withheld · %d declined · %d errored\n",
		minted, started, queued, withheld, declined, hard)
	if queued > 0 {
		fmt.Fprintln(w, Remedy(r))
	}
}

// branchOf names a row by the branch it minted, falling back to the
// target it was asked about. A row that minted nothing has no branch,
// and printing an empty column would put the standing in the wrong
// place on the line.
func branchOf(row app.Row) string {
	if b := row.Result.Ref.Branch(); b != "" {
		return b
	}
	return row.Target
}

// VerifyRows writes one line per platform a verify asked about, and the
// remedy under any that stayed queued.
func VerifyRows(w io.Writer, v app.VerifyResult, r app.Residency) {
	if v.Adopted {
		fmt.Fprintf(w, "adopted %s\n", v.Change.Branch())
	}
	queued := false
	for _, a := range v.Attempts {
		switch a.Did {
		case app.Started:
			fmt.Fprintf(w, "  attempt %s started%s\n", a.Attempt, leaseNote(a.Lease))
		case app.Queued:
			queued = true
			fmt.Fprintf(w, "  attempt %s queued\n", a.Attempt)
			if a.Deferred != nil {
				fmt.Fprintf(w, "  %s\n", deferral(*a.Deferred))
				queued = false
			}
		case app.Stood:
			fmt.Fprintf(w, "  attempt %s %s\n", a.Attempt, verdictWord(a.Verdict))
		case app.Minted, app.Shown, app.Edited, app.NothingToDo, app.NotRealized:
		}
	}
	if queued {
		fmt.Fprintln(w, Remedy(r))
	}
}

// Promotion writes what a publication did. The advisories come FIRST
// and the outcome last, because an advisory is what a reviewer will
// have to be told and the URL is what the person typing this came for.
func Promotion(w io.Writer, p app.PromoteResult) {
	if p.Body != "" {
		fmt.Fprintln(w, p.Body)
		return
	}
	for _, a := range p.Advisories {
		fmt.Fprintf(w, "  %s: %s\n", a.Kind, a.Text)
	}
	for _, id := range p.Running {
		fmt.Fprintf(w, "  attempt %s is still building; `dockhand cancel` stops it\n", id)
	}
	if p.Published == nil {
		return
	}
	if p.Published.URL != "" {
		fmt.Fprintf(w, "opened %s\n", p.Published.URL)
		return
	}
	fmt.Fprintln(w, "pushed to the fork; no pull request was asked for")
}

// Pass writes what one cycle did — the pass's own summary, which is
// what a `cycle` prints and what one tick of a `dispatch` prints.
//
// The counts come first and the rows that want a person come after, in
// that order and never the reverse: a pass is N outcomes, most of them
// uneventful, and a reader scanning for the handful that are addressed
// to them should not have to scroll past thirty that are not.
func Pass(w io.Writer, p app.Pass, dry bool) {
	if dry {
		fmt.Fprintln(w, "dry run: nothing irreversible was performed — and this is NOT a read-only pass; it still observed, judged and settled")
	}
	settled, started := 0, 0
	for _, res := range p.Changes {
		switch res.Did {
		case app.Stood:
			settled++
		case app.Started:
			started++
		case app.Minted, app.Queued, app.Shown, app.Edited, app.NothingToDo, app.NotRealized:
		}
	}
	// PUBLICATIONS AND NOT Published: the pass keeps publish.Apply's
	// Outcome on the error path too, so that a caller can tell "pushed, no
	// pull request" from "never left the machine", and a count over every
	// entry reported a publication for a candidate that refused before any
	// I/O at all — with an empty URL on the line beneath it.
	published := p.Publications()
	fmt.Fprintf(w, "%d settled · %d started · %d retired · %d published · %d owed · %d refused\n",
		settled, started, len(p.Retired), len(published), len(p.Owed), len(p.Refusals))
	if p.Compacted != nil {
		fmt.Fprintf(w, "  %d closed records dropped\n", *p.Compacted)
	}
	for _, out := range published {
		fmt.Fprintf(w, "  published %s\n", publication(out))
	}
	for _, out := range p.Retired {
		fmt.Fprintf(w, "  retired #%d %s\n", out.Number, out.URL)
	}
	for _, a := range p.Advisories {
		fmt.Fprintf(w, "  %s: %s\n", a.Kind, a.Text)
	}
	for _, ns := range p.Ineligible {
		fmt.Fprintf(w, "  attempt %s not started: %s\n", ns.Attempt, ineligible(ns))
	}
	if p.MaintainErr != nil {
		// Advisory and never a band: a gc.lock held by the operator's own
		// `git maintenance start` is somebody else doing the housekeeping,
		// and the pass that settled, retired, published and drained did not
		// fail because of it.
		fmt.Fprintf(w, "  git maintenance did not run: %v\n", p.MaintainErr)
	}
	Refusals(w, p.Refusals)
	if v := p.Vacancy; v.Known {
		fmt.Fprintf(w, "  %d of %d environments free\n", v.Free, v.Limit)
	}
}

// publication is how a pass names one publication it made. The URL is
// what a person wants and what they can open; a pull request whose
// number the forge gave but whose URL it did not is named by the number,
// because an empty string on a line reading "published" is the shape of
// a publication that never happened.
func publication(out publish.Outcome) string {
	switch {
	case out.URL != "":
		return out.URL
	case out.Number != 0:
		return "#" + strconv.Itoa(out.Number)
	}
	return "(the forge named neither a number nor a url)"
}

// Refusals writes the rows a pass could not carry forward. It is
// exported because a resident dispatcher prints them EDGE-TRIGGERED —
// once per change, from its own announced-at memory — rather than on
// every tick, and that memory is cli's; this is the words for whichever
// subset cli decides to say.
func Refusals(w io.Writer, rs []app.Refusal) {
	for _, r := range rs {
		fmt.Fprintf(w, "  %s needs you: %v\n", r.Change, r.Err)
	}
}

// ineligible is the sentence a queued-but-not-started attempt earns.
// Typed, because the three answers have three different remedies and a
// reader recovering the fact from the Detail would be reading words
// (rule 6).
func ineligible(ns run.NotStarted) string {
	switch ns.Why {
	case run.Held:
		return "held — `dockhand unhold` releases it" + detail(ns.Detail)
	case run.Superseded:
		return "a newer sibling replaced its change" + detail(ns.Detail)
	case run.Closed:
		return "its change is closed" + detail(ns.Detail)
	case run.IneligibleUnknown:
	}
	return "ineligible" + detail(ns.Detail)
}

func detail(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// Standings is `status`: the whole local picture, at whichever of the
// three depths the invocation asked for.
//
// THE FIRST LINE SAYS WHAT WAS ASKED. Under --no-update nothing was
// polled, no lock was taken and no forge was asked, so the report says
// so rather than letting a missing line read as an answer — a report
// with no PR standings looks exactly like a report of no pull requests,
// and rule 7 forbids the reader having to guess which they are holding.
//
// It takes no residency parameter of its own: the residency is on the
// result, because it is what the OPERATION was handed and what it chose
// its settle posture from, and a report that printed a second reading
// of the lock could disagree with the judgment that was already made.
func Standings(w io.Writer, s app.StatusResult, now time.Time) {
	if s.NoUpdate {
		// THE PURE READ, SAID BECAUSE IT WAS ASKED FOR and not inferred
		// from what came back empty. A default `status` whose lock probe
		// failed also settles nothing and also carries an unknown
		// residency, and the shipped condition — unknown plus no settled
		// attempts — printed this line over a report that had polled the
		// provider, listed obligations and read the forge cache.
		fmt.Fprintln(w, "the ledger as written: nothing was polled, nothing was settled, and no forge was asked")
	}
	fmt.Fprintln(w, Residency(s.Residency))
	for _, id := range s.Settled {
		fmt.Fprintf(w, "  settled attempt %s\n", id)
	}

	rows := standingRows(s, now)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank < rows[j].rank })
	for _, row := range rows {
		fmt.Fprintf(w, BranchLine, row.name, row.standing)
		for _, note := range row.notes {
			fmt.Fprintf(w, "  %s\n", note)
		}
	}
	for _, d := range s.Disagreeing {
		fmt.Fprintf(w, BranchLine, d.Ref, disagreement(d))
	}
	Obligations(w, s.Obligations)
	if v := s.Vacancy; v.Known {
		fmt.Fprintf(w, "%d of %d environments free\n", v.Free, v.Limit)
	}
	if s.Spent.Counted() {
		fmt.Fprintln(w, allowance(s.Spent, now))
	}
	if anyQueued(rows) {
		fmt.Fprintln(w, Remedy(s.Residency))
	}
}

// allowance is the standing publication grant `status` prints, and the
// window it counts over is THE PACE'S and not the store's.
//
// The two are different numbers and the shipped line printed the wrong
// one. publish.MaxWindow is 24h: the floor statestore.Compact keeps
// machine publication rows above, and the span publish.Gather collects
// stamps over precisely because Gather is handed no Pace. It is not an
// allowance and nothing is measured against it — Spend.Within is what
// applies the pace, and publish.Authorize asks it over pace.Window. A
// report counting the store's floor said "9 in the last 24h" for a
// machine that had spent 2 of 20 in the six hours that decide whether
// the next publication is refused.
//
// THE PACE IT NAMES IS THE DEFAULT, AND IT SAYS SO. A dispatcher's own
// --publish-max and --publish-every live in the process holding the
// lock; the residency stamp carries a pid, a host and a verb, and there
// is nowhere else for `status` to read them from. So the line prints the
// grant a dispatcher gets when nobody overrode it, names it as the
// default, and never claims to know what the resident one was started
// with — which is the honest half of the fact rather than a confident
// wrong one.
func allowance(spent publish.Spend, now time.Time) string {
	pace := publish.DefaultPace
	return fmt.Sprintf("machine publications: %d of %d in the last %s (the default allowance; counted as of %s)",
		spent.Within(pace.Window, now), pace.Max, pace.Window, short(spent.At()))
}

// disagreement is the line a bound record whose ref a foreign hand
// moved earns, and the two halves of it are two different remedies. It
// is shown rather than skipped, because a change missing from the
// listing reads as nothing to report (rule 7).
//
// EVERY REMEDY HERE IS A ROAD THAT ACTUALLY EXISTS, and the reason that
// sentence has to be written down is that one of them did not. The
// shipped line offered `dockhand discard <branch>` for a MOVED branch;
// Discard.Run resolves before it does anything, meets the same
// disagreement this line was rendered from, and refuses it — exit 45,
// the very code the report is describing. A person told to abandon a
// change that way ran a command that could not work, and on a host with
// no verifier the other half of the sentence refused too (exit 33,
// before FollowIn), leaving no road at all.
//
// So the moved half names the two that work, in the order a person
// wants them: `verify` FOLLOWS the commit they made, and git puts the
// ref back where the record says it was, which is what makes every other
// verb — `discard` included — resolve again. The recorded tip is printed
// rather than described, because a remedy a reader has to go and look
// something up for is a remedy they will get wrong.
//
// The gone half keeps `discard`: Discard.Run carries an absent ref
// through as the observation its close is handed, so that road really
// does end the record.
func disagreement(d *change.TipDisagreement) string {
	if d == nil {
		return "the record and its ref disagree"
	}
	if d.Absent {
		return "the branch is gone — `dockhand discard " + target(d) + "` ends it"
	}
	return "moved by hand — `dockhand verify " + target(d) + "` follows your commit, or " + restore(d) + " puts it back"
}

// target is the name a person types at a verb for this ref: the branch,
// or the change id behind a pin, which is what change.Resolve takes.
func target(d *change.TipDisagreement) string {
	if b, ok := strings.CutPrefix(d.Ref, "refs/heads/"); ok {
		return b
	}
	return string(d.ID)
}

// restore is the git that puts a hand-moved ref back at the tip the
// record holds. A branch is moved with `git branch -f` and anything else
// — the pin a branchless snapshot lives on — with `git update-ref`,
// because the ref namespaces are not the same and a command that named
// the wrong one would be the second remedy in this file that cannot run.
func restore(d *change.TipDisagreement) string {
	if b, ok := strings.CutPrefix(d.Ref, "refs/heads/"); ok {
		return "`git branch -f " + b + " " + short(d.Recorded) + "`"
	}
	return "`git update-ref " + d.Ref + " " + short(d.Recorded) + "`"
}

// Obligations lists what this checkout owes and what it merely found.
// A FOREIGN root is NAMED and never seized: an obligation belonging to
// another checkout is that checkout's own pass to discharge, and a
// report that said "somebody else's" would leave a person with nowhere
// to go.
func Obligations(w io.Writer, obs []lease.Obligation) {
	for _, o := range obs {
		what := obligationKind(o.Kind)
		if o.Root != "" {
			fmt.Fprintf(w, "owed elsewhere: %s on %s, held by %s\n", what, o.Platform, o.Root)
			continue
		}
		fmt.Fprintf(w, "owed: %s on %s (%s)\n", what, o.Platform, o.Why)
	}
}

// obligationKind is the one word an obligation kind is printed as. It
// is a switch here and not a String method on the type, because the
// vocabulary belongs to the report: lease owns what the kinds ARE and
// this package owns what a person is told they are.
func obligationKind(k lease.ObligationKind) string {
	switch k {
	case lease.Owed:
		return "a release this checkout claimed and did not finish"
	case lease.Requested:
		return "a lease written before the provider answered"
	case lease.Untracked:
		return "an environment no lease accounts for"
	case lease.Due:
		return "a kept environment past its deadline"
	case lease.UnknownObligation:
	}
	return "an obligation of no stated kind"
}

// standingRow is one change as the report lists it, with the sort rank
// the attention order assigns.
type standingRow struct {
	name     string
	standing string
	notes    []string
	rank     int
	queued   bool
}

func anyQueued(rows []standingRow) bool {
	for _, r := range rows {
		if r.queued {
			return true
		}
	}
	return false
}

// standingRows turns the store into lines, in the ATTENTION ORDER: the
// changes a person must act on first, the ones waiting on a machine
// after them, and the ones doing exactly what they should last.
//
// The order is imposed HERE and never on the value: a sort applied to
// the state would reorder what every other reader holds, and a report
// is a value first and a listing second. Refname order — which is what
// the ref listing gives and what the shipped report printed — is
// alphabetical order of a slug nobody chose for reading, so the one
// branch that failed sits wherever its port name puts it, under twelve
// branches doing exactly what they should. A fleet's report is scanned,
// not read, and what it is scanned for is the handful of changes that
// want a person.
func standingRows(s app.StatusResult, now time.Time) []standingRow {
	byChange := map[record.ChangeID][]record.Attempt{}
	for _, a := range s.State.Attempts {
		byChange[a.Change] = append(byChange[a.Change], a)
	}
	var rows []standingRow
	for _, c := range s.State.Changes {
		if c.State.Closed() {
			continue
		}
		row := standingRow{name: nameOf(c)}
		row.standing, row.rank, row.queued = standingOf(c, byChange[c.ID], now)
		if f, ok := s.Facts[c.ID]; ok && f.Forge.OwnFound {
			row.notes = append(row.notes, forgeLine(f.Forge.Own.State, f.Forge.Own.Number, f.Forge.AsOf, now))
		}
		if c.Hold != nil {
			row.notes = append(row.notes, "held: "+holdReason(c.Hold))
		}
		for _, f := range c.Findings {
			if f.Disposition == record.Proposed {
				row.notes = append(row.notes, "proposes: "+f.Criterion)
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

// nameOf is what a change is called on a line: its branch, or the
// headline port for a branchless snapshot, which has no branch to name
// and would otherwise print as an empty column.
func nameOf(c record.Change) string {
	if c.Branch != "" {
		return c.Branch
	}
	if len(c.Subjects) > 0 {
		return c.Subjects[0].Port + " (snapshot)"
	}
	return string(c.ID)
}

// holdReason is a hold's own words, or the honest admission that it has
// none. "held" with no reason and "held for a reason nobody wrote down"
// are the same to a reader and the second is what actually happened.
func holdReason(h *record.Hold) string {
	if h.Reason == "" {
		return "no reason recorded"
	}
	return h.Reason
}

// standingOf is one change's standing, its attention rank, and whether
// anything about it is waiting on a scheduler.
//
// The rank is the band and not the verb: a FAILED verdict and an
// upstream duplicate are different problems with the same urgency, and
// a reader scanning the page wants both above the thirty branches that
// passed.
func standingOf(c record.Change, atts []record.Attempt, now time.Time) (string, int, bool) {
	worst, worstRank := "", 99
	queued := false
	for _, a := range atts {
		if a.Sha != c.Tip {
			continue // a former tip's work says nothing about what stands
		}
		text, rank := attemptStanding(a, now)
		if a.Queued() {
			queued = true
		}
		if rank < worstRank {
			worst, worstRank = text, rank
		}
	}
	if worst == "" {
		return "no verification asked for", 40, false
	}
	return worst, worstRank, queued
}

// attemptStanding is one attempt's line and its rank.
func attemptStanding(a record.Attempt, now time.Time) (string, int) {
	switch {
	case a.Queued():
		return "queued", 30
	case a.Active():
		return "building, " + since(a.Started, now), 20
	}
	return verdictLine(a, now)
}

// verdictLine is a settled attempt's own words: the worst run on it,
// with when it landed. Worst-first, because a cohort with one failure
// is a change that failed.
func verdictLine(a record.Attempt, now time.Time) (string, int) {
	worst := record.Passed
	seen := false
	for _, r := range a.Runs {
		if bad(r.State) > bad(worst) || !seen {
			worst, seen = r.State, true
		}
	}
	if !seen {
		return "settled with nothing measured", 5
	}
	// The rank is the BAND and not the verdict: a reader scanning the
	// page wants everything addressed to them above the thirty branches
	// that passed, and a passing change is the one row that never is.
	rank := 5
	switch worst {
	case record.Passed:
		rank = 50
	case record.Withheld:
		rank = 45
	case record.Queued, record.Submitting, record.Running,
		record.Failed, record.Unsupported, record.Blocked,
		record.Canceled, record.Superseded, record.Errored:
	}
	return verdictWord(worst) + " " + since(a.Started, now), rank
}

// bad ranks two run states so verdictLine can take the worse of them.
// It is the same precedence app.verdictOf uses and it is deliberately
// not shared: that one produces an EXIT CODE and this one produces a
// sentence, and a projection that imported a judgment would be a second
// place a verdict is decided.
func bad(s record.RunState) int {
	switch s {
	case record.Failed:
		return 5
	case record.Errored, record.Canceled, record.Superseded:
		return 4
	case record.Unsupported:
		return 3
	case record.Blocked:
		return 2
	case record.Passed:
		return 1
	case record.Withheld:
		return 0
	case record.Queued, record.Submitting, record.Running:
		return 4
	}
	return 4
}

// verdictWord is the one word a run state is printed as, and it is the
// single place the vocabulary is spelled: `status`, a --wait and a
// pass's settle row all read the same word for the same state.
func verdictWord(s record.RunState) string {
	switch s {
	case record.Passed:
		return "passed"
	case record.Failed:
		return "FAILED"
	case record.Blocked:
		return "blocked"
	case record.Unsupported:
		return "unsupported"
	case record.Errored:
		return "errored"
	case record.Canceled:
		return "canceled"
	case record.Superseded:
		return "superseded"
	case record.Withheld:
		return "withheld"
	case record.Queued:
		return "queued"
	case record.Submitting:
		return "submitting"
	case record.Running:
		return "running"
	}
	return string(s)
}

// forgeLine states a pull request's standing AND WHEN IT WAS LEARNED.
// "open" and "open when I last looked" are different claims, and the
// courtesy cache means the second is what a default `status` is holding
// — so the AsOf is printed rather than implied, and `--refresh` is what
// makes it now.
func forgeLine(state string, number int, asOf, now time.Time) string {
	line := fmt.Sprintf("PR #%d %s", number, state)
	if asOf.IsZero() {
		return line + ", never looked up"
	}
	return line + ", as of " + since(asOf, now)
}

// since is a duration a person reads: "2m ago", "3h ago", "5d ago". A
// zero time has no age at all and says so, because "0s ago" is a claim
// about a moment nobody recorded.
func since(t, now time.Time) string {
	if t.IsZero() {
		return "at an unrecorded time"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// short abbreviates an opaque token to its first eight characters, which
// is what a lease and an attempt id are printed as: long enough to name
// one in a listing, short enough to sit at the end of a line.
func short(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

// Purged renders what a purge removed, or would have.
//
// The three populations are printed separately because they are three
// different kinds of loss. A branch is a person's work and its removal
// is the only irreversible line here — git keeps a reflog entry, but
// nothing else names the commit — so the branches are listed by name
// rather than counted. The pins and the notes are machinery: a pin kept
// a snapshot reachable and a note is a derived export that regenerates
// from the state ref, so a count is the whole of what a reader needs.
//
// THE KEPT LINE IS NOT DECORATION. Purge deliberately leaves the state
// ref, so a checkout whose branches have all just gone will still have
// `status` list every change. A reader who was not told that reads the
// next command's output as a bug, so the count is stated here, once,
// beside the removal that caused it.
func Purged(w io.Writer, p app.PurgeResult) {
	if p.DryRun {
		fmt.Fprintln(w, "dry run: nothing was removed")
	}
	verb := "removed"
	if p.DryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "%s %d branch(es) \u00b7 %d pin(s) \u00b7 %d record(s)\n",
		verb, len(p.Branches), len(p.Pins), p.Notes)
	for _, ref := range p.Branches {
		fmt.Fprintf(w, "  %s\n", strings.TrimPrefix(ref, "refs/heads/"))
	}
	// THE STATE REF IS ITS OWN LINE and it carries the record count,
	// because that count is the last account anybody gets of what was in
	// there: nothing survives the purge to look it up afterwards.
	if p.StateRef {
		fmt.Fprintf(w, "%s the state ref and the %d change record(s) it held\n", verb, p.Records)
	}
	// THE ENVIRONMENTS ARE REPORTED SEPARATELY FROM THE REFS, and their
	// absence is reported differently from their emptiness: nil means the
	// provider was never reached, where an empty non-nil slice means it
	// was asked and held none. Collapsing those two into one line would
	// tell a person the machine is clean on the strength of a question
	// nobody put.
	if p.Removed != nil {
		fmt.Fprintf(w, "%s %d environment(s)\n", verb, len(p.Removed))
		for _, name := range p.Removed {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	// WHAT WAS LEFT IS SAID OUT LOUD, AND EACH REASON SEPARATELY. A
	// purge that removed four and said nothing about the six still
	// standing would report a clean machine that is not one, and the
	// three reasons need three different next steps from the reader:
	// nothing, nothing, and one named command.
	if len(p.Kept) > 0 {
		fmt.Fprintf(w, "kept %d reference image(s); `provision tart` restores a base by cloning one\n", len(p.Kept))
		for _, name := range p.Kept {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if len(p.Theirs) > 0 {
		fmt.Fprintf(w, "left %d environment(s) belonging to another checkout\n", len(p.Theirs))
		for _, name := range p.Theirs {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if len(p.Unowned) > 0 {
		fmt.Fprintf(w, "left %d environment(s) nothing on this machine accounts for; `dockhand cycle --reclaim-unattributed` clears those\n", len(p.Unowned))
		for _, name := range p.Unowned {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if p.EstateRefused != nil {
		// Rule 7 at the surface: not "there are none".
		fmt.Fprintf(w, "environments were NOT removed: %v\n", p.EstateRefused)
	}
}
