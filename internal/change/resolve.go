package change

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Ref names a change that exists. Its fields are unexported and there
// is no literal for it: the ONLY way to obtain one is Resolve, which
// found it in the store AND in the repository after a transaction
// landed. A draft had a second way — Bind, which created it — and with
// the ref moving inside the Amend there is no moment after a mint at
// which app holds a Ref it did not resolve: the mint roads call Resolve
// on their own branch after the Amend returns. That is a second read
// per mint, and it is the honest cost of the batch being the only
// writer; it is also the read that reports a foreign move made in the
// second between (exit 45) rather than building on it.
type Ref struct {
	repo *git.Repo
	// id is the change's durable identity, and a Ref carries it because
	// three of the four documents in the state ref are keyed on it. A
	// draft of this design introduced ChangeID and left Ref without one,
	// so an operation holding a Ref could not key anything it stored.
	id       record.ChangeID
	branch   string
	tip      string
	content  record.ContentID
	subjects []record.Subject
}

func (r Ref) ID() record.ChangeID        { return r.id }
func (r Ref) Branch() string             { return r.branch }
func (r Ref) Tip() string                { return r.tip }
func (r Ref) Repo() *git.Repo            { return r.repo }
func (r Ref) Content() record.ContentID  { return r.content }
func (r Ref) Subjects() []record.Subject { return append([]record.Subject(nil), r.subjects...) }
func (r Ref) Valid() bool                { return r.tip != "" }

// TipDisagreement is ErrTipDisagrees typed (rule 7: Absent and Found ""
// are one answer, spelled so a caller branches on the bool and not on
// the empty string; a read that FAILED is its own error, never this):
// the record's Tip and what the ref holds, for the branch or, for a
// branchless record, the pin. It is what status prints per disagreeing
// change and what Verify's follow, Discard and PinLostIn are handed.
type TipDisagreement struct {
	ID       record.ChangeID
	Ref      string // BranchRef(branch) or PinRef(id)
	Recorded string
	Found    string
	Absent   bool
}

func (e *TipDisagreement) Error() string { return "change: " + e.Ref + " is not at the recorded tip" }
func (e *TipDisagreement) Unwrap() error { return ErrTipDisagrees }

// Resolve finds an existing change by branch, port, tip, or pin name —
// change.PinRef(id), the one target that names a BRANCHLESS record by
// its id, which `status`'s facts loop and `discard` use for a snapshot
// (a port may carry a snapshot and a branch change at once, so a port
// is not a name for the pin) — and it takes the STORE as well as the
// repository because a change is its record, and the branch only a binding on it. Where two records carry one branch name —
// names are reused, ids are not — it answers with the Bound() one. It
// returns ErrNoRecord for a dockhand/ branch the store does not know —
// hand-made, or minted before the state ref was recreated — which
// Verify's adopt stage answers with AdoptIn and every other road refuses
// (exit 44); and ErrTipDisagrees, as a *TipDisagreement, when a Bound()
// record's Tip is not what its ref holds — Branch, or Pin — which is a
// person's own `git commit` on a dockhand branch, a `git branch -D`, or
// a hand-moved pin, and is REPORTED rather than trusted either way. With
// Prepared and Extending gone, disagreement is the only way a bound
// record and its ref can differ, and it is always a foreign hand:
// `status` shows it, Verify's follow stage answers it for a moved branch
// (FollowIn), `discard` ends a record whose ref is gone (PinLostIn for a
// pin; no DemolishIn for a branch — Discard.Run keeps the disagreement
// it was returned and hands it down, since the roads that continue past
// a disagreement are the ones that resolved it), Cycle's retire closes
// without the delete line and reports, and every other road refuses it
// (exit 45). Cancel's stages need no Ref at all: they take the record's
// id and tip, so a road that resolved a disagreement still stops and
// releases the recorded tip's work.
func Resolve(ctx context.Context, repo *git.Repo, st *statestore.Store, target string) (Ref, error) {
	if repo == nil || st == nil {
		return Ref{}, ErrNoRecord
	}
	state, err := st.Read(ctx)
	if err != nil {
		return Ref{}, err
	}
	c, ok := match(state, target)
	if !ok {
		return Ref{}, ErrNoRecord
	}
	// The ref the record says is its own: a branch while it has one, and
	// a pin for the branchless record a snapshot wrote. A record that
	// names neither is a closed one nothing is keeping alive, and there
	// is nothing to hold its tip against.
	name := BranchRef(c.Branch)
	if c.Branch == "" {
		name = c.Pin
	}
	if name == "" {
		return Ref{}, ErrNoRecord
	}
	found, err := refValue(ctx, repo, name)
	if err != nil {
		return Ref{}, err
	}
	if found != c.Tip {
		return Ref{}, &TipDisagreement{
			ID: c.ID, Ref: name, Recorded: c.Tip, Found: found, Absent: found == "",
		}
	}
	return Ref{
		repo: repo, id: c.ID, branch: c.Branch, tip: c.Tip,
		content: c.Content, subjects: c.Subjects,
	}, nil
}

// match is Resolve's four readings of one target, over the store alone:
// a change id, a pin ref name, a branch name, or a port. The Bound()
// record wins wherever two carry one name, because a name is a binding
// and an id is not — and the ids are walked in sorted order so a target
// two closed records both answer to resolves to the same one every time.
//
// The order of the readings is the order of their specificity. An id and
// a pin name are unique by construction; a branch name is unique among
// Bound() records because MintIn's ErrStanding says so; a port is the
// loosest, and the newest change wins there for the reason Standing
// gives.
func match(s statestore.State, target string) (record.Change, bool) {
	if target == "" {
		return record.Change{}, false
	}
	if c, ok := s.Changes[target]; ok {
		return c, true
	}
	if id, ok := strings.CutPrefix(target, PinRef("")); ok {
		if c, ok := s.Changes[id]; ok {
			return c, true
		}
		return record.Change{}, false
	}
	var byBranch, byPort *record.Change
	for _, key := range slices.Sorted(maps.Keys(s.Changes)) {
		c := s.Changes[key]
		if c.Branch != "" && c.Branch == target {
			if byBranch == nil || c.Bound() {
				byBranch = &c
			}
		}
		if names(c, target) {
			if byPort == nil || c.Bound() {
				byPort = &c
			}
		}
	}
	switch {
	case byBranch != nil:
		return *byBranch, true
	case byPort != nil:
		return *byPort, true
	}
	return record.Change{}, false
}

// names reports that a change is about this port — its own name or one
// of its subports, which is what Subject.Names exists to answer: a
// person naming py312-foo means the member that owns it, and a match on
// Port alone would find no change and refuse.
func names(c record.Change, port string) bool {
	for _, s := range c.Subjects {
		if s.Port == port || slices.Contains(s.Names, port) {
			return true
		}
	}
	return false
}

// refValue is what a ref holds, with ABSENCE told apart from a failure
// to read (rule 7): for-each-ref answers an exact ref name with that ref
// or with nothing, and a git that could not be run at all is an error.
// git.Repo.RevParse cannot be asked this on its own — it returns "" with
// a non-nil error for both — and the store's own classifier says so in
// as many words at internal/git/update.go.
//
// Two calls and not one, because RefsUnder answers only with names.
// Measured against git 2.55: `for-each-ref refs/heads/dockhand/jq-1.8`
// does NOT match refs/heads/dockhand/jq-1.8.2 — the patterns are
// path-wise, so a partial component matches nothing — which is what
// makes the exact name safe to pass as a pattern.
func refValue(ctx context.Context, repo *git.Repo, name string) (string, error) {
	present, err := repo.RefsUnder(ctx, name)
	if err != nil {
		return "", err
	}
	if !slices.Contains(present, name) {
		return "", nil
	}
	return repo.RevParse(ctx, name)
}

// Standing is the change currently in flight for a port, if one is: a
// Bound() record whose subjects name the port. It is a pure lookup over
// one read, and it is what Change's resolve stage asks BEFORE it plans —
// a standing branch is exit 11 under Refuse, the old change under
// --replace, and a Stood row under a selector (InFlight Advance). It
// answers from the STORE and not from the branch namespace, which is
// what makes a Survey target withheld with no record invisible to it, as
// it should be; a branch with no record is the batch's finding
// (git.ErrRefMoved on the create line), not this one's.
//
// The NEWEST wins where a port carries two — a snapshot beside a branch
// change, say — and newest is the greatest id, because a change id is
// minted before any effect and is ordered by the moment it was minted.
// A rule is needed at all because the answer feeds --replace, and a
// replace that superseded whichever record a map iteration reached first
// would supersede a different one on the next run.
func Standing(s statestore.State, port string) (record.Change, bool) {
	var found record.Change
	ok := false
	for _, key := range slices.Sorted(maps.Keys(s.Changes)) {
		c := s.Changes[key]
		if c.Bound() && names(c, port) {
			found, ok = c, true
		}
	}
	return found, ok
}

var (
	// ErrInFlight is Standing's positive answer as the Change road's
	// refusal under InFlight Refuse: exit 11, BranchInFlight.
	ErrInFlight       = errors.New("change: a branch already stands for this port")
	ErrNoRecord       = errors.New("change: the branch has no change record")
	ErrTipDisagrees   = errors.New("change: the record's tip is not the branch's")
	ErrNoProposal     = errors.New("change: no proposed cohort finding on this change")
	ErrUnknownMember  = errors.New("change: a named member is not a candidate")
	ErrEmptyCohort    = errors.New("change: every candidate was excluded")
	ErrNotWithheld    = errors.New("change: --force-withheld names a member that was not withheld")
	ErrCannotForce    = errors.New("change: a withheld member cannot be forced; nothing to deactivate")
	ErrForcedConflict = errors.New("change: two forced members deactivate each other")
)
