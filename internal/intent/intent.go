// Package intent holds the shape every intent has and the tail every
// intent runs, so that each intent package holds only what makes it
// different.
//
// The split follows where the judgment is. What an intent decides —
// which spans to rewrite, which fields must have moved for the change
// to have actually happened — is its own, and stays in its own package.
// What every intent does with that decision is one sequence: apply the
// edits to a copy, evaluate the copy, diff it against the state before,
// refuse anything the prediction did not promise, and assemble the
// plan. Written three times it drifted; written once it cannot. The
// drift is worth naming, because two of the five differences are bugs
// this collapse fixed and two are behaviours it deliberately changed,
// and a reader who cannot tell which is which will restore the wrong
// one:
//
//   - Bug, dormant. bump and refresh both looked their own context up
//     by a key built with the zero variant frame, which misses every
//     context under any other frame and reports that the edit reached
//     nothing. OwnChanges matches on the subport and ignores the frame,
//     which is the only predicate that is correct. bumprevision
//     iterated every key and never had it.
//   - Bug, nondeterministic. bumprevision's accept walked the changed
//     map in map order and named the first field it disliked, so which
//     subport a refusal blamed varied between runs on identical input.
//     OnlyFields names the least by (subport, variants, field).
//   - Behaviour changed, deliberate. The via-set sibling proof ran
//     BEFORE the subports check in bump and refresh; it runs after it
//     here. A delta that violates both now reports SubportsChanged
//     rather than an ambiguous carrier, because a Portfile whose
//     structure moved is not one the set-variable question can be asked
//     about at all.
//   - Behaviour changed, deliberate. The intent's own judgment now
//     precedes the field sweep, so a change that both missed its target
//     and moved something unrelated is reported by the reason the
//     intent knows rather than the generic one.
//   - Hardening. The edit sort is stable. No two edits share a start
//     offset today, so no bytes move; riders insert at zero width, and
//     the day two can land at the same offset an unstable sort would
//     make a plan's bytes depend on the sort implementation.
//
// The two deliberate changes are pinned by tests, so restoring either
// ordering is a failure rather than a review comment.
//
// This is also where the catalogue's registration shape lives. A
// Definition is what cmd builds a verb from, so a fourth intent is an
// entry in a slice rather than a fourth hand-written constructor. The
// slice itself is assembled by cmd and never here: a definition names
// its intent package, and the intent packages import this one.
package intent

import (
	"context"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tool"
)

// Planner is the shape every intent has: parameters in the value, one
// Plan call spending everything at plan time, a *plan.Plan out or a
// *plan.Decline explaining why not.
//
// One caller calls through it: cmd's intentAction, the road every write
// intent travels — resolve, plan, summarize, gate, realize — with the
// Planner as the only thing that varies. The intents also carry
// compile-time assertions against it, so the shape the first two
// converged on is enforced rather than merely observed, and a third
// intent that drifts is a build failure instead of a review comment. A
// sweep would be the second polymorphic caller, not the first.
//
// It lives here rather than in plan because a plan is an inert value
// and this interface is the only reason plan named a port handle and a
// fetcher at all — two parameter types in a declaration plan does not
// implement, which between them dragged the Tcl shell and os/exec into
// the dependency set of everything that renders a plan.
type Planner interface {
	Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*plan.Plan, error)
}

// CohortPlanner is the shape a plural intent has: one member planned
// at a time, from the bytes the caller read rather than from the
// working tree.
//
// It is a second interface rather than a widened Planner because the
// two are asked different questions. A Planner is given a target and
// goes and reads it; a CohortPlanner is given a member's source,
// because the source that matters is what the BRANCH TIP holds — the
// working tree is sacred, may differ, and is not what the extend commit
// will be built on. The portdir travels beside the bytes for the same
// reason: the handle is a shadow of those bytes, so the directory the
// change lands in has to be stated rather than read off the handle.
//
// Only bumprevision implements it. That is not a coincidence and not a
// limitation to design around: a cohort is a set of revision bumps, and
// what makes it one commit is that every member's edit is the same
// mechanical edit for the same stated reason.
type CohortPlanner interface {
	PlanMember(ctx context.Context, h port.Handle, src []byte, portdir string) (*plan.Plan, error)
}

// Definition is one entry in the catalogue: everything cmd needs to
// build a verb, with nothing about how the verb is realized. Realizing
// is identical for every intent and belongs to the shared action.
//
// The registration exists so that the catalogue is data. An intent
// added as a fourth hand-written cobra constructor is an intent whose
// flag validation, whose caution and whose fetch behaviour are three
// more places to get subtly wrong; an intent added as a Definition
// differs from its siblings only where its fields say it does.
type Definition struct {
	// Name is the verb as typed, and the value that lands in a plan's
	// Intent field: "bump", "bump-revision", "refresh-checksums".
	Name string
	// Aliases are the shorter spellings cobra also accepts.
	Aliases []string
	// Fetches says the intent goes to the network for distfiles, so the
	// run must acquire a fetcher for it. It is load-bearing rather than
	// advisory: a revision bump downloads nothing to count, and a verb
	// that claimed otherwise would open a Tcl session per revbump.
	Fetches bool
	// Caution is printed after the plan summary, on stderr, when the
	// intent is one a reader should not act on without thinking. Only
	// refresh-checksums has one, and it says why an unchanged version
	// serving changed bytes may be a supply-chain event rather than
	// maintenance.
	Caution string
	// New builds the planner from the parameters the command line
	// gathered. It returns an error for a parameter combination only
	// the intent can judge; combinations the flag set can judge are
	// rejected at the cobra boundary, before this is called.
	New func(Params) (Planner, error)
}

// Params is everything the command line gathers for an intent, in one
// value so that New has one argument and the catalogue has one shape.
// Fields an intent does not use are simply zero — a params struct with
// unused fields is the price of a registry, and a much smaller price
// than five constructor signatures.
type Params struct {
	// Target is the port, subport or portdir as the user typed it,
	// before resolution.
	Target string
	// Version is the version to bump to, already resolved: when the
	// user asked for --latest, the command resolves it against upstream
	// and fills this in before New is called, so an intent never sees
	// the word "latest".
	Version string
	// Latest records that the version above was resolved rather than
	// stated. The intent does not need it to plan; it is here because
	// the catalogue's flag validation does, and because a resolved
	// version that arrives empty is a different bug from a stated one.
	Latest bool
	// Reason is why users must rebuild — required by a revision bump,
	// which is meaningless in review without it.
	Reason string
	// ClosesTicket is the ticket this change closes. It is carried into
	// Identity, and from there onto the plan as a field of its own and
	// into the mint commit's trailer. It rides as a field precisely so
	// that it stays out of Slug and Summary: the plan's bytes are a hash
	// gate, and a subject line that grew a ticket number would be a
	// different plan for the same change. A plan carrying a ticket does
	// hash differently from one that does not, which is the honest
	// answer — the ticket was an argument to the run.
	ClosesTicket string
	// Recheck asks for a re-derivation at the version the port already
	// carries: fetch again, compare, regenerate. It is how a stealth
	// update is caught.
	//
	// It is --recheck on the command line. One switch spelled it and the
	// in-flight replacement both until S10, and this field was named apart
	// from that switch so that the day they were spelled apart nothing
	// about the planner would have to move. They are --recheck and
	// --replace now, and nothing here moved.
	Recheck bool
	// Riders says what this run does with the housekeeping riders every
	// headline intent is examined for. It is a run parameter and not an
	// intent's property: the rules are written once, and which of them
	// ride is the caller's to say.
	Riders RiderPolicy
	// Cohort are the other ports this change is planned alongside. The
	// substrate is plural and the catalogue runs it at N==1, so this is
	// always empty today — present so that the day a cohort lands,
	// nothing about the registration shape has to move.
	Cohort []string
	// Tools is the run's tool finder, handed down rather than
	// discovered: which binaries this machine has is one fact, found
	// once. It is not a user parameter — it is here because a planner
	// that regenerates a vendored block or reads a lockfile out of a
	// distfile needs it, and New takes only Params.
	Tools *tool.Finder
	// Dependents are the ports the tree's reverse index says depend on
	// this one, for the instruction-comment rule to read a roster
	// against.
	//
	// Not a user parameter either, and empty in every case but one: the
	// run fills it only where the Portfile actually carries a
	// revbump-instruction comment, because building the reverse index is
	// a full pass over the PortIndex and the rule is the only reader.
	// Empty means the rule falls back to its word list, which is what it
	// does for every ordinary port anyway.
	Dependents []string
}

// Identity is what a change is called, decided by the intent that made
// it because only the intent knows. A plan's Slug names the branch, its
// Summary is the commit's subject line, and Intent is the verb — all
// three settled at plan time so that no realizer re-derives a name the
// planner already had.
//
// ClosesTicket rides along for the commit message and is deliberately
// not folded into Summary. It reaches the plan as a field of its own —
// the plan gate hashes it there like any other argument — and the point
// of the separate field is that Summary stays the commit's subject and
// a trailer stays a trailer.
type Identity struct {
	// Intent is the verb: the Definition's Name.
	Intent string
	// Slug is the change's short identity — "jq-1.8.2", "jq-checksums",
	// "jq-rev1" — from which the branch name is composed.
	Slug string
	// Summary is the one-line description in the project's commit
	// format, computed by the intent itself.
	Summary string
	// ClosesTicket becomes a "Closes:" trailer on the mint commit. It
	// is intent-level only: promote's --closes stays in the PR body,
	// with its checklist box honestly unchecked.
	ClosesTicket string
}
