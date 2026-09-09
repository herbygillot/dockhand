package publish

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Advanced is what carrying one change through the permit came to.
//
// Out STANDS EVEN WHEN Err IS SET, which is why Ran is separate from
// both: Apply's outcome is what actually reached the forge, and a call
// that failed after an effect landed has an outcome worth recording. A
// caller that read Err and discarded Out would forget the push it just
// made.
type Advanced struct {
	Advisories []Advisory
	Err        error
	Out        Outcome
	Ran        bool
}

// AdvanceFrom asks for a permit on facts already gathered, and applies
// it. Authorize is still the only thing that decides; this is the three
// steps that always follow it in the same order.
//
// THE FACTS ARE PASSED AND NOT GATHERED, because both callers already
// hold them and each got them for its own reason — a resumption gathers
// before it reconciles, and the machine slot gathers to see whether a
// candidate is publishable at all. A ladder that gathered again would
// ask the forge twice per change per pass.
//
// A no-op permit returns with Ran false and no error: the branch's own
// pull request is already open at this tip, which is not a failure and
// not a publication.
func AdvanceFrom(ctx context.Context, env Env, f Facts, pace Pace) Advanced {
	var a Advanced
	permit, adv, err := Authorize(f, pace)
	a.Advisories = adv
	if err != nil {
		a.Err = err
		return a
	}
	if permit.NoOp() {
		return a
	}
	// Ran before Err on purpose: Apply's outcome is what reached the
	// forge, and it stands whether or not the call came back clean.
	a.Out, a.Err = Apply(ctx, env, permit)
	a.Ran = true
	return a
}

// Resumed is one unfinished publication, finished — an Advanced with
// the change it belongs to.
type Resumed struct {
	Change record.ChangeID
	Advanced
}

// Resume finishes every publication whose journal shows work started and
// not completed.
//
// THE OBSERVATION COMES FIRST AND IS NOT A DECISION. Reconcile asks the
// remote for the branch's object and reads the gather's forge answer,
// and marks Finished only what it can confirm — so a step whose call
// errored after the effect landed stops being retried, and a step that
// genuinely did not happen stays owed. Only then is Authorize asked, and
// Authorize is still the only thing that decides.
//
// A ROW WHOSE STEPS ALL RECONCILE NEEDS NOTHING FURTHER, and Authorize
// says so itself: the permit is a no-op, because the branch's own pull
// request is open at this tip. The row is left for the retirement stage,
// which is whose it is once the forge has it.
//
// IT LIVES HERE AND NOT IN THE PASS THAT CALLS IT. The ladder —
// reconcile, ask what is still owed, authorize, apply — is this
// package's own sequence, and an operation that spelled it out step by
// step would be doing publication's job rather than deciding when in a
// pass it happens. What the caller keeps is the ordering and the
// reporting: which stage runs when, and what a person is told.
// The state is PASSED and not read: the pass that calls this has
// already read one, and a second read inside a stage would be a second
// snapshot of a store the caller is holding steady.
func Resume(ctx context.Context, env Env, st statestore.State, forge ForgePolicy, invoker record.Driver, now time.Time) []Resumed {
	var out []Resumed
	for _, pub := range Unfinished(st) {
		r := Resumed{Change: pub.Change}
		old := st.Changes[string(pub.Change)]
		if old.ID == "" {
			// A row naming a change the store no longer holds. Compaction
			// roots a change its publications need, so this is a hand or an
			// older build; it is reported rather than guessed at.
			r.Err = ErrNoChange
			out = append(out, r)
			continue
		}
		r.Change = old.ID
		ref, rerr := change.Resolve(ctx, env.Repo, env.State, change.TargetFor(old))
		if rerr != nil {
			r.Err = rerr
			out = append(out, r)
			continue
		}
		f, gerr := Gather(ctx, env, ref, forge, Asks{}, invoker, now)
		if gerr != nil {
			r.Err = gerr
			out = append(out, r)
			continue
		}
		row, cerr := Reconcile(ctx, env, pub, f)
		if cerr != nil {
			r.Err = cerr
			out = append(out, r)
			continue
		}
		if !Owed(row) {
			continue // the world had already done what the journal was unsure of
		}
		r.Advanced = AdvanceFrom(ctx, env, f, Pace{})
		if r.Err == nil && !r.Ran && len(r.Advisories) == 0 {
			continue // the permit was a no-op and there was nothing to say
		}
		out = append(out, r)
	}
	return out
}
