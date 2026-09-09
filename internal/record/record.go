// Package record is the durable shape of what dockhand knows about a
// change, and its codec. It decides nothing: no promotability, no
// eligibility, no interpretation of a provider status. Those live with
// the domain that owns the question.
//
// Declaration order is wire order: encoding/json emits struct fields
// as declared, and this is also status --json's public surface, so
// reordering a field here moves the bytes of every note and of every
// golden that re-marshals one.
package record

import "time"

// Schema versions the NOTE, which is a derived export: a build that
// cannot read one clears it and regenerates from the store.
const Schema = 4

// DocSchema versions the four documents in the state ref. IT IS A
// TRIPWIRE AND NOT A MIGRATION, and an earlier draft of this comment had
// it the other way round — an additive-only discipline, a migrate-on-read
// inside Amend, and a refusal for documents from a newer peer.
//
// That was compatibility scaffolding for software that has shipped
// nothing. dockhand is pre-release: the ruling is that we optimize for
// ease of refactoring and do not care about previous data formats, so a
// shape change is a shape change and the state ref is recreated. A
// migrator would be a permanent tax on every future edit, paid to
// preserve records nobody has promised anybody.
//
// WHAT THE NUMBER STILL BUYS is one comparison, and it is worth keeping
// for a rule-7 reason rather than a compatibility one: encoding/json
// ignores fields it does not know and zero-fills the ones it is missing,
// so reading a document written before a shape change SILENTLY SUCCEEDS
// and yields a wrong answer — a lease with no owner, a crossing that
// reads as unknown. Refusing on the version turns that into a stated
// refusal a person can act on.
//
// THE ONE THING RESETTING COSTS, and it is operational rather than
// archival: clearing the ref discards LIVE leases, so every environment
// the machine currently holds is leaked with nothing left to name it.
// The remedy is a drain and not a migrator — reconcile, let the pass
// discharge what it owes, then recreate. Anything else in the ref is
// re-derivable from the branches and the forge.
const DocSchema = 1

// ContentID is a digest over the complete file set a change writes.
// It is what binds a proof to the bytes that earned it, and it is
// durable because a reader a week later must be able to tell whether
// the evidence on this note describes the tip it is attached to.
type ContentID string

// Driver is who an act was carried out by — a person, or dockhand
// running unattended.
//
// The two uses of this type are not the same thing, and keeping them
// verbally apart is the whole of the ruling behind it. A RECORDED
// Driver — Change.AskedBy, Publication.By, PublicationState.PublishedBy
// — is provenance and nothing else: no gate reads one back, and it
// exists so that a later question about how a change reached review is a
// query rather than an estimate. A Driver PASSED as an invoker is the
// opposite: it is a decision input, and the publish gates turn on it.
// Feeding a recorded one into a gate would let a change authorize
// itself by claiming its own history, which is why the invoker always
// arrives as a parameter at the call site that decides.
//
// Which one a value is is never inferred either. A run's invoker is
// declared — --auto, DOCKHAND_AUTO — and dockhand never asks whether a
// terminal is attached to work it out.
type Driver string

const (
	// Human is a person's own act, and the invoker of every verb that
	// did not declare otherwise.
	Human Driver = "human"
	// Machine is dockhand acting unattended: what a run in auto mode
	// mints is recorded as asked by the machine, and the one unattended
	// publish road publishes as it.
	Machine Driver = "machine"
)

// Record is one note on one mint commit. It is a struct of four
// sections because dockhand tracks four lifecycles, and each section
// names the package permitted to advance it. The note stays one
// object, and therefore one atomic git write; ownership is expressed
// in the mutation API, not in the file layout.
type Record struct {
	Schema int    `json:"schema"`
	Sha    string `json:"sha"`
	Tree   string `json:"tree"`

	Change Change `json:"change"` // owner: internal/change
	// Runs on the exported note are a PROJECTION of the change's
	// attempts, flattened to (port, platform) for a person reading the
	// commit. The authority is Attempt.Runs in the state ref; this is
	// the readable copy, and nothing decides from it.
	//
	// The projection is MANY-TO-ONE and the collapse rule has to be
	// stated or two builds of one port silently fight over a key: take
	// the attempts whose Sha is this commit, and for each (member,
	// platform) keep the most recently STARTED attempt that reached a
	// terminal verdict. A retry therefore replaces its predecessor in the
	// note while both survive in the store, which is the right way round
	// — the note is what a person reads, and the store is what happened.
	//
	// Both halves of the key come off the attempt itself: the member port
	// from the Runs map, and the platform from Attempt.Platform. An
	// earlier draft had the platform only on the attempt's LEASE, so the
	// exporter would have had to split lease.Slot's joined string to
	// recover it — a hand-written scanner over a joined string, which is
	// the exact defect RunKey was made a struct to kill. And a QUEUED
	// attempt has no lease at all, so the queued projection this section
	// promises could not have been written.
	Runs        map[RunKey]Run    `json:"runs,omitempty"`
	Leases      map[string]Lease  `json:"leases,omitempty"`      // owner: internal/lease, keyed by platform
	Publication *PublicationState `json:"publication,omitempty"` // owner: internal/publish
}

// RunKey is a pair, not a string. The current "port@release" spelling
// is parsed back out by three call sites with a hand-written scanner,
// which is a struct wearing a string's clothes.
type RunKey struct {
	Port     string `json:"port"`
	Platform string `json:"platform"`
}

// ChangeID identifies a change for its whole life. It is minted when
// the change record is first written — which may be BEFORE a mint, for
// a prepared revision a gate refused — and it never changes.
//
// It is not the branch name, not the slug and not the mint sha, and the
// reason is that each of those is either absent at some point in the
// life or reusable across two different changes. A branch deleted and
// recreated for a later change of the same port would share a slug and
// a branch name; a publication keyed on either would be reattached to
// work it has nothing to do with. The branch is a BINDING on a change,
// not its identity.
type ChangeID string

// ChangeState is where a change has got to, and it exists because
// nothing else in this struct can say. A draft of this design named the
// five states in prose, gave the store a Compact that drops "closed"
// records, and then had no field for closed to land in — so changes,
// the one kind every other kind is keyed on, could never be dropped and
// the tree grew for the life of the repository. SupersededBy covers one
// branch of the enum and nothing covered the others.
//
// THERE IS NO STATE BETWEEN THE RECORD AND THE REF. A draft had two —
// ChangePrepared (minted, ref not yet created) and ChangeExtending
// (extended, ref not yet moved) — each the honest name for a crash
// window between an Amend and the ref move that followed it. R23 closed
// the window: change.MintIn and change.ExtendIn queue the ref line into
// the SAME update-ref batch as the record (statestore.Txn.Ref), so a
// change is Minted the moment it exists and Extended the moment the
// record says so, and a state that could only be reached by dying is
// not a state. Two open states, four closed.
type ChangeState string

const (
	// The constants carry the type's name because record already spells a
	// RUN's supersession as Superseded, and two Superseded constants in
	// one package is precisely the weak identity this design is about.
	ChangeMinted   ChangeState = "minted"
	ChangeExtended ChangeState = "extended"
	// The last four are the closed states. Compact may drop a change in
	// one of them, and only then.
	ChangeSuperseded ChangeState = "superseded"
	ChangeDiscarded  ChangeState = "discarded"
	ChangePublished  ChangeState = "published"
	// ChangeAbandoned is a change that was minted, failed, and that nobody
	// is going to carry further. It exists because without it a minted
	// change whose verification failed stays ChangeMinted forever: nothing
	// moves it, Closed() is false, and Compact may never drop it. That was
	// inert while nothing counted changes — a stale branch was just a stale
	// branch — and it stops being inert the moment a budget does, because
	// every permanently-failing port then holds a slot for good.
	ChangeAbandoned ChangeState = "abandoned"
)

// Closed reports that a change's life is over, which is the single
// predicate Compact tests. It is a method rather than a set literal at
// the call site so that adding a state is a compile-time visit here
// rather than a silent omission there.
func (s ChangeState) Closed() bool {
	return s == ChangeSuperseded || s == ChangeDiscarded ||
		s == ChangePublished || s == ChangeAbandoned
}

// Crossing is how a change's version moved with respect to stability. It
// is a MINT-TIME fact, recorded on the change, and it is separate from
// publish.Facts.Direction — which asks whether the version moved FORWARD
// and must be recomputed at publication because a tip can move. Two
// questions at two moments, which is rule 2.
//
// The distinction the tree does not currently draw: internal/engine/
// mint.go:359 and :383 both test only verdict.Prerelease(TARGET), never
// the version the port already rides. So a port going 2.0 -> 2.1-rc1 —
// a genuine change of posture — and one going 2.1-rc1 -> 2.1-rc2 — where
// the posture judgment was made by whoever put it on a prerelease — are
// announced identically, on every update, forever. The concept already
// exists one layer up and never reaches the mint:
// internal/upstream.PrereleaseLateral (upstream.go:68-74) is exactly
// "the port itself rides prereleases".
//
// THE FROM SIDE IS THE EVALUATED VERSION, info.Values.Version, and
// naming it is not pedantry — a draft left it undefined and the two
// candidate strings give OPPOSITE answers on real ports. The bump path's
// "current version" is the CARRIER LITERAL (internal/intent/bump/bump.go:142,
// `moving := carrier.Text(src) != b.Version`), i.e. the raw text at the
// version span. Three ports in the tree — math/gts, science/gerris,
// science/gfsview — carry `version 0.7.6-20${snapshot}`, and the
// heuristic fires TRUE on that literal because it matches the Tcl
// VARIABLE NAME, while the evaluated value 0.7.6-20121130 reads false.
// One port, one predicate, two answers, decided by which string the
// caller happened to hold. Where the evaluated version is unavailable the
// answer is CrossingUnknown, which is what rule 7's slot is for.
//
// THE HOLD IS BORN FROM THIS, not from a test on the target alone, and
// that fixes a contradiction live in the tree today. internal/upstream
// resolves PrereleaseLateral because "alpha to alpha gives up no
// stability, so resolution proceeds... field-measured on amber-lang,
// whose only possible update path a stricter rule had closed"
// (upstream.go:68-74) — and internal/engine/mint.go:359 then tests
// verdict.Prerelease(TARGET) and holds exactly that move. dockhand
// resolves a port's only available update and then withholds it. That is
// D28, and this ruling closes it.
//
// THE HUMAN ROAD NEVER REFUSES ON THIS. A maintainer asking for a
// release candidate by name is asking for a legitimate thing, and
// dockhand does not second-guess a typed version — mint.go's own comment
// says so. What StableToPrerelease earns is a WARNING: the operator is
// told they are taking the port out of stable, once, at the moment they
// do it — and Warns is that telling. The MACHINE road refuses the same
// crossing, through WithholdsUnattended below. One value, read twice for
// two different purposes: a sentence to a person, a gate on a machine.
type Crossing string

const (
	// CrossingUnknown is the zero value: the comparison could not be made
	// — an unparseable current version, an evaluation that failed. Rule 7.
	CrossingUnknown    Crossing = ""
	StableToStable     Crossing = "stable"
	StableToPrerelease Crossing = "leaving-stable" // the one that warns
	PrereleaseLateral  Crossing = "prerelease-lateral"
	PrereleaseToStable Crossing = "returning-to-stable"
)

// Warns reports the crossing a person should be told about as it
// happens. Only one does, and naming it as a method rather than testing
// the constant at the call site means adding a crossing is a
// compile-time visit here.
func (c Crossing) Warns() bool { return c == StableToPrerelease }

// WithholdsUnattended is the 2026-09-06 ruling on what a machine may
// publish, and it is the WHOLE of the prerelease condition: a change is
// born held when it takes its port OUT of stable, and not otherwise.
//
//	StableToStable      allowed — 99.7% of real moves
//	PrereleaseLateral   allowed — already prerelease, following upstream up
//	PrereleaseToStable  allowed — always; it is a move TOWARDS stable
//	StableToPrerelease  HELD    — the only hard gate
//	CrossingUnknown     HELD    — rule 7; see below
//
// This settles what ruling 1's "non-prerelease version bump" means. It
// is not "the target is not a prerelease" — that reading held 93% of
// GitHub ports once the provenance table was priced, and it is the
// reading that produces the contradiction below. It is "the change does
// not leave stable".
//
// MEASURED over 14,639 real version moves in macports-ports across
// twelve months: 14,596 StableToStable, 26 PrereleaseLateral, 9
// PrereleaseToStable, and 8 StableToPrerelease. This refuses those 8 and
// admits 99.945%. Three of the 8 are regex false positives on -devel
// ports whose FROM side is misread, so they err toward refusal.
//
// WHY THE HEURISTIC'S BLINDNESS DOES NOT REACH THIS. verdict.Prerelease
// misreads a port's versioning STYLE, not individual versions — it sees
// 12% of the 1,142 entries riding a commit hash, a datestamp or a
// prerelease name. It therefore misreads BOTH SIDES of a bump the same
// way, and the misread CANCELS: Stable(20250920) && !Stable(20260101) is
// true && false, so no gate fires. That cancellation is the structural
// reason a crossing works where a target test cannot, and it is not
// luck.
//
// CrossingUnknown is held, and the ruling did not name it: mine, under
// rule 7. "I could not compare" is not "it did not leave stable", and a
// machine must not publish what it could not classify.
func (c Crossing) WithholdsUnattended() bool {
	return c == StableToPrerelease || c == CrossingUnknown
}

type Change struct {
	Schema int         `json:"schema"`
	ID     ChangeID    `json:"id"`
	State  ChangeState `json:"state"`
	// Branch is the branch this record binds while Bound(), and the name
	// it HAD afterwards. It is never cleared: a closed record that forgot
	// its branch could not say "was dockhand/foo, deleted" or "kept under
	// --keep-merged" to `status`, and the release of the NAME for reuse is
	// Bound() and not an empty field — one predicate where a draft had
	// CloseIn and SupersedeIn each blanking the field so MintIn's
	// ErrStanding check would pass. Whether a kept name still stands in
	// git is git's fact, observed (HasBranch, Resolve) and judged by the
	// create line; the record does not judge it twice.
	Branch string `json:"branch,omitempty"`
	// Tip is the commit the record names, and while Bound() it is what
	// the ref — Branch, or Pin — holds, BY CONSTRUCTION: the batch that
	// wrote this record moved the ref to it, or asserted it. There is no
	// state under which the record names a commit its ref does not carry;
	// a draft had two, and they are gone with the window that made them.
	// A ref that no longer holds Tip was moved by a foreign hand, which
	// change.Resolve reports as ErrTipDisagrees and never trusts either
	// way.
	Tip string `json:"tip,omitempty"`
	// Pin is the ref keeping a BRANCHLESS record's Tip alive —
	// change.PinRef(ID) — and empty for a record with a Branch. It is a
	// field and not a rule ("Branch empty and MintedVia Adopted") because
	// change.CloseIn queues a delete line for it with Tip as the expected
	// value, and a delete line for a ref that does not exist refuses the
	// whole batch: the record has to SAY whether a pin exists (rule 7).
	// change.PinLostIn clears it when a hand has already deleted the ref.
	Pin string `json:"pin,omitempty"`
	// Slug is the name the branch was minted under, written from the
	// plan that named it. It is recorded rather than read back out of
	// the branch name, because parsing a branch name is a guess and the
	// value that produced it is right here.
	Slug string `json:"slug,omitempty"`
	// Content is the digest over the complete file set this change
	// writes: what binds the evidence on this note to the bytes that
	// earned it.
	Content ContentID `json:"content"`
	// Subjects are the members of the change, in build order.
	// Subjects[0] is the headline: the port the change is about, the one
	// a refusal names and the one the branch is named for. More than one
	// is a cohort.
	Subjects []Subject `json:"subjects,omitempty"`
	// Destination is how far this change's contract reaches, recorded
	// when it was minted rather than inferred later from what happens to
	// be running.
	Destination Destination `json:"destination,omitempty"`
	// Unverified is "no build was asked for" — --no-verify — recorded
	// because the DESTINATION STOPPED BEING A PROXY FOR IT and a reader
	// downstream cannot recover it any other way.
	//
	// It used to be readable off Destination: --no-verify wrote ToBranch,
	// so ToPublished with no attempts could only mean a machine that had
	// no environment to submit to. Once --no-verify and --to-pr composed,
	// that inference broke in the worst way available — it published "no
	// verification environment on the submitting machine" as a fact about
	// a machine holding two provisioned bases, in a pull request body, to
	// reviewers. Absence of a run has two causes and the record has to
	// know which one it was (rule 7).
	Unverified bool `json:"unverified,omitempty"`
	// AskedBy is who asked for that destination. It is provenance and
	// never an input to any gate — the ladder's arithmetic counts human
	// and unattended promotions apart, and a field that could widen what
	// the machine is allowed to do would be an authorization.
	AskedBy Driver `json:"asked_by,omitempty"`
	// Agent is the AI agent marker, when one was set in the
	// environment. Provenance only, on the same terms as AskedBy:
	// recorded so a later question about how a change reached review can
	// be answered by a query instead of an estimate, and read by nothing
	// that decides anything.
	Agent string `json:"agent,omitempty"`
	// MintedVia says whether this change came from a deliberate single
	// target, from a sweep over many, from a cohort — or from no mint of
	// dockhand's at all.
	MintedVia MintedVia `json:"minted_via,omitempty"`
	// Crossing is recorded, not merely announced, so that `status` and a
	// reviewer reading the note can see that this change took its port out
	// of stable — a warning nobody was at the terminal for is no warning.
	Crossing Crossing `json:"crossing,omitempty"`
	// Hold is a brake on this change, with its origin and the reason
	// given. A pointer because "not held" and "held for no stated
	// reason" are different facts.
	Hold *Hold `json:"hold,omitempty"`
	// Riders are the discovered todos folded into the change's own
	// commit — a modeline insertion and its kin. They are named here
	// because the pull request body vouches for what the note remembers
	// and not for what the diff can be re-read to contain.
	Riders []string `json:"riders,omitempty"`
	// Findings are what verification noticed that nobody asked about:
	// an ABI change, the dependents that would need a revision bump, an
	// instruction comment in the Portfile. A finding proposes and never
	// executes, which is what Disposition is for.
	Findings []Finding `json:"findings,omitempty"`
	// ClosesTicket is the ticket this change closes, carried to the
	// commit's trailer and the pull request's body.
	ClosesTicket string `json:"closes_ticket,omitempty"`
	// SupersededBy names the newer sibling's branch, written on the
	// older record when a port-keyed supersede takes its place. It is one
	// of the two ways a record gives its name up; see Bound.
	SupersededBy string `json:"superseded_by,omitempty"`
	// Closed is when the change entered a closed state; Compact's tail is
	// measured from here.
	Closed *time.Time `json:"closed,omitempty"`
	// Base is the commit the change was minted on top of, with the time
	// that commit was made. Both halves are needed by different readers:
	// the sha is the honest "before" a baseline is measured at, and the
	// time is how a reader tells a change written against a week-old
	// tree from one written against today's.
	Base Base `json:"base,omitzero"`
}

// Bound reports that this record's ref is its own: the branch or pin
// holds Tip, the name is not free, and change.DemolishIn may not delete
// it. False for a closed record and for one a newer sibling superseded
// while its publication stayed open (change.SupersedeIn) — the two ways
// a record gives its name up. ONE predicate, read by change.MintIn's and
// change.AdoptIn's ErrStanding check, by change.Standing, by
// change.Resolve when two records carry one branch name (names are
// reused; ids are not), by run.Pending's Superseded reason, by
// change.SupersedeIn's ErrNotBound refusal, and by change.DemolishIn's
// refusal. A method so that a new way of giving a
// name up is a compile-time visit here.
func (c Change) Bound() bool { return !c.State.Closed() && c.SupersededBy == "" }

// Destination is how far the MACHINE may carry a change, and it has
// TWO values. ToVerdict is deleted: a draft used it to mean "a
// verification was asked for", and under always-enqueue that question is
// answered by the attempt's existence and by nothing on the change — so
// `verify` on a --no-verify branch enqueues and flips nothing, and the
// shipped askVerdict (which flipped the destination so the drain would
// admit the run) has no successor. run.Pending's NoDestination refusal
// went with it: an attempt's existence IS the ask. The zero value is
// unset and change.MintIn refuses it, so a caller cannot mint a change
// bound for nowhere by forgetting the field.
type Destination string

// The three are a LADDER, and they are the tool's own three verbs: bump
// stops at a branch, verify stops at a verdict, promote goes all the
// way. Each names where the change is meant to STOP, which is the only
// question a reader of this field ever has.
//
// ToVerdict was missing, and its absence made ToBranch a lie. Every
// ordinary bump was recorded ToBranch beside every --no-verify one, so
// the word meant less than its own documentation said and anything
// reading it as "--no-verify" was reading a fact that is not there. The
// publish body did exactly that: it told reviewers a branch had been
// minted with a flag nobody typed, and shadowed the honest sentence for
// the case it had actually met.
const (
	// ToBranch is --no-verify: mint the branch and stop. An adoption is
	// bound here too — a person pointing at work that already exists has
	// asked for nowhere further.
	ToBranch Destination = "branch"
	// ToVerdict is the default: mint the branch and get it built. Where
	// it goes after that is a person's call, which is why this is not
	// ToPublished.
	ToVerdict Destination = "verdict"
	// ToPublished carries the change through to a pull request.
	ToPublished Destination = "published"
)

// MintedVia says how a change came to exist. It is the field the
// ladder's arithmetic turns on — human promotions of sweep-minted
// changes are its numerator — so it is recorded rather than inferred
// afterwards from a branch name.
type MintedVia string

const (
	// MintedSingle is a change the user named, one target per run.
	MintedSingle MintedVia = "single"
	// MintedSweep is a change a sweep proposed on its own.
	MintedSweep MintedVia = "sweep"
	// MintedCohort is a change minted around a headline and the
	// dependents a finding proposed with it.
	MintedCohort MintedVia = "cohort"
	// MintedAdopted is a change dockhand did not mint: a dockhand/ branch
	// with no record (hand-made, or made before the state ref was
	// recreated) that `verify` met, or a working tree `verify <portdir>`
	// snapshotted so the attempt it queues has a sha to be built from.
	// A machine may never demolish one — Discard's machine road tests
	// this value and not a sentence — because dockhand did not make the
	// thing it would be deleting. It is what makes D22's last road
	// trackable without giving a pass the right to eat it.
	MintedAdopted MintedVia = "adopted"
)

// Subject is one member of a change: a port, where it lives, and what
// was done to it.
//
// A cohort's members differ in every one of these, which is why they
// are a struct per member rather than parallel slices on the change. A
// dependent revision-bumped because its library's ABI moved carries a
// different intent, a different target and a different reason from the
// headline that caused it, and a record that flattened them would make
// the pull request body guess.
type Subject struct {
	// Port is the port's own name, as `port` would be given it.
	Port string `json:"port"`
	// Names is the port and its subports — every name a build log can
	// blame that belongs to this member.
	//
	// It exists for the subport-vs-parent blame guard: a cohort log that
	// fails on py312-foo must map to the member that owns it, and a
	// reader matching on Port alone would find no member and blame a
	// stranger, or blame nobody.
	//
	// It is written as [Port] even when the port has no subports at all,
	// because the empty slice already means something else: a reader
	// cannot otherwise tell "this port has no subports" from "nobody
	// ever asked".
	Names []string `json:"names,omitempty"`
	// Portdir is the <category>/<port> directory the change touched, on
	// the host. It is what gets staged ahead of the environment's own
	// ports tree.
	Portdir string `json:"portdir,omitempty"`
	// Intent is what was done — bump, refresh, bump-revision. It is per
	// member and not per change: a cohort's headline is a version bump
	// and its members are revision bumps, in one commit series.
	Intent string `json:"intent,omitempty"`
	// Target is what this member moved to: a version ("1.9"), a
	// re-derivation ("checksums"), a revision ("rev2").
	//
	// It is recorded so that deciding whether one change supersedes
	// another is a comparison of two values rather than a parse of two
	// branch names.
	Target string `json:"target,omitempty"`
	// Reason is why this member is in the change, in the words that
	// reach the commit body — for a cohort member, the criterion the
	// finding measured.
	Reason string `json:"reason,omitempty"`
}

// Hold is the brake on a change, and it now says WHO SET IT, because
// the two producers withhold different acts. A draft wrote the crossing's
// born-hold as the same value a person's `hold` writes, and run.Start
// and run.Pending both refused a held change — so `bump amber-lang --to
// 0.3.2-alpha` enqueued its attempt and then refused its own
// opportunistic start, and no drain ever started it until `unhold`,
// which also lifted the publication hold. cli_spec flow 10 (ruled) shows
// that bump SUBMITTED with "the hold is on publication, not on the
// build", and the shipped mint says the same in as many words. Origin is
// what makes the two reaches typed rather than sentences (rule 6).
type Hold struct {
	Origin HoldOrigin `json:"origin"`
	Reason string     `json:"reason"`
	At     time.Time  `json:"at"`
}

// HoldOrigin is who set a hold, and therefore what it withholds. The
// zero value is unknown and change.Held treats it as withholding
// EVERYTHING (rule 7): a hold record nobody stamped an origin on is a
// wiring gap, and a machine must not publish or delete past a gap.
//
//	HoldPerson    withholds verification, publication and deletion —
//	              the hold verb's contract. Verify and Accept refuse at
//	              resolve (exit 23); run.Pending refuses it as Held;
//	              every publication road refuses it; demolish refuses it.
//	HoldCrossing  withholds ONLY the unattended acts: publish.Authorize's
//	              machine road and the machine's demolish (app's
//	              mayDemolish, before change.DemolishIn). It
//	              never withholds a build, and the human road is warned
//	              rather than refused. It is born in change.MintIn from
//	              Crossing.WithholdsUnattended and nowhere else.
//
// `unhold` clears either.
type HoldOrigin string

const (
	HoldUnknown  HoldOrigin = ""
	HoldPerson   HoldOrigin = "person"
	HoldCrossing HoldOrigin = "crossing"
)

// WithholdsVerification is the one reach a crossing hold does NOT have.
// A method rather than a comparison at the four call sites, so a new
// origin is a compile-time visit here; the unknown origin withholds, by
// rule 7.
func (o HoldOrigin) WithholdsVerification() bool { return o != HoldCrossing }

// WithholdsUnattended reports whether a machine may act past this hold.
// No origin permits it: a person's hold and a crossing's both withhold
// the unattended acts, and the unknown origin withholds by rule 7. It
// exists so the machine road's question has one answer beside the human
// road's, rather than a comparison somebody narrows later.
func (o HoldOrigin) WithholdsUnattended() bool { return true }

// Base is the commit a change was minted on top of.
type Base struct {
	Sha         string    `json:"sha"`
	CommittedAt time.Time `json:"committed_at"`
}

// Finding is something verification noticed that nobody asked about:
// a library whose ABI moved, the dependents that would need a revision
// bump, an instruction comment in the Portfile, an upstream statement
// about compatibility.
type Finding struct {
	Kind FindingKind `json:"kind"`
	// Diverged is the portdir-relative paths a reconstruction found the
	// tip does not match, for KindStealth. Data and not a sentence: the
	// pass that raises this hold is unattended, so the only account a
	// person gets is what is written here.
	Diverged []string `json:"diverged,omitempty"`
	// Ports are the ports the finding is about.
	Ports []string `json:"ports,omitempty"`
	// Candidates are the ports it examined, with what it concluded
	// about each.
	Candidates []Candidate `json:"candidates,omitempty"`
	// Criterion is the measurement in words a reader can check: which
	// install name moved, which compatibility version changed, between
	// which two builds on which platform. The mechanical criterion is
	// necessary and never sufficient, so it is stated rather than
	// implied.
	Criterion string `json:"criterion,omitempty"`
	// Source and Quote are where a non-mechanical finding came from and
	// what it actually said — an upstream release note, a comment in the
	// Portfile. A finding that cannot be traced back to its words is an
	// assertion.
	Source string `json:"source,omitempty"`
	Quote  string `json:"quote,omitempty"`
	// Disposition and At carry no omitempty. A finding with no
	// disposition on the wire would read as one nobody had to answer,
	// and the zero value of the type is not one of the three words.
	Disposition Disposition `json:"disposition"`
	At          time.Time   `json:"at"`
}

// FindingKind is an enum, not a free string. It is declared here
// because the note is the only thing that outlives the process that
// wrote it; a presenter that invents a kind writes a note a later
// build cannot classify.
type FindingKind string

// THE VALUES ARE THE PLANNERS' OWN WORDS, and two of them were not.
// A plan-time finding arrives here as a string and change.stamp converts
// it through an exhaustive table, refusing anything else with
// ErrUnknownFinding — and MintIn returns that error, so the whole Amend
// writes nothing: no branch, no record. The two kinds the tree actually
// produces are intent.FindingInstruction ("instruction-comment") and
// bump.FindingPatchesUnchecked ("patches-unchecked"), and NEITHER was
// spelled here. `dockhand bump` therefore failed outright on any port
// carrying a maintainer instruction comment, and on any port whose
// patches could not be checked — measured on the real tree: 49 and 69
// Portfiles respectively.
//
// It is the defect this file's own doc invites. "A presenter that
// invents a kind writes a note a later build cannot classify" is the
// right rule and it is enforced in the right place; what nothing checked
// was the other direction — that every word a planner writes is a word
// this table admits. internal/cli's finding_test.go is that check now.
const (
	KindABIDependents FindingKind = "abi-dependents"
	// KindInstruction was "instruction" and the planners have always
	// written "instruction-comment": intent.FindingInstruction, the rule
	// named throughout internal/intent, and the spelling plan.Finding's
	// own doc gives as "the vocabulary the note classifies by". The
	// record moved to the planners' word rather than the other way, so
	// the one place the word is argued for is the one that keeps it.
	KindInstruction FindingKind = "instruction-comment"
	// KindPatchesUnchecked is bump.FindingPatchesUnchecked: a bump that
	// fetched no distfile had nothing to check the port's patches
	// against. It carries Accepted and not Proposed, so it states a fact
	// and does not hold an unattended publication for an answer nobody
	// can give — see bump.patchesUnchecked.
	KindPatchesUnchecked FindingKind = "patches-unchecked"
	// KindPatchUnrelocated is bump.FindingPatchUnrelocated: a patch the
	// bump could not carry onto the new source by the one move it will
	// make — every hunk's before-block found once, verbatim, with only
	// its line numbers rewritten.
	//
	// It carries Proposed and not Accepted, which is the difference
	// between it and KindPatchesUnchecked above. "Nothing was fetched, so
	// nothing was checked" is a statement; "this patch does not carry
	// over and somebody has to look" is a QUESTION, and a question with
	// nobody on the unattended road to answer it is what
	// publish.Authorize's fourth gate exists for.
	KindPatchUnrelocated FindingKind = "patch-unrelocated"
	KindStealth          FindingKind = "stealth-change"
)

// Disposition is what has become of a finding. A finding proposes and
// never executes, so the proposal and the answer to it are two facts
// and this is the second one.
type Disposition string

const (
	// Proposed is a finding nobody has answered yet. It is the state a
	// finding is appended in, and an unanswered one is a question the
	// change is still carrying.
	Proposed Disposition = "proposed"
	// Accepted means the proposal was taken up — the cohort was built,
	// the revision bumped.
	Accepted Disposition = "accepted"
	// Dismissed means a person looked and said no. Dismissal is an
	// answer worth recording, not an absence: a finding that vanished
	// when declined would be proposed again on the next look.
	Dismissed Disposition = "dismissed"
)

// Candidate is one port a finding examined, whether or not the finding
// proposes doing anything to it.
//
// The ports the tool declined to touch are recorded beside the ones it
// proposes, because they are exactly what a reviewer must check by
// hand: a dependent excluded for being obsolete, replaced, or already
// in flight is a decision, and a decision no reader can see is a
// decision nobody can disagree with.
type Candidate struct {
	Port string `json:"port"`
	// Portdir is where it lives, when the finding knows.
	Portdir string `json:"portdir,omitempty"`
	// Proposed says the finding puts this port forward. A candidate
	// without it was looked at and left out.
	Proposed bool `json:"proposed,omitempty"`
	// Reason is why, either way.
	Reason string `json:"reason,omitempty"`
	// Solo says this member is bumped by the change but left out of the
	// cohort's own build, because a member already in it declares a
	// conflict and MacPorts will not activate both. What it must not
	// lose is its revision, which is why it stays proposed; what it is
	// not owed is a build of its own — the person is told it was
	// withheld, and that is the answer.
	Solo bool `json:"solo,omitempty"`
	// Over names the seated member this candidate lost its seat to —
	// the sibling it declares a conflict with, or that declares one
	// with it, spelled as the proposal spells that member. It is set
	// wherever Solo is set and is empty otherwise.
	//
	// It is a field and not a reading of Reason, because Reason is prose
	// for the person and this is a fact the tool acts on. A renderer that
	// rewords the sentence must not be able to change which port gets
	// deactivated, and it cannot once the name travels on its own.
	Over string `json:"over,omitempty"`
	// Forced says a person overrode the withholding: this candidate is
	// Solo — it would have been bumped and left out of the guest — and
	// was seated anyway, last, with the member Over names deactivated
	// before its own build. Set only where Solo and Over are set.
	//
	// It is on the candidate and not only on the run because the run is
	// written when the cohort is submitted, and a cohort accepted with
	// --no-verify is never submitted at that moment: the person's
	// override would live nowhere but in the reason's prose, and a hand
	// `dockhand verify` of the branch afterwards would have to parse the
	// sentence or drop the ask. The resubmission roads read this and
	// Solo instead — a withheld member stays out of the guest and a
	// forced one goes in last — with the run, where there is one, saying
	// the same thing.
	Forced bool `json:"forced,omitempty"`
}
