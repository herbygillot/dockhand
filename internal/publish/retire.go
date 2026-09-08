package publish

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Standing is the OBSERVATION half of retirement: what the forge says
// became of a publication, and what that would leave removable. It
// performs no write. Reading it off ForgeFacts rather than ancestry is
// the whole of the shipped defect it removes — a squash-merge leaves no
// ancestry and a rebase-landed change leaves none either, so ancestry
// answers "was this merged" with a confident no. The forge's own word,
// or nothing: an Outcome of Open, or an unreachable forge, retires
// nothing.
//
// A CACHED STANDING RETIRES NOTHING EITHER, which is Authorize's
// ErrNotFresh one lifecycle over. Retirement closes a change, deletes a
// branch and push-deletes a fork copy; deciding any of that from what a
// previous pass wrote down would be the destructive half of the same
// mistake the publication gate refuses on.
func Standing(p record.Publication, f ForgeFacts) (record.Outcome, Demolish, error) {
	switch {
	case !f.Fresh:
		return record.Open, Demolish{}, ErrNotFresh
	case f.Err != nil:
		return record.Open, Demolish{}, f.Err
	case !f.OwnFound:
		// Promoted with no pull request found — the push happened and the
		// pull request did not, or somebody deleted it. There is nothing the
		// forge said, so there is nothing to conclude and nothing to remove.
		return record.Open, Demolish{}, nil
	case prMerged(f.Own):
		return record.Merged, demolishable(f), nil
	case prOpen(f.Own):
		return record.Open, Demolish{}, nil
	}
	// Closed without merging. Rejection is information and the branch
	// stays: deleting the evidence of it helps nobody, which is why
	// Demolish is empty here and not merely withheld by a policy.
	return record.Rejected, Demolish{}, nil
}

// Demolish is what a retirement leaves removable — and ONLY what
// dockhand itself created. A branch a person pushed to the same
// namespace is not dockhand's to delete, and the fork copy is removable
// exactly because dockhand put it there. The two halves are consumed by
// two different mechanisms, which is the split R23 draws: Local by
// change.DemolishIn's line INSIDE the retire batch (a local ref is
// authority the store moves), Fork by the record.DeleteFork step that
// DeleteForkIn writes and DeleteFork performs afterwards (a remote is a
// foreign effect that keeps record -> effect -> outcome).
type Demolish struct {
	Local string // the local branch, if dockhand minted it
	Fork  string // the pushed copy, if dockhand pushed it
}

// demolishable is what a merged publication leaves removable, read off
// the pull request's own head.
//
// THE NAME COMES FROM THE FORGE AND THE NAMESPACE IS THE TEST. The head
// ref is the branch the merged pull request proposed from, so it is the
// one name that is certainly the copy dockhand pushed; and a branch
// under git.BranchNamespace is one dockhand's own naming produced. That
// is a fact about the NAME, and it is deliberately not the whole
// permission: whether a machine may delete this particular change — it
// may never demolish a MintedVia Adopted, nor act past a hold — is
// app's, asked inside the closure over the state the transaction was
// handed, because a hold can land between a pass's read and its Amend.
// Standing observes; the caller decides.
func demolishable(f ForgeFacts) Demolish {
	branch := f.Own.Head.Ref
	if branch == "" || !strings.HasPrefix(branch, git.BranchNamespace) {
		return Demolish{}
	}
	d := Demolish{Local: branch}
	if f.ForkRemote != "" {
		// The pull request's head lives on the fork by construction — it was
		// opened as <owner>:<branch> — so a merged one always leaves a copy
		// there to remove. Whether it is STILL there is not this pure
		// function's question: DeleteFork asks the remote itself, before it
		// pushes anything and again after.
		d.Fork = branch
	}
	return d
}

// RetireIn writes the terminal Outcome on a publication row, as a
// transaction step, and it is the split of a draft's Retire (observe +
// write in one call) into observation and effect. It is a *Txn mutator
// because a merge closes TWO lifecycles: the publication (this) and the
// change (change.CloseIn to ChangePublished), and Cycle's close stage
// writes both in ONE Amend — a change closes ChangePublished only when
// the forge says merged, and never at Promote. The outcome row is
// written under every retirement policy, because a merge is what a
// published change ends as whether or not anybody will act on it; only
// the DELETION is a policy — the local branch through change.DemolishIn's
// line in this same Amend, the fork copy through the DeleteFork step
// DeleteForkIn writes beside it. It refuses a publication already
// Settled.
//
// It stamps a record.RecordOutcome step, which is what that step kind is
// for and is also what dates the row: statestore.Compact measures a
// publication's age from its last step, so a retirement that wrote only
// the Outcome would leave the row dated by the push that opened it.
func RetireIn(tx *statestore.Txn, id string, out record.Outcome, now time.Time) error {
	if !out.Settled() {
		return fmt.Errorf("%w: %q", ErrNotTerminal, out)
	}
	p, ok := tx.State().Publications[id]
	if !ok {
		return ErrNoRow
	}
	if p.Outcome.Settled() {
		return fmt.Errorf("%w: %s is already %s", ErrAlreadySettled, id, p.Outcome)
	}
	p.Outcome = out
	p.Steps = markStep(p.Steps, record.Step{Kind: record.RecordOutcome, Phase: record.Finished, At: now.UTC()})
	tx.PutPublication(p)
	return nil
}

// DeleteForkIn writes a record.DeleteFork step in Phase Requested on the
// publication row, as a transaction step, in the same Amend as RetireIn
// and change.CloseIn — under Retirement Demolish, and only when Standing
// said the fork copy is dockhand's to delete. It is a separate mutator
// rather than a bool on RetireIn because one mutator writes one step,
// which is the shape lease.RequestIn keeps beside SettleIn; a flag that
// changed what RetireIn wrote would be a mode hidden in a field. The
// step is Requested here and performed AFTER the Amend by DeleteFork,
// because a push is a foreign effect that cannot join the batch: the
// record says what is about to happen, the effect happens outside every
// lock, and the outcome is written by ForkGoneIn — lease.Release's shape,
// on the publication lifecycle. It refuses a publication whose outcome
// is not Settled() and one that already carries a DeleteFork step.
//
// THE ORDER INSIDE THE CLOSURE IS RetireIn FIRST. This reads the outcome
// the transaction has been left with, so a caller that wrote the
// retirement in the same Amend is answered by its own write, and one
// that has not is refused — which is what makes "the fork copy of an
// open pull request is the head the review is reading" a rule the
// mutator holds rather than a rule the caller remembers.
func DeleteForkIn(tx *statestore.Txn, id string, now time.Time) error {
	p, ok := tx.State().Publications[id]
	if !ok {
		return ErrNoRow
	}
	if !p.Outcome.Settled() {
		return fmt.Errorf("%w: %s is %s", ErrNotSettled, id, p.Outcome)
	}
	for _, step := range p.Steps {
		if step.Kind == record.DeleteFork {
			return fmt.Errorf("%w: %s", ErrForkStepStands, id)
		}
	}
	p.Steps = append(p.Steps, record.Step{Kind: record.DeleteFork, Phase: record.Requested, At: now.UTC(), Attempt: 1})
	tx.PutPublication(p)
	return nil
}

// ForkGoneIn writes what became of the DeleteFork step: Finished when
// the copy is observed gone FROM THE REMOTE (git.Repo.RemoteHas —
// whether this pass deleted it or found it already absent), Uncertain
// when the remote could not be asked, before or after the push. It never
// writes Requested back: a push that failed and left the copy standing
// leaves the step AS IT WAS, with Attempt incremented and NotBefore set,
// so the next pass past the backoff retries it. It refuses a publication
// with no Requested or Uncertain DeleteFork step.
//
// So the three phases mean three different things HERE, and the odd one
// is Requested: a caller passing it is not asking for the step to be
// re-requested, it is reporting that the copy still stands. The phase on
// the record is left exactly as it was — Requested stays Requested and
// an Uncertain that has since been asked and answered stays Uncertain —
// and only the retry state moves.
func ForkGoneIn(tx *statestore.Txn, id string, ph record.Phase, detail string, notBefore *time.Time, now time.Time) error {
	p, ok := tx.State().Publications[id]
	if !ok {
		return ErrNoRow
	}
	at := -1
	for i, step := range p.Steps {
		if step.Kind == record.DeleteFork && (step.Phase == record.Requested || step.Phase == record.Uncertain) {
			at = i
			break
		}
	}
	if at < 0 {
		return fmt.Errorf("%w: %s", ErrNoForkStep, id)
	}
	step := p.Steps[at]
	step.At, step.Detail = now.UTC(), detail
	switch ph {
	case record.Finished:
		// The copy is gone from the remote, whether this pass deleted it or
		// found it already absent. Nothing is owed, so the backoff goes with
		// the obligation it belonged to; the attempt count stays, because how
		// many tries a deletion took is history a person may want.
		step.Phase, step.NotBefore = record.Finished, nil
	case record.Uncertain:
		// The remote could not be asked, before or after the push. The
		// obligation stands and ForkOwed keeps reporting it.
		step.Phase, step.NotBefore = record.Uncertain, notBefore
	case record.Requested:
		// The copy still stands. The phase is left as it was and the retry
		// state moves: one more attempt, and a deadline before the next.
		step.Attempt++
		step.NotBefore = notBefore
	case record.Acquiring, record.Active:
		return fmt.Errorf("%w: %q", ErrNotAnOutcome, ph)
	default:
		return fmt.Errorf("%w: %q", ErrNotAnOutcome, ph)
	}
	steps := append([]record.Step(nil), p.Steps...)
	steps[at] = step
	p.Steps = steps
	tx.PutPublication(p)
	return nil
}

// ForkOwed is the population DeleteFork walks: every publication whose
// DeleteFork step is Requested or Uncertain. Pure, over one read, like
// lease.Outstanding's list — and like it, a report before it is a
// worklist, so `status` can say "fork copy of dockhand/foo not yet
// deleted, 3 attempts, next after T". Compact keeps every row in it.
func ForkOwed(s statestore.State) []record.Publication {
	var out []record.Publication
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		p := s.Publications[key]
		for _, step := range p.Steps {
			if step.Kind == record.DeleteFork && (step.Phase == record.Requested || step.Phase == record.Uncertain) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// DeleteFork is the sequencer for retirement's one foreign effect, and
// it is written on lease.Release's precedent: observe, act outside the
// lock, record. It skips a step whose NotBefore has not passed. It
// observes THE REMOTE first — git.Repo.RemoteHas, `ls-remote`, never
// the remote-tracking cache PushedTo reads, which this machine's own
// push or fetch writes and nothing the forge does (measured: a copy the
// forge deleted stays listed, and `push --delete` of it fails and leaves
// it listed, so a sequencer over the cache would retry a deletion that
// can never succeed until the give-up and then report as owed a copy
// that does not exist) — and a copy already gone is Finished with
// "already gone", never a push-delete whose error would then have to be
// read as advisory (rule 6). Then PushDelete, then RemoteHas again,
// then ONE Amend with ForkGoneIn: Finished if the remote no longer
// lists it, unchanged-with-Attempt+1-and-NotBefore if it still does,
// Uncertain if the remote could not be asked at either moment. The
// stale tracking ref a foreign deletion leaves behind is a local
// non-authority ref nothing reads once the step is Finished (Standing
// reads the forge, and the cache is only ever asked which REMOTE holds a
// copy). Cycle's close stage calls it over ForkOwed after the retire
// Amends; a failure is a Refusal row, never a stop, and it is retried on
// its backoff until the remote no longer lists the copy.
//
// PushedTo IS READ, AND ONLY FOR THE REMOTE'S NAME. Which remote holds a
// copy is a fact about this checkout's own last push, which is exactly
// what the tracking refs record and exactly what they are trustworthy
// for; whether the copy is STILL there is a fact about the forge, and
// nothing but the forge is asked it. A checkout whose tracking ref has
// been pruned has no name to push to and cannot observe either, so the
// step is left Uncertain rather than guessed at — `status` reports it as
// owed and a person can fetch or delete it by hand.
func DeleteFork(ctx context.Context, e Env, p record.Publication, now func() time.Time) error {
	if e.Repo == nil || e.State == nil {
		return ErrNoEnv
	}
	step, ok := forkStep(p)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoForkStep, p.ID)
	}
	at := now()
	if step.NotBefore != nil && at.Before(*step.NotBefore) {
		return nil
	}
	branch, err := forkBranch(ctx, e, p)
	if err != nil {
		return err
	}
	remote, err := e.Repo.PushedTo(ctx, branch)
	if err != nil {
		return gone(ctx, e, p.ID, record.Uncertain, "reading this checkout's remote-tracking refs: "+err.Error(), step, at)
	}
	if remote == "" {
		return gone(ctx, e, p.ID, record.Uncertain,
			"no remote in this checkout holds a copy of "+branch+", so the deletion cannot be attempted or observed here", step, at)
	}
	has, err := e.Repo.RemoteHas(ctx, remote, branch)
	if err != nil {
		return gone(ctx, e, p.ID, record.Uncertain, "asking "+remote+" about "+branch+": "+err.Error(), step, at)
	}
	if !has {
		return gone(ctx, e, p.ID, record.Finished, "already gone from "+remote, step, at)
	}
	pushErr := e.Repo.PushDelete(ctx, remote, branch)
	// The remote again, and the push's own error is NOT what decides. A
	// delete that reported a failure and removed the branch, and one that
	// reported nothing and removed nothing, are told apart by asking the
	// remote — never by reading what git said, which is rule 6.
	still, err := e.Repo.RemoteHas(ctx, remote, branch)
	switch {
	case err != nil:
		return gone(ctx, e, p.ID, record.Uncertain, "asking "+remote+" about "+branch+" after the delete: "+err.Error(), step, at)
	case !still:
		return gone(ctx, e, p.ID, record.Finished, "deleted from "+remote, step, at)
	}
	detail := "the copy still stands on " + remote
	if pushErr != nil {
		detail += ": " + pushErr.Error()
	}
	return gone(ctx, e, p.ID, record.Requested, detail, step, at)
}

// gone is DeleteFork's one write: ForkGoneIn inside an Amend, with the
// backoff computed for the phases that carry one. It is a function
// because DeleteFork reaches it from seven places and a sequencer whose
// recording is spelled seven times is a sequencer with seven chances to
// forget the backoff.
func gone(ctx context.Context, e Env, id string, ph record.Phase, detail string, step record.Step, at time.Time) error {
	var notBefore *time.Time
	if ph != record.Finished {
		until := at.Add(retryAfter(step.Attempt))
		notBefore = &until
	}
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, id, ph, detail, notBefore, at)
	})
}

// forkStep is the outstanding DeleteFork step on a row, if there is one.
func forkStep(p record.Publication) (record.Step, bool) {
	for _, step := range p.Steps {
		if step.Kind == record.DeleteFork && (step.Phase == record.Requested || step.Phase == record.Uncertain) {
			return step, true
		}
	}
	return record.Step{}, false
}

// forkBranch is the name the fork copy is under: the change's own
// branch, read from the store.
//
// It is read rather than carried on the row, and that is the honest
// consequence of the row being keyed by the change: record.Publication
// names a ChangeID and no branch, so the name lives where the change
// lifecycle keeps it. A closed change KEEPS its Branch — the record is
// never blanked, so that a closed row can still say "was dockhand/foo" —
// which is what makes this readable at exactly the moment the change is
// closed and the copy is owed.
func forkBranch(ctx context.Context, e Env, p record.Publication) (string, error) {
	st, err := e.State.Read(ctx)
	if err != nil {
		return "", err
	}
	c, ok := st.Changes[string(p.Change)]
	if !ok {
		return "", fmt.Errorf("%w: the publication names a change the store no longer holds", ErrNoChange)
	}
	if c.Branch == "" {
		return "", fmt.Errorf("%w: %s", ErrNoBranch, p.Change)
	}
	return c.Branch, nil
}

// retryAfter is how long a refused fork deletion waits before the next
// pass tries it again: five minutes, doubling, capped at an hour.
//
// It is lease.retryAfter's schedule and its reasoning, on the other
// lifecycle that owes a foreign effect. The floor is the resident
// dispatcher's own cadence — record.Step.NotBefore states the problem as
// "a fork copy the remote will not delete is not pushed at on every
// five-minute tick" — so the first retry is the next pass and no sooner.
// The ceiling is there because the commonest reason a forge refuses is
// transient, and an hour is short enough that it comes back on its own
// and long enough that a permanently refusing remote is not being pushed
// at every tick until somebody notices.
func retryAfter(attempts int) time.Duration {
	const base, ceiling = 5 * time.Minute, time.Hour
	wait := base
	for i := 1; i < attempts && wait < ceiling; i++ {
		wait *= 2
	}
	return min(wait, ceiling)
}
