// Package publish owns the publication lifecycle: what a person or a
// machine is permitted to put in front of reviewers, and the effects
// that follow. Every gate that can refuse lives in Authorize; every
// irreversible act lives in Apply; and Apply cannot be called without
// the value Authorize returns.
//
// ITS IMPORTS ARE RULED, and they are the whole of the layering claim
// this package makes: change, gh, git, macports, record, statestore —
// with NO ledger among them, so publish cannot even obtain a
// record.Record. That is not tidiness. record.Record is the note's
// shape, a derived export, and nothing decides from a derived copy; an
// earlier draft of this design had publish.Facts carrying one, so the
// gate ladder would have decided from the projection while every other
// road decided from the state ref. A package that cannot reach the
// exporter cannot make that mistake.
//
// darwin/abi is the one edge beyond that list, and it is one CONSTANT
// wide: abi.Limits, the caveat that travels beside a measurement's
// criterion wherever it is quoted. The pull request body quotes the
// criterion a cohort proposal rests on, and the sentence saying what
// otool cannot see has to be the same sentence in the commit body and
// in the pull request — which is exactly why it is a constant and not
// a paragraph anyone rewords.
package publish

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// ForgeFacts is what the forge said, and when. Freshness is a field
// rather than an assumption because an authorization granted over a
// tip is not authorization over a later one.
type ForgeFacts struct {
	Upstream       string
	ForkRemote     string
	ForkOwner      string
	Own            gh.PullRequest
	OwnFound       bool
	Duplicate      gh.PullRequest
	DuplicateFound bool
	Err            error // the forge could not be reached; a refusal for a machine, an advisory for a person
	AsOf           time.Time
	// Fresh is rule 7 over ruling 8, on the value a decision reads: true
	// when THIS gather asked the forge, false when it served what a cycle
	// cached. Authorize REFUSES Facts whose Forge.Fresh is false, so a
	// cached standing can never reach a decision — the report may say
	// "no PR as of 03:14", the gate may not. A bool beside AsOf rather
	// than a staleness threshold, because "how old is too old" is a
	// policy nobody ruled and "did we ask" is a fact.
	Fresh bool
	// SamePort are the other open pull requests the duplicate walk saw on
	// this port and did NOT call duplicates. They are advisories rather
	// than a refusal — somebody else is working on the same port, which a
	// maintainer coordinating both changes wants to know before a
	// reviewer's attention is spent — and they are ON THE FACTS rather
	// than printed as the walk goes, which is the shipped defect this
	// removes: engine's promote wrote them to stderr from inside the
	// search, so no caller could count them, hold them, or render them
	// anywhere but a terminal.
	//
	// The sketch's ForgeFacts stops at Duplicate. It has to grow this
	// field or the advisories have no producer: Authorize is pure and
	// cannot walk a forge, and Gather is the only thing here that ever
	// saw the list.
	SamePort []gh.PullRequest
	// Asked says the duplicate walk actually ran and answered. It is rule
	// 7 over the two zero values SamePort and DuplicateFound share: "no
	// duplicate found" and "nobody looked" are the same silence to a
	// reader of those two fields alone, and one of them checks a box in
	// the pull request body that vouches for a search.
	Asked bool
}

// Asks is what the invocation asked for. They are separated from the
// facts because a request is not evidence, and because three of them
// are only honoured for a person.
type Asks struct {
	Remote string
	Title  string
	Body   bool
	NoPR   bool
	// Ignore was NoVerify (R15): "ignore the verdict", not "skip the
	// work". The evidence gate is walked and its refusal is turned into an
	// advisory the PR body states; nothing is skipped.
	Ignore    bool
	NoPRCheck bool
	Force     bool
	// Closes is NOT here (R16). The ticket is named once on the bump and
	// carried: Prepared.Closes -> the commit trailer -> record.Change
	// .ClosesTicket -> the PR body. Gather reads it off the record.
}

// ignore, noPRCheck and force are the three asks as THIS invoker may
// spend them, which is to say as a person and never as a machine.
//
// The first two are a person's judgment written down: "I have looked at
// the failure and I am publishing anyway", "I know about the other pull
// request". An unattended pass has no judgment to write down, so on that
// road the flags are not refused — they are simply not honoured, which
// is the reading that cannot be argued with by a caller that set one by
// accident. --ignore unreachable from the machine road is the whole of
// the positive-evidence rule; the duplicate check unreachable is what
// keeps a rate-limited pass from opening a second pull request beside
// somebody's first.
//
// force is narrowed the same way and for a stronger reason than either.
// It selects a with-lease force-push and a retitle of an open pull
// request — the most damaging thing on this road. Reading it through a
// method rather than raw is what keeps that a property of the TYPE
// instead of a property of every call site remembering.
// The test is `== record.Human` and not `!= record.Machine`, which is
// the same rule-7 reading the invoker gets one function over: the zero
// Driver is a wiring gap and not a person, so an invocation that never
// declared who it was honours none of the three.
func (a Asks) ignore(by record.Driver) bool    { return a.Ignore && by == record.Human }
func (a Asks) noPRCheck(by record.Driver) bool { return a.NoPRCheck && by == record.Human }
func (a Asks) force(by record.Driver) bool     { return a.Force && by == record.Human }

// Facts is everything Authorize may look at. It is gathered once,
// before any of it is used, and it is the only input to the decision.
type Facts struct {
	// The change and its attempts come from the STATE REF, not from the
	// exported note. An earlier draft had this field as a record.Record,
	// which is the note's shape — so the gate ladder would have decided
	// from the derived copy the design says nothing decides from, and
	// publish cannot even obtain one, since it does not import ledger.
	Change   record.Change
	Attempts []record.Attempt
	Branch   string
	Tip      string
	Own      []string // commits beyond the mint
	Forge    ForgeFacts
	// Direction is how this change moves its port's version, and it is
	// here as an observation with an Err because a draft ASSERTED it was
	// enforced elsewhere and it is not: record.ToPublished is written in
	// exactly one place (internal/cmd/intent.go:117, the --to-pr road),
	// internal/engine/publishslot.go:342 admits on it, and on that road
	// direction is `moving := carrier.Text(src) != b.Version`
	// (internal/intent/bump/bump.go:142) — a string inequality. A
	// downgrade typed by hand would have reached an unattended
	// publication.
	//
	// THE FROM SIDE IS UPSTREAM MAIN'S TIP, not record.Change.Base.Sha.
	// The install a downgrade strands is whatever main ships at merge, and
	// when Drift.BehindBy > 0 the two differ.
	Direction Direction
	Invoker   record.Driver
	// Unattended is what THIS BUILD AND THIS CHANGE together permit a
	// machine to do, and it replaces a bool meaning "this build permits
	// unattended publication".
	//
	// A bool was sufficient while the answer was a build constant. Under
	// the 2026-09-06 ruling the permission is conditional on the change —
	// a machine may publish a non-prerelease version bump whose edits are
	// confined to the version, checksum and vendored regions and which
	// passed verification — so the value has to name the permission
	// granted rather than the one withheld, which is this document's own
	// rule for exactly this field.
	Unattended Grant
	// Simplicity is the edit-confinement verdict, computed by change.Judge
	// over the REALIZED change and passed in as a parameter. It is not
	// read back off record.Change, and that is deliberate: the tree's own
	// discipline is that provenance fields are "never an input to a gate —
	// one that could widen what the unattended road is allowed to do would
	// be an authorization rather than a record of what happened"
	// (internal/engine/mint.go:369-375). A stored verdict is a token; a
	// recomputed one is a judgment.
	Simplicity change.Simplicity
	// Why is what change.Judge said when Simplicity is NotSimple — the
	// regions that refused it, in the judge's own words. It rides beside
	// the verdict because the refusal a machine writes is the only account
	// a person ever gets of an unattended pass's reasoning.
	Why []string
	// Regions is what the RECONSTRUCTION found this change is made of,
	// passed in beside Simplicity by the same caller and for the same
	// reason: change.Reconstruct produces both, Judge consumes one, and
	// record.Publication.Basis has no other producer.
	//
	// It is written onto the row as PROVENANCE and never read back as
	// permission — record.Publication says so where Basis is declared —
	// which is why it travels as a value from the caller that recomputed
	// it rather than as a field publish reads off the change.
	Regions []record.Region
	// Drift is change.Behind's answer — how far the branch fell behind
	// the base and whether the portdir moved underneath it — and it is
	// here because Behind had no consumer: the pending register named
	// the gatherer as missing, and Gather is it.
	Drift change.Drift
	// Spent is how many MACHINE publications the store already holds
	// inside Pace.Window, derived by Gather over the publications the
	// state ref keeps — the DURABLE last-published moment R5 requires,
	// with no fifth record kind to hold it (record.Station refused). It
	// counts a publication whose OpenPR step Finished, and by rule 7 one
	// whose OpenPR step is Uncertain (a crash between the push and the
	// answer may have opened one); it NEVER counts a RefreshPR step or a
	// no-op permit, because an adversarial pass showed a dispatcher
	// refreshing every open PR on every tick spending the whole 20/6h
	// allowance on refreshes inside two hours and then refusing real
	// publications for the rest of the window.
	//
	// It is a Spend and not an int, and that is this step's spec
	// correcting the sketch: "unexported fields and one constructor,
	// because a zero spend admits the whole cap". See Spend.
	Spent Spend
	Asks  Asks
	// Title is what the pull request will be called: the ask when a person
	// named one, and otherwise the MINTED commit's subject, which is
	// already in the project's `<port>: <description>` form. The sketch's
	// Facts is silent about it and Permit carries a title, so the value
	// needs a producer — and it cannot be Authorize's, which is pure and
	// cannot read a commit message.
	Title     string
	Body      string
	BodyLimit int
	AsOf      time.Time
}

// Spend is how many machine publications the store already holds, and
// when each of them happened. Its fields are unexported and Spent is
// its only constructor, which is the mechanism run.Open's doc names as
// "publish.Permit's ... applied to the value whose zero is most
// expensive" — turned back on the value that decides an allowance.
//
// THE ZERO VALUE IS "NOBODY COUNTED", NOT "NOTHING SPENT". Every other
// fact in the roll call fails closed when nobody filled it in; a bare
// integer here would fail OPEN — a forged zero says the machine has
// published nothing and admits the whole cap, which is the one
// direction this value must never fail in. So counted is set by the
// constructor and by nothing else, and Authorize refuses an uncounted
// Spend on the machine road (ErrSpendUnknown, rule 7).
//
// IT HOLDS INSTANTS AND NOT A COUNT, because the window it must be
// counted over belongs to a value Gather never sees: Pace is
// Authorize's parameter, and Gather takes no pace. So the derivation
// collects every machine publication inside MaxWindow — the constant
// floor statestore.Compact keeps machine rows above, which cli's refusal
// of a longer --publish-every makes an upper bound on any pace window —
// and Within counts the ones inside whatever window the pace actually
// names. One read, one derivation, and the pace applied where the pace
// is known.
type Spend struct {
	counted bool
	at      string      // the state commit the count was taken from
	stamps  []time.Time // when each counted publication opened, oldest first
}

// Counted reports that this value came from Spent over a real read. It
// is the rule-7 half: false is "nobody counted", which is not "nothing
// spent".
func (s Spend) Counted() bool { return s.counted }

// At is the state commit the count was taken from, so a report can say
// as-of. It is a stamp and never a gate.
func (s Spend) At() string { return s.at }

// Within is how many of the counted publications fall inside window,
// measured back from now. An uncounted Spend answers zero, which is
// safe only because Authorize refuses one before it asks.
func (s Spend) Within(window time.Duration, now time.Time) int {
	if !s.counted || window <= 0 {
		return 0
	}
	since := now.Add(-window)
	n := 0
	for _, at := range s.stamps {
		if at.After(since) {
			n++
		}
	}
	return n
}

// Pace is the machine's publication allowance: at most Max publications
// per Window, counted against Facts.Spent. R5's numbers are the
// defaults — --publish-max 20 per --publish-every 6h — and per-process
// counters never reach the cap, which is why Spent is derived over the
// store. Set is rule 7: a zero Pace would be "publish nothing" and
// "nobody configured this" at once, on the road that publishes by
// default.
type Pace struct {
	Set    bool
	Max    int
	Window time.Duration
}

// DefaultPace is R5 spelled once.
var DefaultPace = Pace{Set: true, Max: 20, Window: 6 * time.Hour}

// MaxWindow is the longest --publish-every cli accepts, and it is the
// floor statestore.Compact keeps machine publication rows above: a
// machine publication is never dropped while its first step's At is
// younger than this, whatever Retention says. A CONSTANT, and the
// reason it is one is a cross-process defect an adversarial pass found
// in the draft's rule "Retention.ClosedFor must cover Pace.Window": that
// check could only run in the process that calls Compact — a person's
// `cycle --compact`, which is Human, publishes nothing and carries no
// --publish-every — while the window Spent must cover belonged to a
// DIFFERENT process, the resident dispatcher. Two flags on two processes
// over one store; the check passed in one and the undercount landed in
// the other. With the floor a constant, any Retention is safe, including
// ClosedFor = 0, the compacting process needs no Pace, and the risk
// entry retires. cli refuses a --publish-every above it, so a window
// Spent must cover is always inside the floor.
const MaxWindow = 24 * time.Hour

// ForgePolicy is how fresh the forge facts a gather returns must be. It
// MOVED here from app: Gather is its consumer, and app importing publish
// for the type it passes in is the right direction. cycle and Promote
// are ForgeRefresh; status is ForgeAsCached; the third thing a bool
// could not say — `status --no-update` asking nothing at all — is
// ForgeAsCached with nothing recorded, which Gather reports as
// Forge.Fresh false and Forge.AsOf zero.
type ForgePolicy uint8

const (
	ForgeUnset    ForgePolicy = iota // rule 7: nobody chose; Gather refuses it
	ForgeAsCached                    // serve what was recorded, with its AsOf; ask nothing
	ForgeRefresh                     // ask, and record what was said
)

// The steps a permit authorizes are record.StepKind, and this package
// declares NO Step of its own. A draft of this design had a publish.Step
// enum one import away from record.Step — the same-name collision this
// document calls its central theme, committed inside it for the third
// time — and the durable half of the pair was worse than a duplicate: a
// free string with no owner, in the one lifecycle whose recovery works
// by reading its own history back. The retire pass tells "pushed but no
// PR" from "PR opened" by matching those values, so an unowned spelling
// is a publication that cannot be resolved.

// Permit is permission to publish. Its fields are unexported and no
// other package can construct one, so an effect cannot run before the
// decision that allows it: Apply takes a Permit, and only Authorize
// returns one.
//
// granted is the last hole in that argument closed. Unexported fields
// stop a caller inventing a permit with steps in it; they do not stop
// `publish.Permit{}`, which every composite literal in Go may write —
// and a zero Permit reaching Apply would be an effect with no decision
// behind it. So the flag is set by Authorize and by nothing else, and
// Apply refuses a permit without it (ErrNoPermit). That is the same
// rule-7 shape Spend.counted carries, applied to the value whose zero
// is most dangerous rather than most expensive.
type Permit struct {
	granted bool
	facts   Facts
	steps   []record.StepKind
	running []string
	title   string
	body    string
}

func (p Permit) Steps() []record.StepKind { return append([]record.StepKind(nil), p.steps...) }
func (p Permit) Tip() string              { return p.facts.Tip }

// Change names the change this permit is for, so a caller reporting an
// outcome can key it the way the publication row is keyed.
func (p Permit) Change() record.ChangeID { return p.facts.Change.ID }

// Title and Body are what the pull request will say, resolved by Gather
// and carried here so Apply performs no rendering of its own: the bytes
// a permit was granted over are the bytes that go out.
func (p Permit) Title() string { return p.title }
func (p Permit) Body() string  { return p.body }

// Running reports verifications still in flight on the tip being
// published, as an ADVISORY. Publication does not stop them and has no
// way to: cancelling is a thing a person asks for, with its own verb.
//
// The reasoning is worth stating because an earlier draft of this
// design had publication cancel after the gates rather than before
// them, and that is a smaller fix than it looks. A publication is a
// multi-step distributed operation — push, then open a pull request —
// so it can ALWAYS discover a reason to stop after an irreversible step
// has landed. Ordering every gate above every effect is achievable only
// for the local decision boundary; promising it in general promises
// something the shape of the problem does not allow. Removing the
// incidental destruction is the real fix.
func (p Permit) Running() []string { return append([]string(nil), p.running...) }

// NoOp reports a permit with nothing to do: the change is already in
// front of reviewers at this tip. Apply on one performs and records
// nothing, and Cycle reports it as a row of kind NoOp rather than as a
// publication.
func (p Permit) NoOp() bool { return len(p.steps) == 0 && p.facts.Forge.OwnFound }

// Advisory is something the operator should be told that does not
// refuse: an open proposal, an unproven cohort member, a publication
// that will say it is unverified. Today these are printed by the gate
// as it goes, which is why a caller cannot count them.
type Advisory struct {
	Kind string
	Text string
}

// The advisory kinds, owned here because an advisory outlives the call
// that made it — a caller sorts, counts and renders them — and a
// vocabulary nobody owns is one a reader recovers by matching prose,
// which is rule 6's prohibition. The TEXT is what a person reads and
// nothing decides from; the KIND is what a caller may branch on.
const (
	AdviseUnverified = "unverified"      // the tip carries no pass, and the body will say so
	AdviseBlocked    = "blocked"         // a run never reached the change
	AdviseIgnored    = "ignored-verdict" // --ignore: publishing past a failure, and the body states it
	AdviseUnproven   = "unproven-member" // a cohort member published without a pass
	AdviseProposal   = "open-proposal"   // a finding nobody has answered
	AdviseSamePort   = "same-port-pr"    // somebody else's open pull request on this port
	AdviseForge      = "forge-silent"    // a forge question that went unanswered on the human road
	AdviseDrift      = "drift"           // the branch fell behind, or its portdir moved underneath it
	AdviseCrossing   = "leaving-stable"  // this change takes the port out of stable
	AdviseEpoch      = "epoch-owed"      // a downgrade base would refuse to install without an epoch bump
	AdviseRunning    = "running"         // a verification still in flight on the tip
)

// Direction is a macports.Movement read at publication, plus the one
// judgment publication makes over it. Eleven fields in a draft; four now,
// because Movement already carries the observation and EpochOwed is
// base's own predicate rather than a dockhand judgment over Backwards and
// EpochMoved.
type Direction struct {
	Movement macports.Movement
	// EpochOwed is Moved && !Upgrades: the version string changed and
	// base would skip the install — the downgrade that strands every
	// existing install. The human road emits the epoch edit and says so;
	// the machine road refuses. Computed here from the REALIZED delta, so
	// a planner that could not emit the edit still cannot publish
	// unattended.
	EpochOwed bool
	Err       error // the comparison could not be made — a refusal for a machine
	AsOf      time.Time
}

// Grant names what a machine may do, so that reading the field tells you
// the permission rather than requiring you to know what its absence
// means. The zero value grants nothing.
type Grant uint8

const (
	// GrantNothing is the zero value and the shipped default: a machine
	// may verify, mint and queue, and may not spend ring 3.
	GrantNothing Grant = iota
	// GrantSimpleBumps is the 2026-09-06 ruling: a machine may open a pull
	// request for a change change.Judge called Simple, which has passed
	// verification, and which is not held. THE HOLD IS BORN FROM THE
	// CROSSING (record.Crossing.WithholdsUnattended): a change is held
	// when it takes its port out of stable, and not otherwise.
	//
	// Two drafts got this wrong in opposite directions. The first said the
	// prerelease regex covered this road; measured, it sees 12% of the
	// 1,142 entries riding a commit hash, a datestamp or a prerelease
	// name. The second said PROVENANCE covered it — releases feed versus
	// tag — and that would have held 93% of GitHub ports, because 3,689 of
	// 3,962 fetch from tag-based archive or tarball links rather than a
	// releases feed. The crossing is neither: it cancels the heuristic's
	// blindness, because a misread style is misread identically on both
	// sides of a bump.
	GrantSimpleBumps
)

// Env is the effect surface: the exact set of writes a publication may
// perform. It is a concrete struct rather than an interface because
// there is one implementation and the calls are direct.
//
// THREE HANDLES AND TWO VALUES. The sketch declares the first three;
// the last two are what porting it against the shipped tree needs, and
// each is stated rather than smuggled in:
//
//   - Forge is a gh.Runner and not the sketch's *gh.Client. The shipped
//     gh package is a seam over one invocation (`func(ctx, args...)
//     (string, error)`) with free functions over it, which is what lets
//     a test hand in a scripted GitHub without mutating a global. The
//     sketch's Client is a placeholder for that seam, not a redesign of
//     it, and gh is not a package this step rebuilds.
//   - Eval is the evaluation seam Direction needs. Gather is documented
//     to read "Direction over upstream main's tip", which is a MacPorts
//     evaluation of one portdir at two commits, and publish holds no
//     evaluator and may not import one — the Tcl shell under
//     macports/eval is exactly what this package's import list exists to
//     keep out. So the consumer declares the narrow interface and app
//     implements it, which is run.Stager's and run.Local's precedent.
//   - Version is the running binary's, named in the pull request body's
//     sign-off so that a published sentence found to be wrong can be
//     traced to the build that wrote it. The body is rendered by Gather,
//     no record carries the value, and this is the only bag Gather is
//     handed.
type Env struct {
	Repo    *git.Repo
	Forge   gh.Runner
	State   *statestore.Store
	Eval    Evaluator
	Version string
}

// Evaluator is the narrow slice of a MacPorts evaluation this package
// needs: the identity — epoch, version, revision — a port evaluates to
// at one commit.
//
// It answers in macports.Identity and not in info.Values, and the
// narrowing is the point: the only question publication asks of an
// evaluation is how base would order two installs, Move is the function
// that answers it, and an interface returning the whole of a port's
// evaluated state would invite this package to start deciding from
// fields it has no business reading.
//
// A nil Evaluator is not a failure. It rides back as Direction.Err with
// Compared false — the machine road refuses on it, the human road is
// unaffected, since a person may publish a downgrade on "the operator
// typed it" — which is what rule 7 asks of a fact a decision reads.
type Evaluator interface {
	IdentityAt(ctx context.Context, rev, portdir string) (macports.Identity, error)
}
