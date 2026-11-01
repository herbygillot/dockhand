package engine

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// checkService is the check-execution service: a check's admission, its
// slots and the lease its driver holds, its driving, the following of it,
// its cancellation, and its recovery, one type where they were spread over
// the engine's runner, queue, slots, and a batch's drive (Codex's review of
// 275125ff, finding 4's second step). Evidence's judgments stay in
// evidence, and VMs and prefixes in the providers.
//
// It keeps nothing of its own: what it knows is the store's, so the engine
// makes one from its fields whenever it's asked (Engine.checks), compose
// and tests setting them after Open. Its dependencies are its fields, and
// what it asks of the rest of the engine is checkWork, which a later step
// narrows so the service can be a package of its own.
type checkService struct {
	// store and repository are where runs, plans, leases, and the journal
	// are recorded.
	store      store.Store
	repository model.RepositoryID
	// sessions are this process's sessions, which hold the leases.
	sessions checkSessions
	// providers are where checks build, by name; capacities how many
	// checks each takes at once (providers.<name>.capacity); localServe
	// whether serve may check in a MacPorts prefix on this Mac; and
	// tartHome the Tart home that reaches a running check's VMs.
	providers  map[string]buildenv.Provider
	capacities map[string]int
	localServe bool
	tartHome   string
	// logs is the directory checks' logs are kept in.
	logs string
	// now is the clock; poll how often a driver looks for a cancel
	// request; stall how long a target's log may go without growing, and
	// how often it's looked at (watchStalls).
	now   func() time.Time
	poll  time.Duration
	stall [2]time.Duration
	// work is what the service asks of the rest of the engine.
	work checkWork
}

// checkSessions are this process's sessions, as the engine starts them.
type checkSessions interface {
	Session(ctx context.Context, kind model.SessionKind) (*coord.Session, error)
	Observing(ctx context.Context) (context.Context, *coord.Session, func(), error)
}

// checkWork is what the check-execution service asks of the rest of the
// engine, the engine itself. Admission asks whether a passing check stands
// for a new one and removes the clones a branch's earlier checks kept;
// recovery asks a branch's preparation verdict and proposes its check as
// ship does; and driving asks the checkout a snapshot is committed in and
// reuse compares trees of, an environment's identity, the kept archives,
// the port reader the dependencies a guest may install come from, the
// install estimate, the revision's assessment, an unmet need's remedy, and
// a failed check's baseline. The driving half is what a later step
// narrows before the service moves to a package of its own.
type checkWork interface {
	Covered(ctx context.Context, branch model.BranchID, tree model.ObjectID, want model.Plan) (model.Run, bool, error)
	forgetKept(ctx context.Context, branch model.BranchID) error

	preparation(ctx context.Context, branch model.Branch) (branchstate.Prepared, error)
	ProposeCheck(ctx context.Context, request CheckRequest) (Proposal, error)

	checkout() *git.Repository
	identityNow(ctx context.Context, environment model.Environment) (string, error)
	identitiesNow(ctx context.Context, environments []model.Environment) evidence.Identities
	archivePath(digest string) string
	keptArchive(ctx context.Context, digest string) (model.Archive, bool, error)
	keepArchive(ctx context.Context, archive model.Archive, fetch func(path string) error, also func(store.Tx) error) error
	portReader() (PortReader, error)
	capabilities() (Ports, error)
	InstallEstimates(ctx context.Context, plan model.Plan, source model.Source, wait bool) []InstallEstimate
	revisionAssessments(ctx context.Context, branch model.BranchID, base, tree model.ObjectID, collect bool) ([]model.Assessment, error)
	Remedy(unmet model.Unmet) string
	continueWithBaseline(ctx context.Context, run model.Run)
}

// checks is the engine's check-execution service, made from its fields as
// they are now.
func (e *Engine) checks() *checkService {
	return &checkService{store: e.Store, repository: e.Repository, sessions: e,
		providers: e.Providers, capacities: e.Capacity, localServe: e.LocalServe, tartHome: e.TartHome,
		logs: e.LogDirectory(), now: e.now, poll: e.pollInterval(), stall: e.stall, work: e}
}

// checkout is the ports checkout, which the check-execution service
// commits a snapshot in and compares trees of.
func (e *Engine) checkout() *git.Repository { return e.Repo }

// QueueCheck is the one way a check is queued (checkService.QueueCheck).
func (e *Engine) QueueCheck(ctx context.Context, request QueueRequest) (Queued, error) {
	return e.checks().QueueCheck(ctx, request)
}

// Enqueue records a check request (checkService.Enqueue).
func (e *Engine) Enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin) (model.Run, error) {
	return e.checks().Enqueue(ctx, branch, plan, origin)
}

// Retry queues a run's exact request again (checkService.Retry).
func (e *Engine) Retry(ctx context.Context, previous model.Run) (model.Run, error) {
	return e.checks().Retry(ctx, previous)
}

// ActiveCheck is the branch's one check queued or running
// (checkService.ActiveCheck).
func (e *Engine) ActiveCheck(ctx context.Context, session *coord.Session, branch model.Branch, revision model.Revision) (*ActiveRunError, []model.Run, error) {
	return e.checks().ActiveCheck(ctx, session, branch, revision)
}

// Replace stops the runs a new check replaces (checkService.Replace).
func (e *Engine) Replace(ctx context.Context, session *coord.Session, runs []model.Run) ([]model.Run, error) {
	return e.checks().Replace(ctx, session, runs)
}

// RequestCancel records that a run should stop
// (checkService.RequestCancel).
func (e *Engine) RequestCancel(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().RequestCancel(ctx, session, id)
}

// Drive runs a run to its end under this session's lease on it
// (checkService.Drive).
func (e *Engine) Drive(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().Drive(ctx, session, id)
}

// Resume drives a run, leaving it for the next driver where ctx ends
// (checkService.Resume).
func (e *Engine) Resume(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().Resume(ctx, session, id)
}

// Leader is the serve leading the queue (checkService.Leader).
func (e *Engine) Leader(ctx context.Context, session *coord.Session) (*model.Session, error) {
	return e.checks().Leader(ctx, session)
}

// LeadOf is who sees the run through now (checkService.LeadOf).
func (e *Engine) LeadOf(ctx context.Context, session *coord.Session, id model.RunID) (Lead, error) {
	return e.checks().LeadOf(ctx, session, id)
}

// SeeThrough sees a queued run to its end (checkService.SeeThrough).
func (e *Engine) SeeThrough(ctx context.Context, session *coord.Session, id model.RunID, before func(Lead)) (model.Run, *model.Session, error) {
	return e.checks().SeeThrough(ctx, session, id, before)
}

// SeeAllThrough sees a batch's queued runs to their ends together
// (checkService.SeeAllThrough).
func (e *Engine) SeeAllThrough(ctx context.Context, ids []model.RunID, waited func(model.Run, string), ended func(model.Run, error)) {
	e.checks().SeeAllThrough(ctx, ids, waited, ended)
}

// TogetherWords say which of a batch's checks run together
// (checkService.TogetherWords).
func (e *Engine) TogetherWords(ctx context.Context, runs []model.Run) string {
	return e.checks().TogetherWords(ctx, runs)
}

// Candidates are the runs serve could drive (checkService.Candidates).
func (e *Engine) Candidates(ctx context.Context, session *coord.Session) ([]model.Run, error) {
	return e.checks().Candidates(ctx, session)
}

// Queue is every check queued or running (checkService.Queue).
func (e *Engine) Queue(ctx context.Context, session *coord.Session) ([]QueuedCheck, error) {
	return e.checks().Queue(ctx, session)
}

// RunProviders are the providers a run's plan builds on
// (checkService.RunProviders).
func (e *Engine) RunProviders(ctx context.Context, run model.Run) ([]string, error) {
	return e.checks().RunProviders(ctx, run)
}

// ResumePrepared queues the check of each branch serve prepared and never
// queued a check of (checkService.ResumePrepared).
func (e *Engine) ResumePrepared(ctx context.Context, where Where, tests model.TestPolicy) ([]Resumed, error) {
	return e.checks().ResumePrepared(ctx, where, tests)
}

// run reads a run.
func (c *checkService) run(ctx context.Context, id model.RunID) (model.Run, error) {
	var run model.Run
	err := c.store.View(ctx, c.repository, func(r store.Reader) error {
		var err error
		run, err = r.Run(id)
		return err
	})
	return run, err
}

// plan reads a plan.
func (c *checkService) plan(ctx context.Context, id model.PlanID) (model.Plan, error) {
	var plan model.Plan
	err := c.store.View(ctx, c.repository, func(r store.Reader) error {
		var err error
		plan, err = r.Plan(id)
		return err
	})
	return plan, err
}

// admit takes a run's lease in tx, the slot its driver holds, and records
// the run's start where it has none: when it took its slot, which it
// waited for from being queued (rc1's G13: check-822's wait for a Tart
// slot was read only from its finish less its took). A run resumed after
// its driver ended keeps the start it had.
func (c *checkService) admit(tx store.Tx, session *coord.Session, id model.RunID) (model.Lease, error) {
	lease, err := session.AcquireIn(tx, RunResource(id))
	if err != nil {
		return lease, err
	}
	run, err := tx.Run(id)
	if err != nil || run.StartedAt != nil || run.State.Terminal() {
		return lease, err
	}
	run.StartedAt = new(c.now())
	return lease, tx.UpdateRun(run)
}
