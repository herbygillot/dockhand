package render

// Attention order: the sequence `status` lists branches in, and the
// sentences an open pull request's age earns.
//
// The listing used to be git's: `for-each-ref refs/heads/dockhand/`
// with no --sort, so branches arrived in refname order and the report
// printed them that way. Refname order is alphabetical order of a slug
// nobody chose for reading, which means the one branch that failed sits
// wherever its port name puts it — under twelve lines of branches doing
// exactly what they should. A fleet's report is scanned, not read, and
// what it is scanned for is the handful of changes that want a person.
//
// So the enumeration order stays what it is — every caller of
// git.Branches still walks it — and the attention order is imposed
// above it, in the renderings. That placement is the whole design: a
// sort applied to Report.Branches would reorder the value every caller
// holds, and a report is a value first and a listing second.

import (
	"fmt"
	"sort"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// Tier is the target port's maintainer tier, which is what the
// project's update policies key on: nomaintainer ports may be updated
// by anyone, openmaintainer ports allow minor updates by others, and a
// maintained port wants its maintainer's approval.
//
// It arrives as a value the caller read, like every other fact here. A
// renderer that resolved a tier would be opening a Portfile to choose a
// sentence, and no golden can pin a sentence that needs a tree.
//
// The empty tier is a real answer and not a missing one: a branch with
// no readable record names no port, a Portfile can be gone from under a
// change, and "we do not know who maintains this" is what the reader
// should be told rather than a tier picked to fill the gap.
type Tier string

const (
	// TierUnknown is no answer: no record, no portdir, no readable
	// Portfile, or a maintainers field nothing could be made of.
	TierUnknown Tier = ""
	// TierNomaintainer is the nomaintainer keyword: nobody to ask.
	TierNomaintainer Tier = "nomaintainer"
	// TierOpenmaintainer is the openmaintainer keyword: unsolicited
	// minor updates are invited, and still wait out the window.
	TierOpenmaintainer Tier = "openmaintainer"
	// TierMaintained is a port naming at least one maintainer and
	// neither keyword.
	TierMaintained Tier = "maintained"
)

// ReviewWindow is the 72 hours the project's update policies give a
// maintainer to look at somebody else's pull request.
//
// It is the same 72 hours for every tier, and that is a ruling rather
// than an oversight. The policy's window is a maintainer-review window,
// so a literal reading would give nomaintainer ports no window at all —
// and nomaintainer is 63.5% of the tree, which would either put every
// one of those pull requests in the attention band from the minute it
// opened or keep them out of it forever. A band holding almost
// everything and a band holding almost nothing are the same useless
// band. docs/cli.md settles it from the other direction: for dockhand's
// persona the window "is merely a lower bound on waiting; the binding
// constraint is committer attention", and committer attention is not a
// function of the tier.
//
// What the tier decides is what the elapsed window MEANS — who, if
// anyone, was being waited on, and therefore what a follow-up can
// honestly say. That is where it is spent, and only there.
const ReviewWindow = 72 * time.Hour

// band is one row of the ruled attention order. The values are the
// order: a lower band lists first.
//
// A branch can answer to several of these at once — a failed run on a
// held branch whose pull request has aged out — and the first band it
// matches is the one that claims it. The ruled sequence is a priority
// list, so first match is what "failures first" means; the alternative,
// counting matches or preferring the narrowest, would sort a failing
// branch by something other than its failure.
type band int

const (
	// bandFailure is a run that failed. The only state that argues
	// against the change itself, and the only one where the reader's
	// next act is to read a log.
	bandFailure band = iota
	// bandExpired is an open pull request past its review window: the
	// waiting has stopped being what policy asked for and started being
	// something to do about.
	bandExpired
	// bandQueued is a run asked for and not yet started. It is above
	// passed-unpromoted because a queue is a thing the machine is about
	// to spend an hour on, and a person who wanted it stopped has a
	// narrow window to say so.
	//
	// Which is why held and ended are CLAIMED ahead of it: on those two
	// the hour is never spent, so the promise this band makes would be
	// false. See bandOf, where the divergence is argued.
	bandQueued
	// bandUnpromoted is a change that passed and was never pushed: the
	// work is done and one verb finishes it.
	bandUnpromoted
	// bandHeld is a change a person stopped. Below the four above it
	// because a hold is the reader's own earlier answer — it needs
	// remembering, not deciding.
	bandHeld
	// bandEnded is the two quiet end states: a pull request closed
	// without merging, and a branch a newer sibling replaced. Nothing
	// further will happen to either without a person choosing it.
	bandEnded
	// bandRest is everything else, in the enumeration order it arrived
	// in. Running builds, unverified tips, merged-and-cleaned branches,
	// open pull requests still inside their window: the report is not
	// silent about them, it just does not lead with them.
	bandRest
)

// bandOf places one branch in the ruled order.
//
// The facts are read off the report and nothing is derived here: every
// one of these was already carried for some other sentence's sake,
// which is why an ordering this specific costs no new observation
// except the pull request's age and the port's tier.
//
// The ARMS below are the claim order and the VALUES are the display
// order, and they diverge in one place: held and ended are asked before
// queued and before passed-unpromoted, though all three list after
// those. Both divergences are the same argument, made about the two
// bands that promise a reader something is about to happen.
//
// A held branch that passed and was never pushed answers to two bands
// truthfully, and filing it under passed-unpromoted would put "this one
// is ready to promote" next to a change somebody stopped — the hold IS
// the answer to that question, and so is a newer sibling having replaced
// the branch.
//
// Held before QUEUED is the same sentence one arm higher, and it is the
// stronger case of the two. bandQueued sits where it does because a
// queue is an hour of the machine about to be spent and a person who
// wanted it stopped has a narrow window to say so — but a held change is
// the one branch in the report where that hour will never be spent: the
// drain refuses a held record at the walk and again under the submit
// lock, so the run stays queued and reprints itself every pass, forever.
// Leading the report with the one branch guaranteed not to move is the
// opposite of what the ordering is for. The same reasoning does NOT
// reach bandExpired, which is claimed first: that pull request really is
// waiting on reviewers whatever the local hold says, and its line still
// prints the hold.
//
// Everywhere else the two orders agree, and a divergence added later
// needs a sentence like these or it is just a bug with a comment.
func bandOf(b BranchReport, now time.Time) band {
	n := b.Note
	switch {
	case n != nil && n.AnyState(record.Failed):
		return bandFailure
	case b.windowElapsed(now):
		return bandExpired
	case n != nil && n.Hold != nil:
		return bandHeld
	case b.rejected(), n != nil && n.SupersededBy != "":
		return bandEnded
	case n != nil && n.AnyState(record.Queued):
		return bandQueued
	case n != nil && n.Promotable() && !b.Retire.Promoted:
		return bandUnpromoted
	}
	return bandRest
}

// rejected is a pull request closed without merging — found, not
// merged, not open. It is asked of the judgment's own facts rather than
// of the verdict, because the verdict a branch was RETIRED under is not
// available once the branch is gone and this question is about the
// branch that is still here.
func (b BranchReport) rejected() bool {
	return b.Retire.PR.Found && !b.Retire.PR.Merged && !b.Retire.PR.Open
}

// windowElapsed reports whether this branch's pull request is open and
// past its review window.
//
// An unknown creation time is never elapsed. The zero time is the year
// one, so a comparison would make an unknown-age pull request the
// oldest thing in the namespace and put it at the top of the band
// claiming the window ran out two thousand years ago. Silence about a
// timestamp is not evidence of age.
func (b BranchReport) windowElapsed(now time.Time) bool {
	if !b.Retire.PR.Found || !b.Retire.PR.Open || b.PRCreatedAt.IsZero() {
		return false
	}
	return now.Sub(b.PRCreatedAt) >= ReviewWindow
}

// Ordered is the branch listing in attention order, as a new slice.
//
// A copy, and never a sort in place, because the same Report is what
// the caller may still be holding and reading in the order the pass
// produced it: an ordering that mutated the value would reorder a
// listing that never asked for one, which is exactly the mistake this
// function exists in this package to avoid.
//
// Within a band the enumeration order survives — that is what makes the
// sort stable rather than merely deterministic, and it is why eleven of
// the sixteen branches in a report move only because the five above
// them left. The one exception is the expired band, ordered oldest
// first: those branches are in a band precisely because of how long
// they have waited, so sorting them by name would throw away the fact
// that put them there.
func (r Report) Ordered() []BranchReport {
	out := make([]BranchReport, len(r.Branches))
	copy(out, r.Branches)
	bands := make(map[string]band, len(out))
	for _, b := range out {
		bands[b.Branch] = bandOf(b, r.Now)
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := bands[out[i].Branch], bands[out[j].Branch]
		if bi != bj {
			return bi < bj
		}
		if bi == bandExpired {
			return out[i].PRCreatedAt.Before(out[j].PRCreatedAt)
		}
		return false
	})
	return out
}

// StillnessLines say why a change has stopped moving: a person held it,
// or a newer sibling replaced it.
//
// Both facts have been on the wire since the record grew them and
// neither has ever been rendered, which was survivable while the report
// was in refname order and unreadable the moment it was not. The
// ordering gives each of them a band, and a band whose members look
// exactly like the branches around them is a sort with nothing to show
// for itself — "held" has to be legible on the line, or the reader sees
// a passed change sitting in a strange place.
//
// Above the proposals and below the standings. A proposal is advice
// about a change that is still moving; these two say it is not, which
// the reader needs first — advice about a superseded branch is advice
// to do work twice.
func StillnessLines(n *record.Record, branch string) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Hold != nil {
		reason := n.Hold.Reason
		if reason == "" {
			// The pointer is the hold and the reason is optional, which is
			// the distinction the pointer exists to keep. A hold with nothing
			// written on it still stops things, and saying so beats printing
			// an empty quotation.
			reason = "no reason given"
		}
		out = append(out, fmt.Sprintf("held: %s — `dockhand unhold %s` releases it", reason, branch))
	}
	if n.SupersededBy != "" {
		out = append(out, fmt.Sprintf("superseded by %s — `dockhand cycle --superseded` removes it",
			n.SupersededBy))
	}
	return out
}

// WindowLines is what status says under an open pull request: where it
// stands in its review window, and — once the window has elapsed — the
// follow-up a person might send about it.
//
// Nothing here is said about a pull request that is not open. A merged
// one is finished and a closed one is answered; an age would be
// archaeology in both cases, and the branch's own line already says
// which it is.
func WindowLines(b BranchReport, now time.Time) []string {
	if !b.Retire.PR.Found || !b.Retire.PR.Open || b.PRCreatedAt.IsZero() {
		return nil
	}
	age := now.Sub(b.PRCreatedAt)
	pr := b.Retire.PR.Number
	if age < ReviewWindow {
		return []string{fmt.Sprintf("PR #%d — %s into the 72-hour review window%s",
			pr, humanAge(age), tierClause(b.Tier))}
	}
	lines := []string{fmt.Sprintf("PR #%d — open %s, the 72-hour review window elapsed %s ago%s",
		pr, humanAge(age), humanAge(age-ReviewWindow), tierClause(b.Tier))}
	if draft := FollowUpDraft(b, now); draft != "" {
		lines = append(lines, draft)
	}
	return lines
}

// tierClause names who was being waited on, when the port said.
func tierClause(t Tier) string {
	if t == TierUnknown {
		return ""
	}
	return " (" + string(t) + ")"
}

// FollowUpDraft is the polite follow-up a maintainer might send about a
// pull request that has aged past its window — written out in full, and
// sent by nobody.
//
// dockhand cannot send it. That is not a policy this function enforces,
// it is a fact about the tree: internal/gh has no comment method, no
// ping, no review and no merge, and the only pull request writes
// anywhere are `pr create` and `pr edit` on the publish road. Pinging
// spends somebody else's attention — ring 3 — and a tool that could
// spend it unattended would eventually spend it twice. So the draft is
// rendered where the reader can read, edit and discard it, and the
// sending stays a person's act in a person's mail client.
//
// It is drafted every pass, with no memory that it was drafted before.
// "Never twice" is a property of the sending, which dockhand does not
// do; a record that a draft had been shown would be state kept to
// suppress a line, and the line is how the reader knows the pull
// request is still waiting.
func FollowUpDraft(b BranchReport, now time.Time) string {
	if !b.windowElapsed(now) {
		return ""
	}
	subject := b.Branch
	if b.Note != nil {
		if port := b.Note.Headline().Port; port != "" {
			subject = port
		}
	}
	body := fmt.Sprintf("PR #%d (%s) has been open %s; the 72-hour review window elapsed %s ago.",
		b.Retire.PR.Number, subject, humanAge(now.Sub(b.PRCreatedAt)), humanAge(now.Sub(b.PRCreatedAt)-ReviewWindow))
	if s := tierSentence(b.Tier); s != "" {
		body += " " + s
	}
	return fmt.Sprintf("follow-up draft — dockhand cannot send this; macports-dev or the PR: %q", body)
}

// tierSentence is what the elapsed window means under each tier, in the
// words the project's own policies use. Unknown says nothing: a
// follow-up that guessed at a port's maintenance in front of the people
// who maintain it would be worse than one that only states the age.
func tierSentence(t Tier) string {
	switch t {
	case TierNomaintainer:
		return "The port is nomaintainer, so there is nobody to review it — it needs a committer."
	case TierOpenmaintainer:
		return "The port is openmaintainer, so a minor update may proceed once the window has passed."
	case TierMaintained:
		return "The port is maintained; past the window a committer may proceed if the commit message documents the timeout."
	case TierUnknown:
		return ""
	}
	return ""
}

// humanAge is a duration at the scale a review window is discussed on:
// hours up to two days, then days.
//
// Truncating and not rounding, because these numbers are read as "at
// least this long" — a window that elapsed 47 hours ago has elapsed one
// day, and saying two would be the tool overstating the case it is
// asking somebody to act on. Nothing below an hour is distinguished: a
// pull request minutes old is not what this line is for.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if h := int(d.Hours()); h < 48 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dd", int(d.Hours())/24)
}
