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
	// lands on, for Continue, Onto, and Adopt.
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

// ResolutionRequest is the selection as the person made it.
type ResolutionRequest struct {
	Action    record.Action
	Selection macports.Selection
	ChangeID  record.ChangeID
	Branch    string
	// Adopt tracks Branch first; with Preview it is tracked in a dry run
	// and the resolution reads the revision adoption would have recorded.
	Adopt bool
	// Squash and KeepBody are adoption's choices.
	Squash, KeepBody bool
	// Require refuses a selection with no open contribution rather than
	// resolving Fresh: a verification continues something or says so.
	Require bool
	// Lookup stops after the contribution and the prior job are read:
	// master is not fetched and the continuation is not checked. A
	// verification resolves this way.
	Lookup bool
	// Preview reads and never writes: master is fetched, the pull request
	// is not refreshed and the continuation is not checked, and nothing
	// is recorded. A Continue found this way is unchecked, and says so.
	Preview bool
	// Offline forbids the master fetch: a resolution that would need it is
	// refused rather than made.
	Offline    bool
	Platform   record.Platform
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
}

// ErrOffline is a resolution that needed master when the request forbade
// fetching it.
var ErrOffline = errors.New("workflow: resolution needs master, which the request forbade fetching")

// Resolve is the one engine method that accepts a nil store: with no
// database there are no records, so every selection is Fresh, and adoption
// is refused. The engine fetches master itself when a resolution needs it.
func (e *Engine) Resolve(ctx context.Context, request ResolutionRequest) (Resolution, error) {
	if e == nil || e.Repo == nil {
		return Resolution{}, errNoState
	}
	if request.Adopt {
		return e.resolveAdoption(ctx, request)
	}
	named := macports.ValidName(request.Selection.Selector)
	if e.State == nil || !named && request.ChangeID == "" {
		return e.resolveFresh(ctx, request, "")
	}
	if e.Repository == "" {
		return Resolution{}, errNoState
	}
	selector := ContributionSelector{ChangeID: request.ChangeID}
	if named {
		selector.Target = request.Selection.Selector
	}
	prior, change, err := e.preparationInput(ctx, selector, request.Action)
	if err != nil {
		return Resolution{}, err
	}
	if change == nil {
		if request.Require {
			_, err := e.SelectContribution(ctx, selector)
			return Resolution{}, err
		}
		if request.Lookup {
			return Resolution{Kind: Fresh, Selection: request.Selection, Intent: request.Intent, Subject: request.Subject, References: request.References, Branch: macports.PortsBranch}, nil
		}
		return e.resolveFresh(ctx, request, "")
	}
	// A contribution with no job of the action, or whose last job was
	// itself prepared onto it, takes the update onto its own revision.
	if prior == nil || prior.Spec.Preparation != nil && prior.Spec.Preparation.Correction != nil {
		return e.resolveOnto(ctx, request, change.ID)
	}
	resolution := e.continued(request, *prior, *change)
	if request.Lookup {
		return resolution, nil
	}
	if request.Offline {
		return Resolution{}, ErrOffline
	}
	master, err := e.fetchMaster(ctx)
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
	if request.Offline {
		return Resolution{}, ErrOffline
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Kind: Fresh, Source: master, Master: &master, Selection: request.Selection, Intent: request.Intent, Subject: request.Subject, References: request.References, Branch: macports.PortsBranch, Detail: detail}, nil
}

// resolveOnto is an update onto an open contribution's current revision.
func (e *Engine) resolveOnto(ctx context.Context, request ResolutionRequest, id record.ChangeID) (Resolution, error) {
	bound, err := e.bindOnto(ctx, id)
	if err != nil {
		return Resolution{}, err
	}
	return e.onto(ctx, request, Onto, bound.change, bound.revision)
}

// onto builds an Onto or Adopt resolution over a contribution's revision.
func (e *Engine) onto(ctx context.Context, request ResolutionRequest, kind Kind, change record.Change, revision record.Revision) (Resolution, error) {
	subject, err := ContributionSubject(ctx, e.Repo, revision.Source, request.Subject)
	if err != nil {
		return Resolution{}, err
	}
	target := onto{change: change, revision: revision}
	resolution := Resolution{Kind: kind, Source: revision.Source, Change: &change, Revision: &revision, Selection: target.selection(request.Selection.Variants), Intent: request.Intent, Subject: subject, References: request.References, Branch: change.Branch,
		Detail: fmt.Sprintf("Preparing the update onto %s's open contribution, branch %s; it lands as an amendment", initiatingNameOf(change), change.Branch)}
	return resolution, nil
}

// resolveAdoption tracks the branch, in a dry run under Preview, and
// prepares onto the revision adoption recorded or would have recorded.
func (e *Engine) resolveAdoption(ctx context.Context, request ResolutionRequest) (Resolution, error) {
	if e.State == nil {
		return Resolution{}, fmt.Errorf("%w: adopting a branch needs the state database", ErrInvalidRequest)
	}
	if request.Offline {
		return Resolution{}, ErrOffline
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Resolution{}, err
	}
	adopted, err := e.AdoptContribution(ctx, AdoptRequest{Branch: request.Branch, Target: request.Selection.Selector, Upstream: master.Commit, Platform: request.Platform, DryRun: request.Preview, Squash: request.Squash, KeepBody: request.KeepBody})
	if err != nil {
		return Resolution{}, err
	}
	progress.Report(ctx, "%s", adopted.Detail)
	resolution, err := e.onto(ctx, request, Adopt, adopted.Change, adopted.Revision)
	if err != nil {
		return Resolution{}, err
	}
	resolution.Master = &master
	return resolution, nil
}

// fetchMaster freezes authoritative master.
func (e *Engine) fetchMaster(ctx context.Context) (record.Source, error) {
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
