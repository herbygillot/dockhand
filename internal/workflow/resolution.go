package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Kind is what a selection resolves to. See docs/resolution-design.md.
type Kind string

const (
	// Fresh starts from master: no open contribution, or one retired by
	// what master and the forge showed.
	Fresh Kind = "fresh"
	// Continue continues a prior job of the action from the source it
	// recorded, with its choices inherited.
	Continue Kind = "continue"
	// Onto prepares onto an open contribution's current revision and
	// replaces its branch head: a contribution with no bump job of its
	// own, or whose last bump was itself prepared onto it.
	Onto Kind = "onto"
	// Adopt tracked a branch dockhand did not make, and prepares onto it.
	Adopt Kind = "adopt"
	// Tracked is a contribution as recorded, with its current revision:
	// what an action that prepares no update, a verification or a
	// correction, works on. It decides nothing about master or a prior
	// job, and its Source is the revision's.
	Tracked Kind = "tracked"
)

// Resolution is what one selection means for one action: the one value
// the bump, the verification, the preview, and adoption read, computed
// once by the engine from the records, the repository, and master.
type Resolution struct {
	Kind Kind
	// Source is what the edit runs on: master, the prior job's recorded
	// source, or the contribution's current revision.
	Source record.Source
	// Master is master as fetched, when it was; a Continue reached with
	// master unreachable has none, and Degraded says why.
	Master   *record.Source
	Degraded string
	// Change and Revision are the contribution and revision the work
	// lands on, for Continue, Onto, Adopt, and Tracked.
	Change   *record.Change
	Revision *record.Revision
	// Prior is the job a Continue inherits from.
	Prior *record.Job
	// Selection is the target as the records name it, with the person's
	// variant choices laid over the recorded ones.
	Selection macports.Selection
	// Intent, Subject, and References are the request's; a Continue
	// inherits all three from the prior job where the request left them
	// empty, an Onto inherits only the subject, from the contribution's
	// commit, and a Fresh inherits nothing.
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
	// Branch is what the work lands on: master's name for a Fresh, the
	// contribution's branch otherwise.
	Branch string
	// Checked is whether master and the pull request were consulted for a
	// Continue; a preview does not consult them, and says so in Detail.
	Checked bool
	// Detail is the one line a command reports: what master holds, what
	// the pull request is, why the contribution continues or retires.
	Detail string
}

// ChangeID is the contribution's, or empty for a Fresh.
func (r Resolution) ChangeID() record.ChangeID {
	if r.Change == nil {
		return ""
	}
	return r.Change.ID
}

// ResolutionRequest is the selection as the person made it. An action that
// prepares an update, a bump, a revision bump, or a checksum refresh,
// resolves Fresh, Continue, or Onto; any other action resolves Tracked, the
// contribution as recorded, or the error the records give.
type ResolutionRequest struct {
	Action    record.Action
	Selection macports.Selection
	ChangeID  record.ChangeID
	// Branch selects the contribution tracked on it.
	Branch string
	// Preview reads and never writes: master is fetched, the pull request
	// is not refreshed and the continuation is not checked, and nothing
	// is recorded. A Continue found this way is unchecked, and says so.
	Preview    bool
	Platform   record.Platform
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
}

// Resolve reads and never writes. It is the one engine method that accepts
// a nil store: with no database there are no records, so every update is
// Fresh and nothing is Tracked. The engine fetches master itself when a
// resolution needs it.
func (e *Engine) Resolve(ctx context.Context, request ResolutionRequest) (Resolution, error) {
	if e == nil || e.Repo == nil {
		return Resolution{}, errNoState
	}
	if !request.Action.Updates() {
		return e.resolveTracked(ctx, request)
	}
	named := macports.ValidName(request.Selection.Selector)
	if e.State == nil || !named && request.ChangeID == "" && request.Branch == "" {
		return e.resolveFresh(ctx, request, "")
	}
	if e.Repository == "" {
		return Resolution{}, errNoState
	}
	selector := ContributionSelector{ChangeID: request.ChangeID, Branch: request.Branch}
	if named {
		selector.Target = request.Selection.Selector
	}
	prior, change, err := e.preparationInput(ctx, selector, request.Action)
	if err != nil {
		return Resolution{}, err
	}
	if change == nil {
		return e.resolveFresh(ctx, request, "")
	}
	// A contribution with no job of the action, or whose last job was
	// itself prepared onto it, takes the update onto its own revision.
	if prior == nil || prior.Spec.Preparation != nil && prior.Spec.Preparation.Correction != nil {
		return e.resolveOnto(ctx, request, *change)
	}
	resolution := e.continued(request, *prior, *change)
	master, err := e.FetchMaster(ctx)
	if err != nil {
		// An unreachable master leaves the recorded source as the only
		// fact, and says so.
		resolution.Degraded = err.Error()
		resolution.Detail = fmt.Sprintf("master not checked (%v); the open contribution is continued as recorded", err)
		progress.Report(ctx, "%s", resolution.Detail)
		return resolution, nil
	}
	resolution.Master = &master
	if request.Preview {
		resolution.Detail = "continuation not checked in a preview; the open contribution is continued as recorded"
	} else {
		decision, err := e.CheckContinuation(ctx, *prior, master, request.Platform)
		if err != nil {
			return Resolution{}, err
		}
		progress.Report(ctx, "%s", decision.Detail)
		if decision.Fresh {
			return e.resolveFresh(ctx, request, decision.Detail)
		}
		resolution.Checked, resolution.Detail = true, decision.Detail
	}
	progress.Report(ctx, "Continuing the port's open contribution from its recorded source")
	progress.VerboseReport(ctx, "Continuing contribution %s from recorded source %s", change.ID, prior.Spec.Source.Commit)
	return resolution, nil
}

// resolveTracked is the contribution as recorded, for an action that
// prepares no update of its own: what a verification or a correction
// works on. It reads the records and nothing else, and a selection with
// no open contribution is the error the records give.
func (e *Engine) resolveTracked(ctx context.Context, request ResolutionRequest) (Resolution, error) {
	if e.State == nil || e.Repository == "" {
		return Resolution{}, errNoState
	}
	change, err := e.SelectContribution(ctx, ContributionSelector{Target: request.Selection.Selector, Branch: request.Branch, ChangeID: request.ChangeID})
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{Kind: Tracked, Change: &change, Selection: request.Selection, Intent: request.Intent, Subject: request.Subject, References: request.References, Branch: change.Branch}
	if change.CurrentRevision != "" {
		revision, err := e.CurrentRevision(ctx, change)
		if err != nil {
			return Resolution{}, err
		}
		resolution.Revision, resolution.Source = &revision, revision.Source
	}
	return resolution, nil
}

// continued is a Continue of the prior job: its recorded source and its
// choices, with the request's laid over.
func (e *Engine) continued(request ResolutionRequest, prior record.Job, change record.Change) Resolution {
	resolution := Resolution{Kind: Continue, Source: prior.Spec.Source, Change: &change, Prior: &prior, Intent: request.Intent, Subject: request.Subject, References: request.References, Branch: change.Branch}
	if prior.Spec.Preparation != nil {
		resolution.Intent.SharedRelease = request.Intent.SharedRelease || prior.Spec.Preparation.SharedRelease
		resolution.Intent.KeepOldChecksums = request.Intent.KeepOldChecksums || prior.Spec.Preparation.KeepOldChecksums
		resolution.Intent.Stub = prior.Spec.Preparation.Stub
	}
	// A continued contribution keeps the subject and tickets it was given;
	// the console's retry names only the port.
	if resolution.Subject == "" {
		resolution.Subject = prior.Spec.Subject
	}
	if len(resolution.References) == 0 {
		resolution.References = prior.Spec.References
	}
	target := prior.Spec.Targets[0]
	variants := maps.Clone(target.Variants)
	if variants == nil {
		variants = map[string]bool{}
	}
	maps.Copy(variants, request.Selection.Variants)
	resolution.Selection = macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}
	return resolution
}

// resolveFresh is a Fresh from master, with the detail that retired a
// contribution when one did.
func (e *Engine) resolveFresh(ctx context.Context, request ResolutionRequest, detail string) (Resolution, error) {
	master, err := e.FetchMaster(ctx)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Kind: Fresh, Source: master, Master: &master, Selection: request.Selection, Intent: request.Intent, Subject: request.Subject, References: request.References, Branch: macports.PortsBranch, Detail: detail}, nil
}

// resolveOnto is an update onto an open contribution's current revision.
// The binding reconfirms the preconditions integration checks; the
// resolution reads the contribution and its revision.
func (e *Engine) resolveOnto(ctx context.Context, request ResolutionRequest, change record.Change) (Resolution, error) {
	if change.Disposition != record.ChangeOpen || change.Branch == "" || change.CurrentRevision == "" {
		return Resolution{}, fmt.Errorf("%w: contribution %s is not open on a branch", ErrInvalidRequest, change.ID)
	}
	revision, err := e.CurrentRevision(ctx, change)
	if err != nil {
		return Resolution{}, err
	}
	resolution, err := e.onto(ctx, request, Onto, change, revision)
	if err != nil {
		return Resolution{}, err
	}
	progress.Report(ctx, "%s", resolution.Detail)
	return resolution, nil
}

// onto builds an Onto or Adopt resolution over a contribution's revision.
func (e *Engine) onto(ctx context.Context, request ResolutionRequest, kind Kind, change record.Change, revision record.Revision) (Resolution, error) {
	subject, err := ContributionSubject(ctx, e.Repo, revision.Source, request.Subject)
	if err != nil {
		return Resolution{}, err
	}
	selection, err := (contribution{change: change, revision: revision}).selection(request.Selection.Variants)
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{Kind: kind, Source: revision.Source, Change: &change, Revision: &revision, Selection: selection, Intent: request.Intent, Subject: subject, References: request.References, Branch: change.Branch,
		Detail: fmt.Sprintf("Preparing the update onto %s's open contribution, branch %s; it lands as an amendment", initiatingNameOf(change), change.Branch)}
	return resolution, nil
}

// ResolveAdopted is the Adopt kind: what a bump onto a branch adoption
// just tracked, or in a dry run would have tracked, means. Adoption is the
// engine's one write on the way to a resolution, and it is the caller's
// call, made first; this reads its result and writes nothing.
func (e *Engine) ResolveAdopted(ctx context.Context, adopted AdoptResult, request ResolutionRequest) (Resolution, error) {
	if e == nil || e.Repo == nil {
		return Resolution{}, errNoState
	}
	if !request.Action.Updates() {
		return Resolution{}, fmt.Errorf("%w: %s does not prepare onto an adopted branch", ErrInvalidRequest, request.Action)
	}
	return e.onto(ctx, request, Adopt, adopted.Change, adopted.Revision)
}

// FetchMaster freezes authoritative master: its commit and tree, and the
// commit as the base a contribution is measured against.
func (e *Engine) FetchMaster(ctx context.Context) (record.Source, error) {
	progress.VerboseReport(ctx, "Fetching MacPorts master")
	commit, tree, err := e.Repo.FetchBranch(ctx, macports.PortsRepositoryURL, macports.PortsBranch)
	if err != nil {
		return record.Source{}, fmt.Errorf("fetching authoritative MacPorts master: %w", err)
	}
	return record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commit)}, nil
}

// preparationInput is the open contribution for a selection and the newest
// job of the action that is still its current revision or still running.
func (e *Engine) preparationInput(ctx context.Context, selector ContributionSelector, action record.Action) (*record.Job, *record.Change, error) {
	var result *record.Job
	var contribution *record.Change
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		change, err := selectContribution(ctx, r, selector)
		if errors.Is(err, state.ErrNotFound) && selector.ChangeID == "" && selector.Branch == "" {
			return nil
		}
		if err != nil {
			return err
		}
		contribution = &change
		history, err := r.JobHistory(ctx, change.ID)
		if err != nil {
			return err
		}
		newest, ok := newestJob(history, func(job record.Job) bool { return job.Spec.Action == action })
		if ok && (change.CurrentRevision == "" || newest.ResultRevision == change.CurrentRevision || !newest.State.Terminal()) {
			result = &newest
		}
		return nil
	})
	return result, contribution, err
}
