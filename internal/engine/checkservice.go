package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// checks is the engine's check-execution service (checks.Service), made
// from its fields as they are now, since it keeps nothing of its own:
// compose and tests set them after Open. Its port readers are found when a
// driver first asks, as capabilities finds MacPorts' own.
func (e *Engine) checks() *checks.Service {
	return &checks.Service{Store: e.Store, Repository: e.Repository, Sessions: e,
		Providers: e.Providers, Capacities: e.Capacity, LocalServe: e.LocalServe, TartHome: e.TartHome,
		Logs: e.LogDirectory(), Launches: e.LaunchDirectory(), Checkout: e.Repo, Ports: e.checkPorts, Archives: e.archives(),
		Admission: checkAdmission{e}, Hooks: checkHooks{e}, Now: e.now, Poll: e.pollInterval(), Stall: e.stall}
}

// archives are the checkout's kept archives, in ArchiveDirectory.
func (e *Engine) archives() *checks.Archives {
	return &checks.Archives{Directory: e.ArchiveDirectory(), Store: e.Store, Repository: e.Repository, Now: e.now}
}

// checkPorts are the port reader and the dependency graph reader a
// check's driver reads the dependencies a guest may install with.
func (e *Engine) checkPorts() (checks.PortReader, checks.DependencyGraphReader, error) {
	ports, err := e.capabilities()
	return ports.Reader, ports.Graphs, err
}

// checkAdmission is what queuing a check asks of the engine: whether a
// check that passed for the files stands for the new one, and the removal
// of the clones a branch's earlier checks kept.
type checkAdmission struct{ e *Engine }

func (a checkAdmission) Covered(ctx context.Context, branch model.BranchID, tree model.ObjectID, want model.Plan) (model.Run, bool, error) {
	return a.e.Covered(ctx, branch, tree, want)
}

func (a checkAdmission) ForgetKept(ctx context.Context, branch model.BranchID) error {
	return a.e.forgetKept(ctx, branch)
}

// checkHooks are what the engine does as a check is driven: say the
// install estimate beside an environment's first clone, assess upstream's
// change once the check has built, queue the baseline a failed check
// points to, and give reuse a revision's change records and a directory's
// evaluation at a revision.
type checkHooks struct{ e *Engine }

func (h checkHooks) BeforeEnvironment(ctx context.Context, _ model.Run, plan model.Plan, environment model.Environment, source model.Source) []string {
	plan.Environments = []model.Environment{environment}
	var lines []string
	for _, estimate := range h.e.InstallEstimates(ctx, plan, source, true) {
		lines = append(lines, estimate.Words())
	}
	return lines
}

func (h checkHooks) AfterBuild(ctx context.Context, run model.Run, revision model.Revision) error {
	_, err := h.e.revisionAssessments(ctx, run.Branch, revision.Source.Base, revision.Source.Tree, true)
	return err
}

func (h checkHooks) AfterFinish(ctx context.Context, run model.Run) {
	h.e.continueWithBaseline(ctx, run)
}

func (h checkHooks) Changes(ctx context.Context, revision model.Revision) (map[string]model.ChangeRecord, error) {
	return h.e.revisionChanges(ctx, revision.Branch, revision.Source.Base, revision.Source.Tree, true)
}

func (h checkHooks) Family(ctx context.Context, revision model.Revision, directory string) (macports.Snapshot, error) {
	return h.e.revisionFamily(ctx, revision, directory)
}

// QueueCheck is the one way a check is queued (checks.Service.QueueCheck).
func (e *Engine) QueueCheck(ctx context.Context, request checks.QueueRequest) (checks.Queued, error) {
	return e.checks().QueueCheck(ctx, request)
}

// Enqueue records a check request (checks.Service.Enqueue).
func (e *Engine) Enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin) (model.Run, error) {
	return e.checks().Enqueue(ctx, branch, plan, origin)
}

// Retry queues a run's exact request again (checks.Service.Retry).
func (e *Engine) Retry(ctx context.Context, previous model.Run) (model.Run, error) {
	return e.checks().Retry(ctx, previous)
}

// ActiveCheck is the branch's one check queued or running
// (checks.Service.ActiveCheck).
func (e *Engine) ActiveCheck(ctx context.Context, session *coord.Session, branch model.Branch, revision model.Revision) (*checks.ActiveRunError, []model.Run, error) {
	return e.checks().ActiveCheck(ctx, session, branch, revision)
}

// Replace stops the runs a new check replaces (checks.Service.Replace).
func (e *Engine) Replace(ctx context.Context, session *coord.Session, runs []model.Run) ([]model.Run, error) {
	return e.checks().Replace(ctx, session, runs)
}

// RequestCancel records that a run should stop
// (checks.Service.RequestCancel).
func (e *Engine) RequestCancel(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().RequestCancel(ctx, session, id)
}

// Drive runs a run to its end under this session's lease on it
// (checks.Service.Drive).
func (e *Engine) Drive(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().Drive(ctx, session, id)
}

// Resume drives a run, leaving it for the next driver where ctx ends
// (checks.Service.Resume).
func (e *Engine) Resume(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.checks().Resume(ctx, session, id)
}

// Leader is the serve leading the queue (checks.Service.Leader).
func (e *Engine) Leader(ctx context.Context, session *coord.Session) (*model.Session, error) {
	return e.checks().Leader(ctx, session)
}

// LeadOf is who sees the run through now (checks.Service.LeadOf).
func (e *Engine) LeadOf(ctx context.Context, session *coord.Session, id model.RunID) (checks.Lead, error) {
	return e.checks().LeadOf(ctx, session, id)
}

// Observe follows a run until it ends, driving nothing
// (checks.Service.Observe).
func (e *Engine) Observe(ctx context.Context, id model.RunID) (model.Run, error) {
	return e.checks().Observe(ctx, id)
}

// SeeThrough sees a queued run to its end (checks.Service.SeeThrough).
func (e *Engine) SeeThrough(ctx context.Context, session *coord.Session, id model.RunID, before func(checks.Lead)) (model.Run, *model.Session, error) {
	return e.checks().SeeThrough(ctx, session, id, before)
}

// SeeAllThrough sees a batch's queued runs to their ends together
// (checks.Service.SeeAllThrough).
func (e *Engine) SeeAllThrough(ctx context.Context, ids []model.RunID, waited func(model.Run, string), ended func(model.Run, error)) {
	e.checks().SeeAllThrough(ctx, ids, waited, ended)
}

// TogetherWords say which of a batch's checks run together
// (checks.Service.TogetherWords).
func (e *Engine) TogetherWords(ctx context.Context, runs []model.Run) string {
	return e.checks().TogetherWords(ctx, runs)
}

// Candidates are the runs serve could drive (checks.Service.Candidates).
func (e *Engine) Candidates(ctx context.Context, session *coord.Session) ([]model.Run, error) {
	return e.checks().Candidates(ctx, session)
}

// Queue is every check queued or running (checks.Service.Queue).
func (e *Engine) Queue(ctx context.Context, session *coord.Session) ([]checks.QueuedCheck, error) {
	return e.checks().Queue(ctx, session)
}

// RunProviders are the providers a run's plan builds on
// (checks.Service.RunProviders).
func (e *Engine) RunProviders(ctx context.Context, run model.Run) ([]string, error) {
	return e.checks().RunProviders(ctx, run)
}

// Remedy is how to give an environment what an unmet target needs, when
// its provider can say (checks.Service.Remedy).
func (e *Engine) Remedy(unmet model.Unmet) string {
	return e.checks().Remedy(unmet)
}
