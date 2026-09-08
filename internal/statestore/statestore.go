// Package statestore is dockhand's operational store: the one dockhand-owned
// git ref that holds the records with no commit to live on, and the
// transaction that writes them together with the refs they imply.
//
// It is a small key-value store with atomic multi-key writes, which is a
// real thing and much less than a database. It deliberately has no
// indices, no query language, no planner and no joins. THE TRIPWIRE: if
// a read ever needs an index, the design has outgrown this store and
// should be reconsidered rather than patched — the failure mode of a
// hand-rolled store is that it acquires a query layer one convenience at
// a time, and the name says "store" rather than "db" so that nobody
// reads the package as an invitation to build one.
//
// It holds NO shapes. Every value it stores is a record type — the
// durable vocabulary has one home, and an earlier draft of this design
// grew a second Lease and a second SpecID here, which is the weak
// identity the whole document is about, committed inside it.
package statestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/record"
)

// Ref is the one ref this package owns. Its tip is an ordinary commit
// whose tree holds one JSON blob per record, FLAT, with the kind in the
// name: attempt-<id>.json, lease-<id>.json, publication-<id>.json,
// change-<id>.json.
//
// Flat rather than a directory per kind for a mechanical reason:
// git.GraftTree creates a missing top-level file but REFUSES a path
// whose parent directory is not already in the base tree
// (internal/git/git.go:461). A directory layout would make each kind's
// FIRST record unwritable, and permanently, since GraftTree is the only
// tree-writer in the package.
// It is scoped to the REPOSITORY — one ref, shared across linked
// worktrees through GIT_COMMON_DIR — and not to a port or a change,
// because the facts that must land together cross ports: a cohort is
// one lease shared by several, and the reclaim pass reads them all.
const Ref = "refs/dockhand/state"

// LogRef is the setting dockhand must ensure, and it is here because the
// consequence is unrecoverable. core.logAllRefUpdates covers refs/heads,
// refs/remotes, refs/notes and HEAD — even when explicitly set to "true",
// which is what the target repository has. It does NOT cover refs/dockhand.
// So the ref that is the AUTHORITY for live leases and open publications
// would be the one ref in the repository with no reflog and no recovery
// path: one stray `git update-ref -d`, one bad compare-and-set, and there
// is nothing to restore from. Only "always" logs it. Under R23 it also
// covers every create and update line the batch carries for
// refs/heads/dockhand/* and refs/dockhand/verify/*; a delete loses its
// reflog with the ref, and PruneExpire says what recovery is left.
const LogRef = "core.logAllRefUpdates=always"

// PruneExpire is the second and last git setting this design depends on,
// and unlike LogRef it is one dockhand READS rather than sets. Two facts
// hang on it, stated here once so neither is restated where it bites:
//
//   - An object between change.Commit (or change.Snapshot) and the batch
//     that names it is unreferenced. If the process dies there, it is
//     garbage — never a recorded state, never a recovery case, never a
//     stray a pass sweeps — and gc.pruneExpire's default of two weeks
//     collects it. The window is seconds; only a setting of `now` lets a
//     concurrent `git gc` eat an object a batch is about to name.
//   - A demolished branch's reflog dies with the ref (git.UpdateRefs),
//     so the closed record's Tip in this ref's history is the recovery
//     path, and it is a path for exactly as long as the object survives:
//     two weeks under the default, none under `now`. A queued attempt
//     whose branch a hand deleted lives on the same clock (run.Defer
//     types the loss when the stager cannot find the object).
//
// `doctor` reports a value shorter than the default. Nothing refuses on
// it: a person who set it chose it.
const PruneExpire = "gc.pruneExpire"

// Lock is the one advisory flock every durable dockhand write takes,
// at $GIT_COMMON_DIR/.dockhand-ledger.lock. Ruled 2026-09-06, and the
// ruling is FLOCK AROUND THE COMPARE-AND-SET rather than instead of it.
//
// ONE lock, covering the state ref AND the note export, and that is the
// point of naming it for the ledger rather than for either. A settle
// writes the state ref and then exports a note; two locks would be two
// domains with an ordering that nothing but a comment enforces, which is
// the exact objection section 11 raises against a second durable store.
// It resolves through GIT_COMMON_DIR, so linked worktrees share it — as
// they already share the ref itself.
//
// WHAT EACH HALF IS FOR:
//
//   - The lock excludes. Lost races go to zero, and with them the wasted
//     work (90% of successes at TWO concurrent writers, measured) and the
//     three unreachable objects each lost race leaves behind for the two
//     weeks gc.pruneExpire retains them. A contended writer also gets a
//     named refusal — lockfile.ErrHeld, "another dockhand holds the lock"
//     — after a deadline, instead of retrying in silence.
//   - The compare-and-set is the assertion that survives a writer who
//     never took the lock: a stray `git update-ref`, a future tool, an
//     older build. The ref is the AUTHORITY for live leases and open
//     publications, so a silent clobber there is the expensive kind.
//
// It costs one lock acquisition per write, and it costs a claim this
// design made and must now withdraw: that the store moves serialization
// "from a lock sitting beside the data into the data itself". It sits in
// both places. That is the honest arrangement and the sentence was the
// overreach.
const Lock = ".dockhand-ledger.lock"

// EmptyTree is git's well-known empty tree, the base of the first write
// in a repository that has never run dockhand.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// lockDeadline is how long a writer waits for a peer before it refuses
// with lockfile.ErrHeld. It is generous on purpose: the holder is
// usually mid-Amend, which is milliseconds, but an Amend behind a slow
// filesystem or a repository with a locked packed-refs is seconds, and a
// person's second terminal should wait those out rather than refuse.
// Past it the refusal is named, which is the whole of what the lock buys
// a contended writer over a silent retry.
//
// It is a var and not a const for one reason, and it is a test's: the
// refusal is a property worth proving, and proving it against thirty
// real seconds would put half a minute into every run of the suite. It
// is unexported, so no caller outside this package can move it, and
// nothing in this package writes it.
var lockDeadline = 30 * time.Second

// amendTries bounds the retry Amend promises. Under the Lock a lost race
// takes a writer who never took the lock at all — a stray `git
// update-ref`, an older build, a future tool — so one retry is already
// the uncommon case and a third is a peer that is writing in a loop
// without the lock. Bounded, then returned as ErrConcurrent: a retry
// that never gives up is a hang with a reason.
const amendTries = 3

// amendMessage is the subject every state commit carries. It is for the
// person reading `git log refs/dockhand/state`, which is the archive,
// and nothing reads it back — a fact recovered from a commit message is
// rule 6's prohibition. The trailing newline is the object's: CommitTree
// hands the message to git verbatim, so a message without one is a
// commit whose subject line is unterminated.
const amendMessage = "dockhand: amend\n"

// pinNamespace and branchNamespace are the two namespaces the store owns
// besides Ref itself, spelled here because Txn.Ref has to JUDGE a name
// and a judge with no vocabulary passes everything. The public spellings
// are change.PinRef and change.BranchRef — the lifecycle that owns the
// refs owns their names — and these are the store's own copy of the
// boundary, which is the one place a ref literal outside change may
// stand.
const (
	pinNamespace    = "refs/dockhand/verify/"
	branchNamespace = "refs/heads/dockhand/"
)

// The four kinds, as the flat tree names them. The kind is a PREFIX and
// the id is the rest, so a name is split by cutting the prefix rather
// than by cutting at the first hyphen — every id in this design carries
// hyphens of its own, and a scanner that cut at the first one would read
// change-chg-01HZ as the change "chg-01HZ" only by accident and a lease
// whose request token starts with a word as something else entirely.
const (
	changePrefix      = "change-"
	attemptPrefix     = "attempt-"
	leasePrefix       = "lease-"
	publicationPrefix = "publication-"
	docSuffix         = ".json"
)

// ErrNoState is what Read returns when the ref does not exist. It is a
// distinguishable error and not an empty State, because "dockhand has
// never run here" and "dockhand has run here and nothing is in flight"
// are the same value otherwise — and the difference matters most to the
// passes that DELETE things.
//
// That is D22's shape one level up: an absence read as a negative with a
// destructive act on the other side of it. Lose the ref — a stray
// `git update-ref -d`, a partial backup restored, a repository re-cloned
// onto a machine that still holds VMs from the old checkout — and Live()
// returns nothing, Owed() returns nothing, and the reclaim pass concludes
// that every environment on the machine is untracked.
//
// So: only an explicit first write may swallow this. Any pass that
// destroys a provider resource refuses on it and says it cannot account
// for anything on this machine.
var ErrNoState = errors.New("statestore: no state ref in this repository")

// ErrDocShape is the DocSchema tripwire firing: the ref holds a document
// this build does not read — another schema number, or a name that is
// not one of the four kinds.
//
// It is a REFUSAL AND NEVER A MIGRATION, which is record.DocSchema's own
// ruling carried to the one place that reads the documents. The remedy
// is to recreate the ref (a drain first: clearing it discards LIVE
// leases, so every environment the machine holds is leaked with nothing
// left to name it), and it is a sentinel because that remedy is a
// caller's to offer — `doctor` says it in words, and no pass may take it
// on its own.
//
// Refusing is what makes the version worth keeping at all: encoding/json
// ignores fields it does not know and zero-fills the ones it is missing,
// so reading a document from before a shape change SILENTLY SUCCEEDS and
// yields a wrong answer — a lease with no owner, a crossing that reads
// as unknown.
var ErrDocShape = errors.New("statestore: the state ref holds a document of another shape")

// State is one consistent read of the whole operational record.
//
// A draft of this design said it was "small by construction: a maintainer's
// tree carries tens of in-flight changes, not tens of thousands of rows",
// and used that to justify both the flat tree and choosing git over a
// database. Measurement did not support "by construction". N is
// Retention.ClosedFor times the closure rate, PLUS every open record — and
// a selector sweep over a twenty-thousand Portfile tree can enqueue tens of
// thousands of open attempts, none of which Compact may drop. Small is
// something the design has to ENFORCE, in two places: a cap on what one
// sweep may enqueue, and a retention window. Compact alone bounds only the
// closed tail.
//
// What the sizing does still justify is the absence of an index, and that
// survives for a different and better reason than smallness: with the batch
// read Read is required to use, fifty thousand records cost 444 ms.
//
// THE KEYS ARE THE RECORDS' OWN IDENTITIES, one per kind and stated once
// here because the tree's names are derived from them: a change by its
// record.ChangeID, an attempt and a publication by their ID, and a lease
// by its Request — the token the caller minted before the provider was
// called, which is what recovery joins a provider's inventory on and
// therefore the one name a lease is guaranteed to have.
type State struct {
	At           string // the commit this was read from
	Changes      map[string]record.Change
	Attempts     map[string]record.Attempt
	Leases       map[string]record.Lease
	Publications map[string]record.Publication
}

// Owed is every obligation this host may act on: a release claimed and
// not finished. Foreign owners are reported, never released — a
// repository copied to another machine must not stop a VM it does not
// own.
//
// Ownership is compared FIELD BY FIELD rather than with ==, because
// OwnerID carries a time.Time: a value decoded from JSON and one built
// in this process differ in their monotonic reading and their location
// pointer while naming the same instant, and == would report this
// checkout's own obligations as a stranger's — which is D20 with extra
// steps, since a foreign obligation is reported and never seized.
func (s State) Owed(me record.OwnerID) []record.Lease {
	var owed []record.Lease
	for _, key := range slices.Sorted(maps.Keys(s.Leases)) {
		if l := s.Leases[key]; l.Owed() && sameOwner(l.Owner, me) {
			owed = append(owed, l)
		}
	}
	return owed
}

// Live reports the leases in use right now, including a pre-mint gate's,
// which today has no record at all — which is why a concurrent reclaim
// pass destroys it.
//
// "In use" is everything the provider has not confirmed back, which is
// wider than Held: a lease whose release is owed still names an
// environment that exists, and one written Requested before the provider
// was ever called names one that MAY exist, which is the whole of rule 3
// and the reason that record is written first. Only a confirmed handback
// takes a lease out of this list.
func (s State) Live() []record.Lease {
	var live []record.Lease
	for _, key := range slices.Sorted(maps.Keys(s.Leases)) {
		if l := s.Leases[key]; !l.Returned() {
			live = append(live, l)
		}
	}
	return live
}

// sameOwner is OwnerID equality with the instant compared as an instant.
func sameOwner(a, b record.OwnerID) bool {
	return a.Root == b.Root && a.Host == b.Host && a.PID == b.PID && a.Since.Equal(b.Since)
}

var ErrConcurrent = errors.New("statestore: the state ref moved; re-read and retry")

// ErrForeignRef is Txn.Ref refusing a name outside the two namespaces the
// store owns — refs/heads/dockhand/ and refs/dockhand/verify/ — or the
// state ref itself, which only Amend's own first line may name. A
// mutator that could queue a line for refs/heads/master is a store that
// moves a person's branch, and the ruling's "only mover of the refs that
// are authority" is scoped to these three names exactly.
var ErrForeignRef = errors.New("statestore: a ref line names a ref the store does not own")

// Store is the handle the domains hold. Concrete, not an interface:
// there is one implementation, the calls are direct, and substituting it
// in a test means pointing it at a temporary repository.
type Store struct {
	repo *git.Repo
	// logged records that core.logAllRefUpdates has been ensured for this
	// repository. It is a per-Store memo and not a per-process one: two
	// Stores are two repositories, and the setting is a property of the
	// checkout whose refs it protects.
	logged bool
}

func Open(repo *git.Repo) *Store { return &Store{repo: repo} }

// Read returns the state at the ref's tip, or ErrNoState.
//
// IT MUST USE ONE LONG-LIVED `git cat-file --batch` SESSION, and that is a
// requirement rather than an optimization. Read has no index and returns
// the whole State, so it is O(N) — and with the per-blob plumbing the tree
// uses today (git.BlobAt shells out once per object, internal/git/git.go:226)
// it is O(N) SUBPROCESSES. Measured on this machine, git 2.55, APFS:
//
//	records     one --batch session     one subprocess per blob
//	    100          ~1 ms + spawn                      1.1 s
//	  1,000          ~9 ms + spawn                     10.8 s
//	  5,000            44 ms                            54   s
//	 50,000           444 ms                           539   s
//
// A ~30 ms process-spawn floor applies to both. So the difference between
// a store that scales to fifty thousand records and one that is unusable at
// a thousand is a single implementation decision — and internal/git's own
// package doc already names it: "the escalation is one long-lived
// `git cat-file --batch` session — the eval pattern — not a library."
//
// Do NOT address records as <tree>:<name>. git re-inflates the whole tree
// object on every path lookup, which makes Read quadratic in N.
//
// THE SESSION ANSWERS THE ABSENCE TOO, and it is why the tip is resolved
// through it rather than through RevParse: `cat-file --batch` reports an
// unresolvable request as a `missing` line and exits zero, so "there is no
// state ref" arrives as git.ErrNoObject and a session that broke arrives
// as its own error — where RevParse hands back "" with a non-nil error for
// both, and a reader that took that zero for absence would answer a
// transient git failure with "dockhand has never run here", which is the
// answer ErrNoState exists to keep away from the passes that delete
// things (rule 7).
//
// IT TAKES NO LOCK, and it does not need one. The tip is resolved once,
// and everything after that is addressed by OBJECT ID — the tree by the
// commit's own name, each record by the id its entry carries — so a peer
// that moves the ref mid-read is read past rather than read halfway: git
// objects are immutable, and the snapshot is the commit, not the ref. A
// read that took the Lock would serialize every `status` behind every
// write for a consistency it already has.
func (s *Store) Read(ctx context.Context) (State, error) {
	batch, err := s.repo.CatFile(ctx)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = batch.Close() }()

	tip, err := batch.Object(Ref + "^{commit}")
	if errors.Is(err, git.ErrNoObject) {
		return State{}, ErrNoState
	}
	if err != nil {
		return State{}, err
	}
	entries, err := batch.Tree(tip.OID + "^{tree}")
	if err != nil {
		return State{}, err
	}
	st := newState(tip.OID)
	for _, e := range entries {
		if e.Dir() {
			return State{}, fmt.Errorf("%w: %s is a directory", ErrDocShape, e.Name)
		}
		blob, err := batch.Object(e.OID)
		if err != nil {
			return State{}, fmt.Errorf("statestore: reading %s: %w", e.Name, err)
		}
		if err := st.absorb(e.Name, blob.Data); err != nil {
			return State{}, err
		}
	}
	return st, nil
}

// newState is an empty read: four maps that exist, so that a caller may
// range over a store holding nothing without asking whether it does.
func newState(at string) State {
	return State{
		At:           at,
		Changes:      map[string]record.Change{},
		Attempts:     map[string]record.Attempt{},
		Leases:       map[string]record.Lease{},
		Publications: map[string]record.Publication{},
	}
}

// absorb decodes one tree entry into the state, applying the DocSchema
// tripwire. An unknown NAME is refused under the same sentinel as an
// unknown schema number, because the two are one fact and have one
// remedy: the ref holds something this build did not write, and what a
// person does about it is recreate the ref.
func (s State) absorb(name string, data []byte) error {
	id, ok := strings.CutSuffix(name, docSuffix)
	if !ok {
		return fmt.Errorf("%w: %s is not a record", ErrDocShape, name)
	}
	switch {
	case strings.HasPrefix(id, changePrefix):
		return absorbDoc(s.Changes, name, strings.TrimPrefix(id, changePrefix), data, func(c record.Change) int { return c.Schema })
	case strings.HasPrefix(id, attemptPrefix):
		return absorbDoc(s.Attempts, name, strings.TrimPrefix(id, attemptPrefix), data, func(a record.Attempt) int { return a.Schema })
	case strings.HasPrefix(id, leasePrefix):
		return absorbDoc(s.Leases, name, strings.TrimPrefix(id, leasePrefix), data, func(l record.Lease) int { return l.Schema })
	case strings.HasPrefix(id, publicationPrefix):
		return absorbDoc(s.Publications, name, strings.TrimPrefix(id, publicationPrefix), data, func(p record.Publication) int { return p.Schema })
	}
	return fmt.Errorf("%w: %s is not a record this build writes", ErrDocShape, name)
}

// absorbDoc decodes one document and refuses one from another shape. The
// schema is read through a getter because Go cannot say "a type with a
// Schema field", and four one-line getters at the four call sites are
// cheaper than an interface every record type would have to implement to
// be storable.
func absorbDoc[T any](into map[string]T, name, id string, data []byte, schemaOf func(T) int) error {
	var doc T
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: %s does not parse: %w", ErrDocShape, name, err)
	}
	if got := schemaOf(doc); got != record.DocSchema {
		return fmt.Errorf("%w: %s is schema %d and this build reads only %d", ErrDocShape, name, got, record.DocSchema)
	}
	if id == "" {
		return fmt.Errorf("%w: %s names no record", ErrDocShape, name)
	}
	into[id] = doc
	return nil
}

// Txn is the write handle inside Amend, and it is a CAPABILITY rather
// than a bag. This is a correction, and the draft it corrects deserves
// stating because the mistake was the document's own opening indictment
// committed one level up.
//
// That draft's Amend took func(*State) error, and State's four maps are
// exported and mutable. So any package holding a *Store could write any
// field of any of the four lifecycles — which is exactly what section 1
// says is wrong with `Update(ctx, sha, func(*record.Record) error)`,
// widened from one lifecycle to four. Worse, it did not even buy the
// composition it cost: every lease mutator took a *Store and ran its own
// Amend, so run.Finish's settle Amend — the design's flagship "one write
// covers the verdict and the release obligation" — could not call one, and had to
// reach into the lease lifecycle's records itself.
//
// A Txn fixes the composition and NARROWS the blast radius to one kind
// per call. What it does not do is make the owner rule a compiler
// statement, and the document should not claim it does: Go cannot say
// "only package lease may call PutLease" without putting the caller in
// that package, and putting it there is what created the god-closure in
// the first place. So the rule is: each lifecycle package exports the
// mutators for its own kind, taking a *Txn (lease.ConfirmIn,
// lease.RequestIn), a cross-lifecycle write is spelled as two owned
// mutators in one transaction, and the Put* methods are called from the
// owning package and nowhere else — enforced by review and by one test
// that walks the AST for out-of-package callers. That is weaker than the
// type system and stronger than a build-order sentence, and saying which
// is the point.
type Txn struct {
	state State
	// changed is what the commit's tree will differ from its parent's
	// by: the entry name of every record a mutator touched, mapped to
	// whether the record stays. True is a write, false is a Drop, and
	// the two are one map because a name may be both in one closure —
	// dropped by Compact and re-put by nothing, or the reverse — and the
	// LAST word is the one the tree gets.
	changed map[string]bool
	// refs is what the commit will carry beside the state line: one
	// update-ref line per ref effect the closure's mutators implied, in
	// call order. Data, not an effect — which is why a line may be queued
	// inside a closure Amend may run twice: each run gets a fresh Txn and
	// queues its own lines, and only the run that commits moves anything.
	refs []git.RefUpdate
}

// State is the read side: what is actually in the ref right now, AS THE
// CLOSURE HAS LEFT IT — a Put is visible to the next mutator in the same
// closure, which is what lets change.CloseIn and change.DemolishIn agree
// on a change's state without a second read, lets SupersedeIn precede
// MintIn's ErrStanding check, and makes the order of mutators inside one
// closure a real order. The Put* stubs write into t.state's maps.
func (t *Txn) State() State { return t.state }

// Ref queues one ref line for the commit: create when old is "", delete
// when new is "", update — or assert, when new == old — otherwise. It is
// the ONLY way a lifecycle package reaches a ref, and it is a *Txn
// method for the reason the Put* methods are: the line lands in the same
// update-ref batch as the record that explains it, or not at all.
//
// It is called from exactly six mutators, each the owner of the
// lifecycle whose ref it is: change.MintIn (create the branch),
// change.ExtendIn (move it, old = the tip the record holds),
// change.FollowIn (assert it at the tip a person left), change.AdoptIn
// (create the pin, or assert an adopted branch's tip), change.CloseIn
// (delete the pin) and change.DemolishIn (delete the branch). Every line
// carries the caller's own expected-old, so the batch answers the
// ref-level question — did a foreign hand move this — beside the
// record-level one the mutator already answered: rule 2, two judges for
// two questions. check_callers.py counts the call sites: six, one per
// mutator — AdoptIn computes its name and expected-old first and calls
// once, so the count of sites IS the count of mutators.
//
// ONE COALESCING RULE: a delete of a name followed by a create of the
// same name in one closure becomes one update line — New from the
// create, Old from the delete — because `bump --replace` of a
// same-version bump re-mints dockhand/<port>-<version> (the shipped
// slug, engine/promote.go:786) in the transaction that supersedes the
// old change, and git refuses two lines for one ref in a batch
// (measured). No assertion is weakened: the delete's expected-old is
// kept. Any OTHER second line for one name is git.ErrBadUpdate — the
// closure's own bug.
//
// It refuses ErrForeignRef for any name outside the owned namespaces or
// the state ref itself, git.ErrBadUpdate for a line with neither value
// or an uncoalescable duplicate, and it refuses nothing else: whether
// the line is RIGHT is the batch's to say. It returns the error rather
// than deferring it to the commit, so the mutator aborts the closure
// typed (rule 6) and the store never judges a lifecycle's line after
// the closure has run (rule 1).
func (t *Txn) Ref(name, new, old string) error {
	switch {
	case name == Ref:
		return fmt.Errorf("%w: %s is the store's own line", ErrForeignRef, name)
	case !owned(name):
		return fmt.Errorf("%w: %s", ErrForeignRef, name)
	case new == "" && old == "":
		return fmt.Errorf("%w: %s names neither a new value nor an expected one", git.ErrBadUpdate, name)
	}
	for i, queued := range t.refs {
		if queued.Ref != name {
			continue
		}
		if queued.New == "" && old == "" {
			// The one coalescing rule: a delete then a create of the same
			// name is an update that keeps the delete's expected-old, so
			// the assertion the deleting mutator made still stands.
			t.refs[i] = git.RefUpdate{Ref: name, New: new, Old: queued.Old}
			return nil
		}
		return fmt.Errorf("%w: %s", git.ErrBadUpdate, name)
	}
	t.refs = append(t.refs, git.RefUpdate{Ref: name, New: new, Old: old})
	return nil
}

// owned reports a name inside one of the two namespaces a lifecycle may
// queue a line for. The prefix must be followed by something: the
// namespace itself is not a ref, and a caller that computed an empty id
// would otherwise be handed the whole namespace to delete.
func owned(name string) bool {
	for _, ns := range []string{pinNamespace, branchNamespace} {
		if rest, ok := strings.CutPrefix(name, ns); ok && rest != "" {
			return true
		}
	}
	return false
}

func (t *Txn) PutChange(c record.Change) {
	c.Schema = record.DocSchema
	put(t, changePrefix, string(c.ID), t.state.Changes, c)
}

func (t *Txn) PutAttempt(a record.Attempt) {
	a.Schema = record.DocSchema
	put(t, attemptPrefix, a.ID, t.state.Attempts, a)
}

func (t *Txn) PutLease(l record.Lease) {
	l.Schema = record.DocSchema
	put(t, leasePrefix, l.Request, t.state.Leases, l)
}

func (t *Txn) PutPublication(p record.Publication) {
	p.Schema = record.DocSchema
	put(t, publicationPrefix, p.ID, t.state.Publications, p)
}

// put is the four Put* methods' shared body: stamp the record into the
// state the closure reads, and mark its entry for the tree. It is a
// function rather than a method because Go has no generic methods, and
// the four Put* methods stay the API — the type parameter is how one
// body serves four maps, not something a caller ever writes.
//
// The schema is stamped by the Put and never trusted from the caller,
// which is record.Encode's discipline one store over: a document read
// back under this build's DocSchema and handed straight to a mutator
// carries whatever it was decoded with, and a record written out
// claiming a shape it does not have is the one thing the tripwire cannot
// catch later.
//
// An EMPTY ID is not refused here, because a Put returns nothing to
// refuse with. It is refused by Amend when the tree is built, which is
// the last moment before the record becomes a name — and refusing there
// means the whole transaction is refused, where a silent skip would
// write every other record of a closure that had already lost one.
func put[T any](t *Txn, prefix, id string, into map[string]T, doc T) {
	into[id] = doc
	t.changed[prefix+id+docSuffix] = true
}

// Drop removes one record from the tree. Only Compact calls it.
func (t *Txn) Drop(name string) {
	t.changed[name] = false
	if id, ok := strings.CutSuffix(name, docSuffix); ok {
		// The state the closure reads must lose it too: a Compact that
		// dropped a record and then read it back out of its own
		// transaction would count it twice.
		switch {
		case strings.HasPrefix(id, changePrefix):
			delete(t.state.Changes, strings.TrimPrefix(id, changePrefix))
		case strings.HasPrefix(id, attemptPrefix):
			delete(t.state.Attempts, strings.TrimPrefix(id, attemptPrefix))
		case strings.HasPrefix(id, leasePrefix):
			delete(t.state.Leases, strings.TrimPrefix(id, leasePrefix))
		case strings.HasPrefix(id, publicationPrefix):
			delete(t.state.Publications, strings.TrimPrefix(id, publicationPrefix))
		}
	}
}

// Amend is the only write: read the ref, hand the caller a transaction
// over the state that is actually there, graft the mutated records into
// a new tree, commit it, and move the ref with the old value as the
// expected one — returning ErrConcurrent when a peer got there first.
//
// The closure may run MORE THAN ONCE, because a lost race is retried
// against a fresh read. It must therefore be a pure function of the
// state it is handed, and it must not carry an effect of its own. That
// is a real constraint on every caller and it is why the provider call
// in lease.Release sits between two Amends rather than inside one.
//
// THE COMMIT IS ONE BATCH. Under the Lock: read; hand the closure a fresh
// Txn over what was read; GraftTree the mutated records into a new tree;
// CommitTree (parent = the commit read, or none for the first write over
// EmptyTree — the ONE path permitted to swallow ErrNoState); then hand
// git.UpdateRefs ONE batch: the state line first — Ref, the new commit,
// old = the commit the closure was handed ("" on the first write) —
// then every line the closure queued through Txn.Ref, in call order.
// All-or-nothing is git's; the classification is by re-read; and the
// two refusals are told apart by WHICH ref the re-read found moved:
//
//   - The state ref: a writer who never took the Lock got there first.
//     ErrConcurrent, and the closure is run again over a fresh read —
//     the retry this doc has always promised, now the only retry,
//     bounded, then returned.
//   - Any other ref: a foreign hand moved, created or deleted something
//     the store owns, between the closure's read and the commit.
//     Returned as the *git.RefMoved it is (errors.Is ErrRefMoved), NOT
//     retried — the closure would derive the same expected-old from the
//     same record and lose the same way — and left for the road to
//     report: exit 45, the person's own `git commit` or `git branch -D`.
//     The state line is checked first in the re-read so a peer's write
//     and a foreign move in the same batch surface as the retryable one.
//   - Neither (every ref where expected): git's own failure, handed on
//     as it came.
//
// This is the whole of R23: the record and the ref it names land in one
// act, so a crash between them is not a state. It retires ChangePrepared
// and ChangeExtending, change.Bind and BindIn, RevertExtendIn, the
// rebind-or-abandon sweep, the orphan-pin sweep and git.CASRef — every
// one of which existed because the record and the ref were two steps.
// The two verbs internal/git had to gain are CommitTree and UpdateRefs;
// the third, CheckedOutAt, is an observation, not a mover.
//
// The notes ref is NOT in the batch. It is a derived, stamped export
// (Export, below) that `cycle` regenerates when its stamp is behind; a
// second writer of a DERIVED ref is not a second authority, and "only
// mover" is a claim about the three refs that are.
//
// Amend is also where record.DocSchema is applied: a document from
// another shape is refused (ErrDocShape, by the Read every Amend begins
// with) and the ref recreated.
//
// THE HOUSEKEEPING BILL IS DEFERRED ONTO THE MAINTAINER, and this was
// measured rather than reasoned about. Every primitive Amend uses —
// hash-object, mktree, commit-tree, update-ref — is PLUMBING, and plumbing
// never runs git's automatic maintenance. So dockhand writes three loose
// objects per amend and never cleans up after itself; the threshold is
// crossed at roughly 2,300 amends (git samples .git/objects/17 and fires
// above 27 entries, which is ~6,900 loose in practice), and the 12-16
// second repack that follows lands on the maintainer's next `git commit`
// or `git pull`. It detaches, so there is no foreground stall — measured at
// 0.11 to 0.34 seconds across every loose-object level up to 8,798 — but
// the work is real and it is charged to the wrong person.
//
// The fix belongs to `cycle`: run `git maintenance run --auto` at the end
// of a pass, so the robot pays its own bill at a moment nobody is waiting.
//
// A LOST RACE WAS NOT FREE EITHER, AND THIS PARAGRAPH IS NOW HISTORY.
// A lost race leaves exactly three unreachable objects, and git packs
// rather than prunes those: gc.pruneExpire defaults to two weeks, so
// contention bought disk that neither gc nor Compact removed for a
// fortnight. At two concurrent writers the measured lost-race rate was 90%
// of successes, which is what made it the common case rather than the
// corner — and what made the flock worth ruling for.
//
// Under the Lock above there are no lost races, so this cost is gone. The
// same measurement appears twice in this file for two opposite purposes,
// which is confusing unless one of them says which side of the ruling it
// is on: this one is the BEFORE.
func (s *Store) Amend(ctx context.Context, mutate func(*Txn) error) error {
	unlock, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureReflog(ctx); err != nil {
		return err
	}
	for try := 0; ; try++ {
		st, err := s.Read(ctx)
		if errors.Is(err, ErrNoState) {
			// The one path permitted to swallow it: a repository that has
			// never run dockhand has no tip to be handed, and the create
			// line the empty At produces is what refuses a peer that
			// created the ref in the meantime.
			st = newState("")
		} else if err != nil {
			return err
		}
		tx := &Txn{state: st, changed: map[string]bool{}}
		if err := mutate(tx); err != nil {
			return err
		}
		files, err := tx.files()
		if err != nil {
			return err
		}
		base := st.At
		var parents []string
		if base == "" {
			base = EmptyTree
		} else {
			parents = []string{st.At}
		}
		tree, err := s.repo.GraftTree(ctx, base, files)
		if err != nil {
			return err
		}
		commit, err := s.repo.CommitTree(ctx, tree, parents, amendMessage)
		if err != nil {
			return err
		}
		batch := append([]git.RefUpdate{{Ref: Ref, New: commit, Old: st.At}}, tx.refs...)
		err = s.repo.UpdateRefs(ctx, batch)
		if err == nil {
			return nil
		}
		// Only the state ref's own line is a race. UpdateRefs classifies
		// by re-reading in batch order with the state line first, so a
		// batch that lost both a race and a foreign move surfaces as the
		// retryable one; every other moved ref is a foreign hand, and the
		// closure would derive the same expected-old and lose the same
		// way, so it is returned untouched and unretried.
		var moved *git.RefMoved
		if !errors.As(err, &moved) || moved.Ref != Ref {
			return err
		}
		if try+1 >= amendTries {
			return fmt.Errorf("%w: %d attempts", ErrConcurrent, amendTries)
		}
	}
}

// take acquires the ledger flock for this repository. The path resolves
// through GIT_COMMON_DIR, so every linked worktree of one repository
// takes the same lock over the one ref they share.
func (s *Store) take(ctx context.Context) (func(), error) {
	path, err := s.repo.CommonDirFile(ctx, Lock)
	if err != nil {
		return nil, err
	}
	return lockfile.Acquire(ctx, path, lockDeadline)
}

// ensureReflog makes LogRef true of this repository before the store
// moves anything, and it runs under the lock for the same reason the
// batch does: it is a write to the repository's config, and two
// dockhands doing it at once is git's own lock file contending with
// itself.
//
// It reads first and writes only when the value is not already the one
// the design needs, so a repository that has been set up once pays a
// read per process rather than a write per amend — and a person who set
// "always" by hand sees no churn in their config's mtime.
func (s *Store) ensureReflog(ctx context.Context) error {
	if s.logged {
		return nil
	}
	key, value, ok := strings.Cut(LogRef, "=")
	if !ok {
		return fmt.Errorf("statestore: %q is not a key=value setting", LogRef)
	}
	got, set, err := s.repo.Config(ctx, key)
	if err != nil {
		return err
	}
	if !set || got != value {
		if err := s.repo.SetConfig(ctx, key, value); err != nil {
			return err
		}
	}
	s.logged = true
	return nil
}

// files renders the transaction's touched records as the tree edits
// GraftTree takes, in a stable order so that two closures that made the
// same edits build the same tree.
//
// The ID refusal lands here, which is the last moment a record is still
// a value rather than a name: a record whose id is empty, or carries a
// slash or a NUL, cannot be a flat entry in a tree — and inventing a
// name for it would file the record where nothing would ever read it
// back.
func (t *Txn) files() ([]git.File, error) {
	files := make([]git.File, 0, len(t.changed))
	for _, name := range slices.Sorted(maps.Keys(t.changed)) {
		if !t.changed[name] {
			files = append(files, git.File{Path: name, Delete: true})
			continue
		}
		id, _ := strings.CutSuffix(name, docSuffix)
		content, err := t.encode(id)
		if err != nil {
			return nil, err
		}
		files = append(files, git.File{Path: name, Content: content})
	}
	return files, nil
}

// encode renders one touched record as the bytes its blob holds: the
// two-space indent record.Encode uses for the note, so that `git show`
// on a record in the archive reads the way a note does, plus the
// trailing newline a file in a tree is expected to end with.
func (t *Txn) encode(id string) ([]byte, error) {
	var doc any
	switch {
	case strings.HasPrefix(id, changePrefix):
		doc = t.state.Changes[strings.TrimPrefix(id, changePrefix)]
	case strings.HasPrefix(id, attemptPrefix):
		doc = t.state.Attempts[strings.TrimPrefix(id, attemptPrefix)]
	case strings.HasPrefix(id, leasePrefix):
		doc = t.state.Leases[strings.TrimPrefix(id, leasePrefix)]
	case strings.HasPrefix(id, publicationPrefix):
		doc = t.state.Publications[strings.TrimPrefix(id, publicationPrefix)]
	default:
		return nil, fmt.Errorf("statestore: %s is not a record name", id)
	}
	if err := legalName(id); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// legalName refuses an id that cannot be a flat tree entry.
func legalName(id string) error {
	for _, prefix := range []string{changePrefix, attemptPrefix, leasePrefix, publicationPrefix} {
		if rest, ok := strings.CutPrefix(id, prefix); ok {
			if rest == "" {
				return fmt.Errorf("statestore: a %srecord has no id", prefix)
			}
			if strings.ContainsAny(rest, "/\x00") {
				return fmt.Errorf("statestore: %q does not name a record", rest)
			}
			return nil
		}
	}
	return fmt.Errorf("statestore: %s is not a record name", id)
}
