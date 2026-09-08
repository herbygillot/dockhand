package change

import (
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Minting is what MintIn writes: everything the record needs to say
// about a change whose commit exists and whose branch the same batch
// creates. It is a struct rather than eight parameters so that the
// caller who forgets one gets a zero the mutator can refuse (Tip,
// Content, Branch and Destination empty: ErrIncomplete) rather than a
// positional slip nobody sees.
type Minting struct {
	ID          record.ChangeID // caller-minted, before any effect (rule 3)
	Branch      string          // the NAME; the batch creates the ref
	Tip         string          // the unreferenced commit Commit wrote
	Content     record.ContentID
	Subjects    []record.Subject
	Crossing    record.Crossing // Cross(p); MintIn derives the born-hold from it
	Destination record.Destination
	Closes      string
	Findings    []plan.Finding
	Riders      []string
	Slug        string
	Base        record.Base
	Prov        Provenance
}

// ErrIncomplete is MintIn's, ExtendIn's, AdoptIn's and FollowIn's
// refusal of a value with a required field empty. A change record
// naming no commit is the branch-with-no-record defect from the other
// side, and one bound for nowhere is a --to-pr the machine slot can
// never see.
var ErrIncomplete = errors.New("change: a required field of the minting is empty")

// ErrUnknownFinding is the stamp refusing a plan-time finding whose kind
// this build cannot classify, and it is the gate plan.Finding's own doc
// promises: "a kind or a disposition this tree does not know can be
// refused" at "the one place a plan-time finding becomes a durable one".
//
// That place is here. A plan lives for one process and states its kind
// as a word, because a planner runs with a Portfile and a parse tree in
// hand and no store; the NOTE outlives the process that wrote it, so its
// kinds are an enum. Converting at one site is what keeps the enum's one
// gate where it belongs, and refusing at that site is what stops a
// vocabulary nobody owns from reaching the durable record (D14).
var ErrUnknownFinding = errors.New("change: a finding names a kind this build cannot classify")

// MintIn writes the change record in state ChangeMinted AND queues the
// line that creates its branch — tx.Ref(BranchRef(m.Branch), m.Tip, "")
// — in the SAME Amend as the attempts run.EnqueueIn writes for it. Two
// owned mutators, one transaction, and now one batch: the record, the
// attempt and the ref land together or not at all, which is rule 4's
// sentence applied to the default road and R23's applied to its ref.
// There is no window and no Prepared state.
//
// THE HOLD IS BORN HERE, from Crossing.WithholdsUnattended and nothing
// else, and it is written with Origin record.HoldCrossing: the machine
// road's whole prerelease gate is this one line, the human road is never
// refused on it — it is warned, by the caller reading Crossing.Warns off
// the value it passed in — and THE BUILD IS NEVER WITHHELD BY IT. A
// draft wrote this hold as the same value a person's `hold` writes, and
// run.Start refused it, so a stable-to-prerelease bump enqueued and then
// refused its own start (cli_spec flow 10, ruled, shows the attempt
// submitted). Destination is ToBranch under --no-verify and by default,
// ToPublished under --to-pr — two values, ToVerdict deleted — and
// Change never publishes whatever it says.
//
// IT MAKES TWO JUDGMENTS FOR TWO QUESTIONS (rule 2), and both stay:
//
//   - The record-level one, here: ErrStanding when any Bound() change in
//     tx.State() already carries this branch name — "a non-closed,
//     non-superseded change already binds this name", a PEER dockhand.
//     It sits inside the closure, under the flock, atomic with the write
//     it guards.
//   - The ref-level one, at the batch: the create line's old is "", so a
//     branch of this name that EXISTS WITH NO Bound() RECORD — hand-made,
//     minted before the state ref was recreated, or kept under
//     --keep-merged and since compacted — refuses the whole Amend with
//     git.ErrRefMoved naming it (exit 45; remedy: `verify` adopts it, or
//     `git branch -D` by hand). "A foreign hand created this ref" is not
//     the same finding as "a record binds this name", and a draft that
//     let the record's judgment stand for both would have minted over a
//     person's branch or refused a free name. There is no third judge
//     for a kept branch: whether a name still stands in git is git's
//     fact, and the create line asks it.
//
// It refuses ErrIncomplete for an empty Tip, Content, Branch or
// Destination. The body is written out because the Ref line is the
// claim.
func MintIn(tx *statestore.Txn, m Minting, now time.Time) (record.Change, error) {
	if m.ID == "" || m.Tip == "" || m.Content == "" || m.Branch == "" || m.Destination == "" {
		return record.Change{}, ErrIncomplete
	}
	findings, err := stamp(m.Findings, now)
	if err != nil {
		return record.Change{}, err
	}
	for _, c := range tx.State().Changes {
		if c.Branch == m.Branch && c.Bound() {
			return record.Change{}, ErrStanding
		}
	}
	if err := tx.Ref(BranchRef(m.Branch), m.Tip, ""); err != nil {
		return record.Change{}, err
	}
	c := record.Change{
		ID: m.ID, State: record.ChangeMinted, Branch: m.Branch, Tip: m.Tip,
		Slug: m.Slug, Content: m.Content, Subjects: m.Subjects,
		Destination: m.Destination, AskedBy: m.Prov.AskedBy, Agent: m.Prov.Agent,
		MintedVia: m.Prov.Via, Crossing: m.Crossing, Riders: m.Riders,
		Findings: findings, ClosesTicket: m.Closes, Base: m.Base,
	}
	// The born hold, from the crossing and from nothing else. Its origin
	// is what keeps it out of the build's way: HoldCrossing withholds the
	// unattended acts and never verification, so the attempt this same
	// transaction enqueues starts as the person asked.
	if m.Crossing.WithholdsUnattended() {
		c.Hold = &record.Hold{
			Origin: record.HoldCrossing,
			Reason: crossingHoldReason(m.Crossing),
			At:     now.UTC(),
		}
	}
	tx.PutChange(c)
	return c, nil
}

// crossingHoldReason is the sentence a born hold carries, spelled once
// because it is written onto the record AND said to the person, and a
// note whose reason differed from the announcement would be two accounts
// of one act. It names the crossing rather than the target version: the
// judgment was made about a MOVE, and a reason quoting only where the
// port is going would invite the target-alone test the crossing replaced.
func crossingHoldReason(c record.Crossing) string {
	if c == record.CrossingUnknown {
		return "the version movement could not be classified, so no machine may publish it; a person decides"
	}
	return "this change takes the port out of stable; a person decides whether MacPorts should follow it"
}

// stamp turns the plan's findings into the record's, at one moment and
// at one site. The kind is converted here — plan states a word, the
// record keeps an enum — and a word this build cannot classify is
// ErrUnknownFinding rather than a durable finding nobody can read back.
//
// The clock is the caller's, handed down from the mint's one read, so a
// record whose hold predated its own findings by a microsecond cannot
// exist. The judgment itself has no clock: a finding is made from bytes
// and says nothing about when, so the moment recorded is the moment the
// record learned it.
func stamp(in []plan.Finding, at time.Time) ([]record.Finding, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]record.Finding, 0, len(in))
	for _, f := range in {
		kind, ok := findingKind(f.Kind)
		if !ok {
			return nil, ErrUnknownFinding
		}
		d, ok := disposition(f.Disposition)
		if !ok {
			return nil, ErrUnknownFinding
		}
		out = append(out, record.Finding{
			Kind:        kind,
			Ports:       f.Ports,
			Candidates:  candidates(f.Candidates),
			Criterion:   f.Criterion,
			Source:      f.Source,
			Quote:       f.Quote,
			Disposition: d,
			At:          at.UTC(),
		})
	}
	return out, nil
}

// findingKind is the plan's word as the record's enum. The table is the
// whole conversion and it is exhaustive by refusal: a word not in it is
// no kind at all, which is what makes the enum's guarantee — that every
// kind on a note is one a later build can classify — hold at the only
// place a word becomes one.
func findingKind(word string) (record.FindingKind, bool) {
	switch word {
	case string(record.KindABIDependents):
		return record.KindABIDependents, true
	case string(record.KindInstruction):
		return record.KindInstruction, true
	case string(record.KindStealth):
		return record.KindStealth, true
	}
	return "", false
}

// disposition is the plan's word as the record's enum, with the empty
// word reading Proposed: a planner that states nothing has made a
// question, which is what a finding is unless it says otherwise.
// Dismissed is deliberately unreachable from here — a dismissal is a
// person's answer to a finding that already exists, written onto the
// record long after the plan is gone.
func disposition(word string) (record.Disposition, bool) {
	switch word {
	case "", plan.Proposed:
		return record.Proposed, true
	case plan.Accepted:
		return record.Accepted, true
	}
	return "", false
}

// candidates carries a plan's candidates into the record's shape. The
// plan-time form knows only the port and why it is here; Portdir,
// Proposed, Solo, Over and Forced are what a MEASUREMENT and a person's
// answer add later, through ProposeIn and AnswerIn, so they are absent
// here rather than guessed.
func candidates(in []plan.Candidate) []record.Candidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]record.Candidate, 0, len(in))
	for _, c := range in {
		out = append(out, record.Candidate{Port: c.Port, Reason: c.Reason})
	}
	return out
}

// ErrStanding is MintIn refusing to write a change for a branch name a
// Bound() change already carries — the record-level half of a pair whose
// ref-level half is the create line (git.ErrRefMoved). Both are needed
// because they answer different questions — a record binds the name; a
// foreign ref stands at it — and a road that met the second without the
// first would report a person's branch as a dockhand race. Exit 11.
var ErrStanding = errors.New("change: a change already stands for this branch")

// ErrTipMoved is ExtendIn refusing because the change's recorded Tip is
// not the one the caller extended from: an Accept that planned against
// tip A must not land on a record a concurrent Accept moved to B — a
// PEER, fixed by re-resolve and rerun (exit 11, the plan's problem; the
// rerun then meets ErrNoProposal). The ref-level twin — the branch
// itself no longer at the tip the record holds — is git.ErrRefMoved from
// the same batch's update line, and Resolve's ErrTipDisagrees before it.
var ErrTipMoved = errors.New("change: the tip moved under this extension")

// ErrBranchless is ExtendIn's and FollowIn's refusal of a record with no
// Branch: a snapshot has nothing to move, and an extension of it would be
// a mint.
var ErrBranchless = errors.New("change: the change has no branch to extend")

// Extension is what ExtendIn writes for Accept: the cohort commit's sha
// and the members it adds. Everything NOT here is carried across
// unexamined — hold, superseded-by, base, crossing, destination — which
// is the invariant that Extend never moves a version: a cohort commit
// re-declares dependents, it does not bump anything, so the facts a mint
// established stand. That invariant is why a person's commit is NOT an
// extension: FollowIn is its own mutator.
type Extension struct {
	ID record.ChangeID
	// ExpectedTip is the tip the caller planned against — what Resolve
	// returned — and ExtendIn refuses ErrTipMoved when the record does
	// not hold it AND queues the branch line with it as Old: the
	// record-level judgment and the ref-level one from one field. A
	// struct field rather than a positional string so that a caller who
	// forgets it hands the mutator an empty value it refuses.
	ExpectedTip string
	Tip         string // the cohort commit, parent = ExpectedTip
	Content     record.ContentID
	Subjects    []record.Subject // unioned onto the record's
	Prov        Provenance
}

// ExtendIn advances a Minted or Extended change to ChangeExtended with
// the new Tip AND queues the branch's update line — tx.Ref(BranchRef
// (c.Branch), e.Tip, e.ExpectedTip) — in the same Amend as AnswerIn and
// the cohort's run.EnqueueIn. It refuses ErrTipMoved when tx.State()
// .Changes[id].Tip is not e.ExpectedTip, ErrBranchless for a record with
// no Branch, ErrIncomplete for an empty ExpectedTip, Tip or Content, and
// ErrNoRecord for a change that is not Bound() — an extension of a
// closed one is a resurrection, of a superseded one a fork nobody asked
// for.
//
// THERE IS NO COMPENSATION, because there is no window. A draft ran the
// record's Amend, then a CASRef, and needed RevertExtendIn for the case
// where the ref lost: the record said Extending with a Tip the branch
// never took. With the update line in the batch, a branch a person moved
// between Resolve and here refuses the whole Amend with git.ErrRefMoved,
// the record is untouched, the cohort commit is garbage, and Accept
// reports exit 45: nothing to revert, nothing to withdraw.
func ExtendIn(tx *statestore.Txn, e Extension, now time.Time) (record.Change, error) {
	if e.ExpectedTip == "" || e.Tip == "" || e.Content == "" {
		return record.Change{}, ErrIncomplete
	}
	c, ok := tx.State().Changes[string(e.ID)]
	if !ok || !c.Bound() {
		return record.Change{}, ErrNoRecord
	}
	if c.Branch == "" {
		return record.Change{}, ErrBranchless
	}
	if c.Tip != e.ExpectedTip {
		return record.Change{}, ErrTipMoved
	}
	if err := tx.Ref(BranchRef(c.Branch), e.Tip, e.ExpectedTip); err != nil {
		return record.Change{}, err
	}
	c.State, c.Tip, c.Content = record.ChangeExtended, e.Tip, e.Content
	c.Subjects = union(c.Subjects, e.Subjects)
	tx.PutChange(c)
	return c, nil
}

// union grows a change's membership: the members it already has, in the
// order it already has them, then the ones it did not.
//
// Old order first, and the head untouched. Subjects[0] is the headline —
// the port the branch is named for and the one a refusal names — so a
// union that let an arriving member sort ahead of it would rename the
// change.
//
// An arriving member that names a port already present does not replace
// it. A subject minted with a portdir, an intent and a target is the
// good copy. What an arrival may do is fill a blank: stating what a
// member lives in takes nothing away.
func union(old, add []record.Subject) []record.Subject {
	out := make([]record.Subject, 0, len(old)+len(add))
	at := make(map[string]int, len(old))
	for _, s := range old {
		at[s.Port] = len(out)
		out = append(out, s)
	}
	for _, s := range add {
		i, ok := at[s.Port]
		if !ok {
			at[s.Port] = len(out)
			out = append(out, s)
			continue
		}
		fillBlanks(&out[i], s)
	}
	return out
}

// fillBlanks states what the standing subject did not say, and
// overwrites nothing it did.
func fillBlanks(into *record.Subject, from record.Subject) {
	if len(into.Names) == 0 {
		into.Names = from.Names
	}
	if into.Portdir == "" {
		into.Portdir = from.Portdir
	}
	if into.Intent == "" {
		into.Intent = from.Intent
	}
	if into.Target == "" {
		into.Target = from.Target
	}
	if into.Reason == "" {
		into.Reason = from.Reason
	}
}

// FollowIn is the record following a branch a PERSON moved: a Bound()
// change whose branch a hand `git commit` carried past the recorded Tip
// (Resolve's ErrTipDisagrees, not Absent). Tip := found, Content := its
// tree, State ChangeExtended, and an ASSERT line tx.Ref(BranchRef
// (c.Branch), found, found) so the commit refuses if the branch is no
// longer where the person left it (measured: a no-op update with a wrong
// old is refused). It is NOT ExtendIn with a different expectation: an
// extension is a commit dockhand wrote whose parent is the recorded tip,
// under the invariant that hold, crossing and destination carry across
// because Extend never moves a version; a person's commit can move the
// version, so that invariant is false here and the mutator is its own
// (rule 2: two questions, two functions). It carries hold, crossing and
// destination across unexamined too — but SAYS so: a follow is an
// adoption of a person's work onto an existing record (Prov.Via
// MintedAdopted), and the person asked for it. No road calls it
// unasked: `verify <branch>` on a diverged change is the ask. It refuses
// ErrIncomplete for found == "" (a deleted ref is discard's case),
// ErrBranchless, ErrNoRecord for a change not Bound(), and ErrTipMoved
// when the record already holds found (nothing to follow).
func FollowIn(tx *statestore.Txn, id record.ChangeID, found string, content record.ContentID, prov Provenance, now time.Time) (record.Change, error) {
	if found == "" || content == "" {
		return record.Change{}, ErrIncomplete
	}
	c, ok := tx.State().Changes[string(id)]
	if !ok || !c.Bound() {
		return record.Change{}, ErrNoRecord
	}
	if c.Branch == "" {
		return record.Change{}, ErrBranchless
	}
	if c.Tip == found {
		return record.Change{}, ErrTipMoved
	}
	if err := tx.Ref(BranchRef(c.Branch), found, found); err != nil {
		return record.Change{}, err
	}
	c.State, c.Tip, c.Content, c.MintedVia = record.ChangeExtended, found, content, record.MintedAdopted
	tx.PutChange(c)
	return c, nil
}

// Adoption is a change dockhand did not mint and now tracks: a dockhand/
// branch with no record, or a working tree `verify <portdir>` is asked
// about. Branch is empty for the second, and Tip is then the snapshot
// commit Snapshot wrote — unreferenced until AdoptIn's pin line names it.
type Adoption struct {
	ID       record.ChangeID
	Branch   string // empty for a working-tree snapshot
	Tip      string
	Content  record.ContentID
	Subjects []record.Subject
	Base     record.Base
	Prov     Provenance // Via is forced to MintedAdopted
}

// AdoptIn writes a record for a change that already exists in git — or
// exists only as a snapshot object — with MintedVia Adopted, in state
// ChangeMinted, and queues the ref line that makes the record true:
//
//	Branch set    tx.Ref(BranchRef(a.Branch), a.Tip, a.Tip) — an ASSERTION.
//	              The ref exists and holds Tip; the line does not move it,
//	              it makes the batch refuse if a foreign hand moved it
//	              between Resolve's read and this commit. Identity before
//	              effect, applied to a ref dockhand did not create.
//	Branch empty  tx.Ref(PinRef(a.ID), a.Tip, "") — CREATE the pin, and
//	              write record.Change.Pin, so the snapshot's commit is
//	              reachable from the moment its record exists and not one
//	              moment earlier. A draft pinned BEFORE the record, inside
//	              Snapshot, and needed a compact sweep for the pin a crash
//	              left recordless; that population is unrepresentable now.
//
// It is Verify's begin for both of its subjects and the one road that
// writes a BRANCHLESS record, which is how D22 closes on its last road.
// It refuses ErrIncomplete for an empty Tip or Content, and ErrStanding
// for a Branch a Bound() record already carries. A machine may never
// demolish what this writes; Discard's machine road tests MintedVia and
// not a sentence.
func AdoptIn(tx *statestore.Txn, a Adoption, now time.Time) (record.Change, error) {
	if a.ID == "" || a.Tip == "" || a.Content == "" {
		return record.Change{}, ErrIncomplete
	}
	for _, c := range tx.State().Changes {
		if a.Branch != "" && c.Branch == a.Branch && c.Bound() {
			return record.Change{}, ErrStanding
		}
	}
	c := record.Change{
		ID: a.ID, State: record.ChangeMinted, Branch: a.Branch, Tip: a.Tip,
		Content: a.Content, Subjects: a.Subjects, Base: a.Base,
		AskedBy: a.Prov.AskedBy, Agent: a.Prov.Agent,
		// Forced, and not taken from the caller: whatever a road believes
		// about where this came from, dockhand did not mint it, and the one
		// field a machine's demolish tests must be the truth about that.
		MintedVia: record.MintedAdopted,
		// A destination is where the MACHINE may carry a change, and
		// nobody asked this one to go anywhere: an adoption is a person
		// pointing at work that already exists, so it is bound for the
		// branch it is already on and a road that wants more says so with
		// its own verb.
		Destination: record.ToBranch,
	}
	// one line, one call site: the name and the expected-old are chosen
	// first (assert the branch, or create the pin), so the census counts
	// six sites for six mutators.
	name, old := BranchRef(a.Branch), a.Tip
	if a.Branch == "" {
		c.Pin = PinRef(a.ID)
		name, old = c.Pin, ""
	}
	if err := tx.Ref(name, a.Tip, old); err != nil {
		return record.Change{}, err
	}
	tx.PutChange(c)
	return c, nil
}

// ErrNotAnAnswer is AnswerIn refusing a disposition that is not one: an
// answer is Accepted or Dismissed, and Proposed is the question. The
// sketch is silent here and the codebase's convention is not — a
// cross-package refusal is a sentinel — so it is one rather than a
// zero-valued write nobody could tell from an answered finding.
var ErrNotAnAnswer = errors.New("change: a disposition that is not an answer")

// AnswerIn is the writer record.Disposition had none of (F5): it moves
// one Proposed finding to Accepted or Dismissed, with the candidates as
// amended — Accept's --exclude/--force-withheld are amendments over the
// candidate list, and the record must carry what was actually seated so
// run.Roster can read it back. Accepted for the cohort finding (and for
// instruction findings, the positive side F5 named) is written by Accept
// in the same Amend as ExtendIn and EnqueueIn; Dismissed is written by
// the dismiss verb straight from cli, because it advances one lifecycle
// (R22).
//
// It refuses a finding not currently Proposed: an answer is given once,
// and a second dismiss over a dismissed proposal is a no-op a person
// should be told about, not a silent rewrite.
func AnswerIn(tx *statestore.Txn, id record.ChangeID, kind record.FindingKind, d record.Disposition, candidates []record.Candidate, now time.Time) error {
	if d != record.Accepted && d != record.Dismissed {
		return ErrNotAnAnswer
	}
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	findings := append([]record.Finding(nil), c.Findings...)
	for i := range findings {
		if findings[i].Kind != kind || findings[i].Disposition != record.Proposed {
			continue
		}
		findings[i].Disposition = d
		findings[i].At = now.UTC()
		if candidates != nil {
			findings[i].Candidates = append([]record.Candidate(nil), candidates...)
		}
		c.Findings = findings
		tx.PutChange(c)
		return nil
	}
	return ErrNoProposal
}

// ProposeIn writes a Proposed finding onto a change — the revbump
// cohort dependents.Propose produced, an instruction a maintainer's
// comment asked for — as a transaction step, because Findings are the
// change's and the producer is run.Finish's propose step, which writes
// it in the SAME Amend as the verdict (run.SettleIn) and the release
// claim (lease.RequestIn): three owned mutators, one transaction. An
// adversarial pass found the Accept road READING this finding off the
// record with nothing in the spine WRITING it; shipped settle does that
// work inside its own function (internal/engine/settle.go findCohort),
// and the design had split observe from judge without moving the
// gathering to the observer's side. It replaces a Proposed finding of
// the same Kind rather than accumulating them — a later verification's
// proposal supersedes an earlier one nobody answered — and never touches
// one already Accepted or Dismissed: an answer given is not re-asked.
func ProposeIn(tx *statestore.Txn, id record.ChangeID, f record.Finding, now time.Time) error {
	if f.Kind == "" {
		return ErrIncomplete
	}
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	f.Disposition = record.Proposed
	f.At = now.UTC()
	findings := append([]record.Finding(nil), c.Findings...)
	for i := range findings {
		if findings[i].Kind != f.Kind {
			continue
		}
		if findings[i].Disposition != record.Proposed {
			// An answer given is not re-asked. Nothing is written, and that
			// is the answer rather than a refusal: a later pass reaching the
			// same measurement is ordinary, and the person already spoke.
			return nil
		}
		findings[i] = f
		c.Findings = findings
		tx.PutChange(c)
		return nil
	}
	c.Findings = append(findings, f)
	tx.PutChange(c)
	return nil
}

// ErrNotHeld is UnholdIn's refusal of a change nothing is holding. It is
// a refusal rather than a silent success for the reason `dismiss` is:
// the verb was asked to release something, there was nothing to release,
// and reporting success would tell a script that a hold it believed in
// had just been lifted.
var ErrNotHeld = errors.New("change: the change is not held")

// HoldIn is the `hold` verb's one write: a record.Hold with Origin
// HoldPerson, the reason, and who. It is a *Txn mutator reached through
// app.Hold — one lifecycle, so an ENTRY POINT and not an operation (R22 as
// ruled 2026-09-07: cli may never import a lifecycle package) —
// and the sibling AnswerIn's doc promised. It refuses a change already
// held by a person (a second hold is a no-op a person should be told
// about) and REPLACES a crossing's hold, since a person's hold withholds
// strictly more and the reason a person typed is the one `status`
// should show.
func HoldIn(tx *statestore.Txn, id record.ChangeID, reason string, by record.OwnerID, now time.Time) error {
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	if c.Hold != nil && c.Hold.Origin == record.HoldPerson {
		return ErrHeld
	}
	c.Hold = &record.Hold{Origin: record.HoldPerson, Reason: reason, At: now.UTC()}
	tx.PutChange(c)
	return nil
}

// UnholdIn is the `unhold` verb's one write, and the shape F5 said the
// negative side lacked: it clears the hold whatever its Origin — a
// person's or a crossing's — because cli_spec flows 6 and 10 (ruled)
// show `unhold` lifting the born-hold, which is what makes the typed
// origin the form that matches the surface rather than the alternative
// of Authorize reading Crossing directly. It refuses a change that is
// not held.
func UnholdIn(tx *statestore.Txn, id record.ChangeID, now time.Time) error {
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	if c.Hold == nil {
		return ErrNotHeld
	}
	c.Hold = nil
	tx.PutChange(c)
	return nil
}

// ErrPublicationOpen is CloseIn's refusal: a merged or open pull request
// outlives the branch that carried it, so a change with an unsettled
// publication is not the change's own to close.
var ErrPublicationOpen = errors.New("change: a publication of this change is still open")

// CloseIn moves a change into one of its closed states — Superseded (by
// a newer sibling, named), Discarded, Published, Abandoned — stamps
// Closed, and, for a record whose Pin is set, queues the line that
// deletes its pin: tx.Ref(c.Pin, "", c.Tip). The pin's life IS the
// record's open life, so the one writer of every closed state is the one
// deleter of every pin, and no road can close a snapshot and leave its
// pin behind — the population a draft's compact sweep existed for. It
// does NOT touch the branch: a branch outlives its change under two
// policies (ReportOnly, WithholdDeletion) and dies under the others, so
// its deletion is DemolishIn's, chosen by the caller in the same
// closure. It does not clear Branch either: the record keeps the name it
// had as history, and Bound() is what releases it.
//
// It is called by Survey's Supersede and Change --replace (in the same
// Amend as the new change's MintIn), by Discard's close stage, by Cycle's
// close stage beside publish.RetireIn when the forge says merged, and by
// Cycle's snapshot sweep. Promote does NOT call it.
//
// It refuses with ErrPublicationOpen while a publication of this change
// is not Settled(), reading tx.State().Publications, ErrNoRecord for an
// id the state does not hold, and ErrNotBound for a record whose State is
// already Closed() — all pure functions of the state the closure was
// handed. The third is the record-level question rule 2 owes HERE: the
// roads that close read `old` outside the flock, and a PEER — a retire
// pass, a `bump --replace` in another worktree — can close it and delete
// its branch before this closure runs. Without the refusal the closure
// would restamp a Published record Superseded (a silent rewrite) and
// DemolishIn would queue a delete line for a branch the peer's batch
// already removed, which the batch refuses as git.ErrRefMoved with a
// remedy naming the person's own git for what was dockhand's own peer.
// The refusal is on Closed(), NOT on Bound(): a record SupersedeIn marked
// while its publication stayed open is still closable here, since
// retire closes it Published when the forge settles. The pin line's
// expected value is the record's Tip, so a pin a foreign hand moved
// refuses the close rather than deleting what it did not resolve; a pin
// a hand DELETED is PinLostIn's case, written first in the same closure
// so this queues no line for it.
func CloseIn(tx *statestore.Txn, id record.ChangeID, to record.ChangeState, by record.ChangeID, now time.Time) error {
	if !to.Closed() {
		return ErrNotAClosedState
	}
	state := tx.State()
	c, ok := state.Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	if c.State.Closed() {
		return ErrNotBound
	}
	for _, p := range state.Publications {
		if p.Change == id && !p.Outcome.Settled() {
			return ErrPublicationOpen
		}
	}
	if c.Pin != "" {
		if err := tx.Ref(c.Pin, "", c.Tip); err != nil {
			return err
		}
	}
	at := now.UTC()
	c.State, c.Closed = to, &at
	if by != "" {
		c.SupersededBy = string(by)
	}
	tx.PutChange(c)
	return nil
}

// ErrNotAClosedState is CloseIn refusing a state that is not one of the
// four closed ones. It is the writer's own guard on the enum it is the
// only writer of: a caller passing ChangeMinted here would be re-opening
// a change through the one function whose whole contract is ending one,
// and record.ChangeState.Closed is the predicate that settles it — a
// state added there is a compile-time visit there and a correct answer
// here, with no list to keep in step.
var ErrNotAClosedState = errors.New("change: that is not a closed state")

// SupersedeIn is what a change with an OPEN publication gets instead of
// CloseIn(Superseded): SupersededBy set to the new change and the State
// left OPEN — Minted or Extended, as it was. It exists because CloseIn keeps its refusal (a change with an
// open pull request does not die) and the two roads that replace a
// promoted change had nowhere to go: `bump --replace` on a branch whose
// PR is open dead-ended in Discard's refusal, and Survey's Supersede on
// a promoted sibling did the same, so the shipped replace-then-`promote
// --force` flow had no road. A first fix re-keyed the open publication
// onto the new ChangeID, which is wrong whenever --replace produces a
// DIFFERENT branch name — the common case, since the slug carries the
// version — because the open PR's head is the OLD branch, untouched by
// any push of the new one.
//
// So the old change stays open with its publication, its LOCAL branch's
// delete line is DemolishIn's in the same Amend (the fork copy kept,
// with the shipped advisory), and Cycle's close stage closes it later
// from the forge's word —
// ChangePublished on merged, ChangeAbandoned on rejected or withdrawn —
// exactly as it does for any other open publication. promote --force
// survives unchanged: Apply's own-PR refresh is keyed on the branch name
// it already revalidates, the only case a refresh can apply to. A change
// with NO open publication takes CloseIn(ChangeSuperseded) as the spine
// says. Either way run.WithdrawIn goes in the same Amend, so the old
// change's queued attempts are never drained, and run.Pending's
// Superseded ineligibility reads SupersededBy != "" and not the State —
// the old change is open and superseded at once. The name is released
// by Bound() reading SupersededBy, not by blanking Branch, so the record
// keeps the branch it had for `status` to name.
//
// THE CLOSURE'S ORDER on a replace or a Survey supersede is SupersedeIn
// or CloseIn, then run.WithdrawIn, then DemolishIn of the old local
// branch, then the new change's MintIn, then its run.EnqueueIn: the old
// name is released, and its ref's delete line queued, before MintIn's
// ErrStanding check and create line would meet it. One batch: the old
// branch's deletion and the new branch's creation land together, so a
// replace can never leave two branches or none, and a same-name replace
// is one coalesced update line (statestore.Txn.Ref). It refuses
// ErrNoRecord, and ErrNotBound for a record that is not Bound() — a peer
// already superseded or closed it between the road's read and this
// closure, and a second supersession would overwrite SupersededBy and
// mint a third change for a port that already has its successor.
func SupersedeIn(tx *statestore.Txn, old, by record.ChangeID, now time.Time) error {
	if by == "" {
		return ErrIncomplete
	}
	c, ok := tx.State().Changes[string(old)]
	if !ok {
		return ErrNoRecord
	}
	if !c.Bound() {
		return ErrNotBound
	}
	c.SupersededBy = string(by)
	tx.PutChange(c)
	return nil
}

// ErrNotBound is CloseIn's and SupersedeIn's refusal of a record a PEER
// has already closed or superseded between a road's read and its
// closure: the record-level twin of ErrStanding and ErrTipMoved (exit 11
// — re-resolve and rerun; the rerun finds no standing change, or the
// peer's). It is asked inside the closure over tx.State(), under the
// flock, which is what reserves the ref-level judge (git.ErrRefMoved,
// 45, "your own git moved this") for a hand: a peer is never reported as
// a foreign hand, and a closed record is never closed twice.
var ErrNotBound = errors.New("change: a peer already closed or superseded this change")

// ErrNotClosed is DemolishIn refusing a change that is still Bound(): no
// road can delete a branch the store still calls live. Since the closure
// runs CloseIn or SupersedeIn first and State() shows the Put, the
// refusal is against the state AS THIS CLOSURE LEFT IT, which is the
// order being enforced.
var ErrNotClosed = errors.New("change: the change is still bound; close or supersede it first")

// ErrCheckedOut is the refusal every road whose batch would MOVE or
// DELETE a branch returns when that branch is some worktree's HEAD,
// observed with git.Repo.CheckedOutAt BEFORE the Amend (rule 3; exit 46,
// BranchCheckedOut: switch away first): Accept's extend road for the
// update line ExtendIn queues, and the demolishing roads for the delete
// line. It replaces the guards `git branch -f` and `-D` owned as
// porcelain, and it is app's observation rather than this mutator's
// because a closure may not read git. FollowIn needs no guard: its line
// is an assert, and the worktree that holds the branch is the one that
// put the tip there.
var ErrCheckedOut = errors.New("change: the branch is checked out in a worktree")

// DemolishIn queues the deletion of a change's LOCAL branch —
// tx.Ref(BranchRef(c.Branch), "", c.Tip) — as a transaction step, in the
// same closure as the CloseIn or SupersedeIn that made it deletable. The
// expected value is the record's Tip, so an extension under a discard's
// feet, or a person's commit on the branch, refuses the WHOLE batch
// (git.ErrRefMoved) — the close included — and a discard never deletes
// work it did not resolve. That is stronger than the draft's Demolish,
// which ran after the close's Amend, outside every lock, and could
// refuse the deletion only after the record already said Discarded.
//
// It replaces Demolish and Demolition. Demolition's three bools each went
// somewhere: Local is whether the caller calls this at all (a policy —
// Retirement, or the discard verb — AND an observation: a road that
// resolved the branch Absent does not call it); Pin is gone, because
// CloseIn deletes the pin unconditionally; Fork is gone from this
// package, because a push-delete is a foreign effect and belongs to
// publish (record.DeleteFork). It refuses ErrNotClosed for a Bound()
// record, ErrNoRecord for a missing one, and ErrBranchless for a
// branchless record — there is no line to queue and a caller that asked
// for one is confused; a snapshot's pin is already CloseIn's. The
// machine road additionally refuses MintedVia Adopted and a change Held
// (ActDemolish, Machine), and those two refusals are app's (mayDemolish),
// not this function's: DemolishIn deletes what it is told, and who may
// tell it is app's decision — asked by app INSIDE the closure over
// tx.State() as well as before Cancel's stages, because a hold or a
// `verify` follow can land between a road's read and its Amend, and a
// captured bool from the earlier read would delete a branch a person
// has since held or continued (case 26).
func DemolishIn(tx *statestore.Txn, id record.ChangeID, now time.Time) error {
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	if c.Bound() {
		return ErrNotClosed
	}
	if c.Branch == "" {
		return ErrBranchless
	}
	return tx.Ref(BranchRef(c.Branch), "", c.Tip)
}

// ErrRefStands is PinLostIn refusing a disagreement that is not an
// absence: a moved pin is reported, never written over.
var ErrRefStands = errors.New("change: the ref was not observed absent")

// PinLostIn is the record catching up to a pin a hand DELETED: Pin is
// cleared, so the CloseIn that follows in the same closure queues no
// delete line for a ref that is not there (a delete of an absent ref
// refuses the whole batch — measured). It takes the OBSERVATION as a
// value (rule 5): the *TipDisagreement Resolve returned, and it refuses
// ErrRefStands unless d.Absent and d.Ref == c.Pin. Tip is left as it was
// — the record still names the commit, which the state ref's history
// keeps for statestore.PruneExpire's window — and nothing else changes:
// this is not a close, and no pass calls it; `discard <port>` is the ask.
// A branch a hand deleted needs no twin of this: the record's Branch is
// history either way, and the road simply does not call DemolishIn.
func PinLostIn(tx *statestore.Txn, id record.ChangeID, d *TipDisagreement, now time.Time) error {
	c, ok := tx.State().Changes[string(id)]
	if !ok {
		return ErrNoRecord
	}
	if d == nil || !d.Absent || d.Ref != c.Pin || c.Pin == "" {
		return ErrRefStands
	}
	c.Pin = ""
	tx.PutChange(c)
	return nil
}
