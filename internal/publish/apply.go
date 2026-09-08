package publish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Outcome is what actually happened, including when an error is also
// returned. Steps names what completed, so a caller can tell a branch
// pushed with no pull request from a branch that never left the
// machine.
type Outcome struct {
	Completed []record.StepKind
	Number    int
	URL       string
	At        time.Time
}

// Apply carries out a permit, recording each step in the state ref
// BEFORE attempting it and its outcome after — so a crash between the
// push and the pull request leaves a publication whose push is known to
// have happened and whose PR is Uncertain, rather than nothing.
//
// The publication is keyed by the change, not by whichever commit was
// at the tip when a forge call was made, which is what stops a row
// opened at one tip from being unclosable once the branch moves.
//
// It revalidates the identities the permit was granted over — the tip,
// and the state of the branch's own pull request — and returns ErrStale
// rather than acting on a permission that has gone out of date.
//
// NO EFFECT IS INSIDE AN AMEND. Every write here is its own transaction
// and every push and forge call sits between two of them, which is
// lease.Release's shape and the same rule for the same reason:
// statestore.Amend takes a repository-wide flock and re-runs its closure
// on a lost race, so a push inside one would be performed twice and
// would stall every peer sharing the checkout for as long as the forge
// takes.
//
// A NO-OP PERMIT RETURNS BEFORE ANY I/O AT ALL, including the
// revalidation. Authorize already established that the branch's own pull
// request is open at this tip; there is nothing to make stale, nothing
// to perform, and nothing to record, and a "no-op" that still spent a
// forge read per candidate per tick would be the cost the no-op exists
// to remove.
func Apply(ctx context.Context, e Env, p Permit) (Outcome, error) {
	if !p.granted {
		return Outcome{}, ErrNoPermit
	}
	f := p.facts
	if p.NoOp() {
		return Outcome{Number: f.Forge.Own.Number, URL: f.Forge.Own.HTMLURL, At: f.AsOf}, nil
	}
	if e.Repo == nil || e.State == nil {
		return Outcome{}, ErrNoEnv
	}
	if err := revalidate(ctx, e, f); err != nil {
		return Outcome{}, err
	}
	id, err := openRow(ctx, e, f, p.steps)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{At: f.AsOf}
	for _, kind := range p.steps {
		if err := mark(ctx, e, id, kind, record.Requested, "", f.AsOf); err != nil {
			return out, err
		}
		number, url, err := perform(ctx, e, p, kind)
		if err != nil {
			// Uncertain and never a phase claiming to know. A forge call that
			// errored may have done the thing it was asked to do — `gh pr
			// create` prints the URL and then fails on the response, a push
			// completes and the connection drops — and rule 6 forbids
			// recovering the difference by reading the words that came back.
			// The retire pass resolves an Uncertain OpenPR by asking the forge,
			// which is the one authority that knows.
			_ = mark(ctx, e, id, kind, record.Uncertain, err.Error(), f.AsOf)
			// WHAT ALREADY STANDS TRAVELS WITH THE FAILURE. The Outcome
			// carries it for a caller that kept the value, and the error
			// carries it for the one that did not — cli's classifier is the
			// second, and the partial band it computes from this is the
			// difference between a wrapper re-running a publication and one
			// that knows the branch is already on the fork.
			return out, &StepError{Kind: kind, Completed: slices.Clone(out.Completed), Err: err}
		}
		if number != 0 || url != "" {
			out.Number, out.URL = number, url
		}
		if err := finish(ctx, e, id, kind, out, f.AsOf); err != nil {
			return out, err
		}
		out.Completed = append(out.Completed, kind)
	}
	if out.Number == 0 && f.Forge.OwnFound {
		// A refresh moves a pull request that already existed, so the number
		// and the URL are the ones the gather read rather than anything this
		// call was told.
		out.Number, out.URL = f.Forge.Own.Number, f.Forge.Own.HTMLURL
	}
	return out, nil
}

// revalidate re-asks the two questions the permit was granted over,
// immediately before the first irreversible act.
//
// THE TIP, because a permit is permission to publish particular bytes:
// an `accept` in another terminal, a person's own `git commit`, a
// `bump --replace` — any of them between Gather and here and the branch
// carries something nobody authorized.
//
// THE BRANCH'S OWN PULL REQUEST, because the permit's whole shape turns
// on it: whether there are steps at all, whether the second step opens
// or refreshes, and whether the merged dead end applies. A pull request
// merged in the window would have a push resurrect work the project has
// already taken.
//
// It is a REFUSAL and not a re-decision. Authorize is the only thing
// that decides, so what this does is notice that its answer has expired
// and hand the caller ErrStale, which re-gathers and re-authorizes. A
// revalidation that adjusted the plan would be a second gate ladder in
// the function whose contract is that there is one.
func revalidate(ctx context.Context, e Env, f Facts) error {
	tip, err := e.Repo.RevParse(ctx, f.Branch)
	if err != nil {
		return err
	}
	if tip != f.Tip {
		return fmt.Errorf("%w: %s is at %s and the permit was granted over %s",
			ErrStale, f.Branch, tip, f.Tip)
	}
	if f.Forge.Upstream == "" || f.Forge.ForkOwner == "" {
		// A permit with no forge identity is --no-pr's: nothing below the
		// push asks the forge anything, so there is nothing to re-ask.
		return nil
	}
	pr, found, err := gh.QueryPR(ctx, e.Forge, f.Forge.Upstream, f.Forge.ForkOwner, f.Branch)
	if err != nil {
		return err
	}
	if found != f.Forge.OwnFound || prOpen(pr) != prOpen(f.Forge.Own) || prMerged(pr) != prMerged(f.Forge.Own) {
		return fmt.Errorf("%w: this branch's pull request is not in the state it was authorized over", ErrStale)
	}
	return nil
}

// perform is the one place an effect happens, and the switch is
// exhaustive over record.StepKind so that a step added there is a
// compile-time visit here rather than a permit that silently performs
// nothing.
//
// RecordOutcome and DeleteFork are retirement's and are unreachable from
// a permit: they are written by RetireIn and DeleteForkIn, in a
// transaction Cycle's close stage owns, and a permit that carried one
// would be a publication performing a retirement.
func perform(ctx context.Context, e Env, p Permit, kind record.StepKind) (number int, url string, err error) {
	f := p.facts
	switch kind {
	case record.PushBranch:
		if f.Asks.force(f.Invoker) {
			return 0, "", e.Repo.PushForce(ctx, f.Forge.ForkRemote, f.Branch)
		}
		return 0, "", e.Repo.Push(ctx, f.Forge.ForkRemote, f.Branch)
	case record.OpenPR:
		out, err := forgeWrite(ctx, e, "pr", "create", "--repo", f.Forge.Upstream,
			"--head", f.Forge.ForkOwner+":"+f.Branch, "--title", p.title, "--body", p.body)
		if err != nil {
			return 0, "", err
		}
		url = strings.TrimSpace(out)
		return prNumberIn(url), url, nil
	case record.RefreshPR:
		_, err := forgeWrite(ctx, e, "pr", "edit", strconv.Itoa(f.Forge.Own.Number),
			"--repo", f.Forge.Upstream, "--title", p.title, "--body", p.body)
		return f.Forge.Own.Number, f.Forge.Own.HTMLURL, err
	case record.RecordOutcome, record.DeleteFork:
		return 0, "", fmt.Errorf("%w: a permit may not carry the %s step", ErrNoPermit, kind)
	}
	return 0, "", fmt.Errorf("%w: a permit carries a step this build cannot perform (%s)", ErrNoPermit, kind)
}

// forgeWrite is the ONE place this package assembles a forge write. It
// is a funnel and not a convenience, on internal/engine's own precedent:
// the seam is a raw string-args runner, so any file here could build any
// argv, and a rule enforced beside two call sites would be a fact about
// today's call graph rather than an invariant.
//
// The shipped gh package exports no OpenPR or EditPR verb — the sketch's
// gh.Client declares two, and gh is a package this step does not rebuild
// — so the argv is spelled here, once, where the permit that authorizes
// it is in scope.
func forgeWrite(ctx context.Context, e Env, args ...string) (string, error) {
	if e.Forge == nil {
		return "", fmt.Errorf("%w: no forge client was wired for this road", ErrNoEnv)
	}
	return e.Forge(ctx, args...)
}

// prNumberIn reads the pull request's number out of the URL `gh pr
// create` prints, which is the only thing it prints. A URL that does not
// end in a number yields zero — the row would rather say it does not
// know the number than claim a wrong one, and record.Publication.Number
// omits a zero on the wire for exactly that reason.
func prNumberIn(url string) int {
	n, err := strconv.Atoi(path.Base(url))
	if err != nil {
		return 0
	}
	return n
}

// openRow is the publication row this act is recorded on: the change's
// own unsettled row where one stands, and a new one otherwise. It
// returns the id and writes nothing else.
//
// ONE OPEN ROW PER CHANGE, which is what "keyed by the change" buys. A
// publication that pushed and then died leaves a row with a Finished
// push and an Uncertain pull request; the rerun continues THAT row
// rather than opening a second one that would make the first
// unresolvable — the retire pass matches step kinds to tell "pushed but
// no PR" from "PR opened", and two rows for one change make that
// question unanswerable.
//
// BY IS WHO OPENED THE PULL REQUEST, and that is what makes it the
// provenance Spent may be derived over. It is stamped at creation from
// the invoker that made the row, and moved to the machine in exactly one
// case: the machine performing the OpenPR step on a row a person left
// without one. A person's `promote --no-pr` creates a row with By Human,
// Outcome Open and no pull request; the change then moves past that
// content, the dispatcher's publish slot takes it up, and Apply
// continues THAT row — one open row per change — and opens a real pull
// request on it. Without this the opening is invisible to Spent, and a
// machine holding N such rows opens N pull requests beyond --publish-max
// with the allowance still reporting itself unspent.
//
// It moves ONE WAY, Human to Machine, and never back. A person
// refreshing a pull request a dispatcher opened must not take that spend
// off the machine's books, which is the property the never-restamped
// rule was protecting; and a person who opens the pull request on a row
// a machine pushed takes nothing off them either, because the machine
// never opened one. Content, Target and Basis DO move with the change,
// because they describe what is being published now.
func openRow(ctx context.Context, e Env, f Facts, steps []record.StepKind) (string, error) {
	id := ""
	err := e.State.Amend(ctx, func(tx *statestore.Txn) error {
		st := tx.State()
		p, found := unsettledRow(st, f.Change.ID)
		if !found {
			p = record.Publication{ID: mintID(), Change: f.Change.ID, By: f.Invoker, Outcome: record.Open}
		}
		if f.Invoker == record.Machine && p.By != record.Machine && slices.Contains(steps, record.OpenPR) {
			p.By = record.Machine
		}
		p.Content = f.Change.Content
		p.Target = headline(f.Change).Target
		if len(f.Regions) > 0 {
			p.Basis = append([]record.Region(nil), f.Regions...)
		}
		id = p.ID
		tx.PutPublication(p)
		return nil
	})
	return id, err
}

// unsettledRow is the change's open publication, if it has one. The walk
// is over sorted ids so two readers of one state agree about which row
// they are looking at.
func unsettledRow(s statestore.State, id record.ChangeID) (record.Publication, bool) {
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		if p := s.Publications[key]; p.Change == id && !p.Outcome.Settled() {
			return p, true
		}
	}
	return record.Publication{}, false
}

// mark writes one step's phase onto the row, as its own transaction.
//
// A step of a kind already on the row is UPDATED rather than appended,
// with Attempt incremented whenever it is written Requested again — so a
// publication retried after a failure carries one step per effect with a
// count of how many times it was tried, and not a growing list of
// identical rows nothing can read a history out of.
func mark(ctx context.Context, e Env, id string, kind record.StepKind, phase record.Phase, detail string, now time.Time) error {
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		p, ok := tx.State().Publications[id]
		if !ok {
			return ErrNoRow
		}
		p.Steps = markStep(p.Steps, record.Step{Kind: kind, Phase: phase, At: now.UTC(), Detail: detail})
		tx.PutPublication(p)
		return nil
	})
}

// finish writes a step's completion, and — for the step that opens or
// moves a pull request — the number and URL beside it, in ONE
// transaction. Two writes would leave a window in which the row says a
// pull request was opened and cannot say which one, and the retire pass
// reads exactly that pair.
func finish(ctx context.Context, e Env, id string, kind record.StepKind, out Outcome, now time.Time) error {
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		p, ok := tx.State().Publications[id]
		if !ok {
			return ErrNoRow
		}
		p.Steps = markStep(p.Steps, record.Step{Kind: kind, Phase: record.Finished, At: now.UTC()})
		if out.Number != 0 {
			p.Number, p.URL = out.Number, out.URL
		}
		tx.PutPublication(p)
		return nil
	})
}

// markStep is the step list's one writer: replace the step of this kind
// where there is one, keeping its Attempt count and bumping it on a
// fresh request, and append it where there is not.
func markStep(steps []record.Step, next record.Step) []record.Step {
	out := append([]record.Step(nil), steps...)
	for i := range out {
		if out[i].Kind != next.Kind {
			continue
		}
		next.Attempt = out[i].Attempt
		if next.Phase == record.Requested {
			next.Attempt++
		}
		// A backoff belongs to the obligation that carried it. A step being
		// written again is a step being acted on, so whatever deadline was
		// holding it back is over.
		out[i] = next
		return out
	}
	if next.Phase == record.Requested {
		next.Attempt = 1
	}
	return append(out, next)
}

// mintID makes a publication's durable name. Random rather than derived
// from the change, because a change publishes, is superseded and
// publishes again, and an id that repeated would address the earlier
// row. Hex and sixteen bytes: hex because the id becomes a flat entry
// name in the state ref's tree, and sixteen bytes because a collision
// here would silently join two publications.
func mintID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "pub-" + hex.EncodeToString(b[:])
}
