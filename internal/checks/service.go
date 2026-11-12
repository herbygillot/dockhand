// Package checks is the check-execution service: a check's admission, its
// slots and the lease its driver holds, its driving, the following of it,
// its cancellation, and the recovery of a run whose driver is gone, one
// type where they were spread over the engine's runner, queue, slots, and
// a batch's drive (Codex's review of 275125ff, finding 4's second step).
// Evidence's judgments stay in evidence, VMs and prefixes in the
// providers, and what a check means for a branch, its plan, its
// assessment, and its baseline, in the engine, which composes a Service
// and drives it.
package checks

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Service is the check-execution service. It keeps nothing of its own:
// what it knows is the store's, so the engine makes one from its fields
// whenever it's asked, compose and tests setting them after the engine
// opens. Its dependencies are its fields; what it asks of the rest of
// dockhand is Admission and Hooks.
type Service struct {
	// Store and Repository are where runs, plans, leases, and the journal
	// are recorded.
	Store      store.Store
	Repository model.RepositoryID
	// Sessions are this process's sessions, which hold the leases.
	Sessions Sessions
	// Providers are where checks build, by name; Capacities how many
	// checks each takes at once (providers.<name>.capacity); LocalServe
	// whether serve may check in a MacPorts prefix on this Mac; and
	// TartHome the Tart home that reaches a running check's VMs.
	Providers  map[string]buildenv.Provider
	Capacities map[string]int
	LocalServe bool
	TartHome   string
	// Logs is the directory checks' logs are kept in, and Launches the one
	// attempts' launch locks are (launchLockPath).
	Logs     string
	Launches string
	// Checkout is the ports checkout a snapshot is committed in for a
	// guest to fetch, and whose trees reuse compares.
	Checkout *git.Repository
	// Ports gives the port reader the dependencies a guest may install are
	// read with, and the dependency graph they're followed through, when a
	// driver first needs them: compose builds MacPorts' reader lazily. An
	// error, or a nil reader, offers no dependencies' archives; a nil graph
	// offers the direct dependencies alone.
	Ports func() (PortReader, DependencyGraphReader, error)
	// Archives are the kept archives a guest installs from and a build
	// keeps.
	Archives *Archives
	// Admission is what queuing a check asks of the rest of dockhand.
	Admission Admission
	// Hooks are what the rest of dockhand does as a check is driven; nil
	// does nothing.
	Hooks Hooks
	// Now is the clock; Poll how often a driver looks for a cancel
	// request; Stall how long a target's log may go without growing, and
	// how often it's looked at (watchStalls), stallAfter and stallPoll
	// where zero.
	Now   func() time.Time
	Poll  time.Duration
	Stall [2]time.Duration
}

// Sessions are this process's sessions, as the engine starts them.
type Sessions interface {
	// Session records this process's session of kind and keeps its
	// heartbeat until ctx ends.
	Session(ctx context.Context, kind model.SessionKind) (*coord.Session, error)
	// Observing gives one observer session on ctx, which every judgment
	// made under ctx shares, and the function that ends it.
	Observing(ctx context.Context) (context.Context, *coord.Session, func(), error)
}

// Admission is what queuing a check asks of the rest of dockhand: whether
// a check that passed for these files stands for the new one, and the
// removal of the clones a branch's earlier checks kept for dockhand shell,
// which its new check replaces.
type Admission interface {
	Covered(ctx context.Context, branch model.BranchID, tree model.ObjectID, want model.Plan) (model.Run, bool, error)
	ForgetKept(ctx context.Context, branch model.BranchID) error
}

// Hooks are what the rest of dockhand does as a check is driven, each at
// its moment and none failing the check but Evaluating, which keeps it
// from being driven at all.
type Hooks interface {
	// Evaluating marks the work under ctx as evaluating the run's branch's
	// Portfiles, where they are another's, so every read a driver makes
	// carries the run's owner (#162); an error is a branch whose author's
	// commit the person hasn't allowed, and the run isn't driven.
	Evaluating(ctx context.Context, run model.Run) (context.Context, error)
	// BeforeEnvironment is said beside an environment's first clone, never
	// delaying it, within estimateWait: the lines of what the check will
	// install there, each said as the environment's progress.
	BeforeEnvironment(ctx context.Context, run model.Run, plan model.Plan, environment model.Environment, source model.Source) []string
	// AfterBuild is done once every environment has built and before the
	// run settles, so the submission that follows finds it: what
	// upstream's change means for each port the revision changes. Its
	// error is said as progress, never the check's failure.
	AfterBuild(ctx context.Context, run model.Run, revision model.Revision) error
	// AfterFinish is done once a failed run has settled: the baseline it
	// points to, where check.baseline asks.
	AfterFinish(ctx context.Context, run model.Run)
	// Changes are a revision's change records, by directory, made where
	// the revision has none yet, as status and submit make them: what
	// reuse confirms an inert change by (reuse.Inert). An error, or no
	// record of a directory, carries nothing over.
	Changes(ctx context.Context, revision model.Revision) (map[string]model.ChangeRecord, error)
	// Family is a port directory evaluated as a revision holds it, every
	// subport of it, on this Mac, as a change record's sides are: what
	// reuse confirms an inert change to a port both revisions add by,
	// whose records, against a base without it, can't
	// (fidelity.InertBetween). An error, as for a tree this Mac doesn't
	// hold, carries nothing over.
	Family(ctx context.Context, revision model.Revision, directory string) (macports.Snapshot, error)
}

// PortReader reads ports from a source tree: the ports a directory defines
// on a platform, main port first, and the directory that defines a port by
// name. MacPorts' own evaluator is the real one.
type PortReader interface {
	// Ports reads a directory as it reads in an environment: on its
	// platform, with its developer tools where it states them, and with the
	// variants chosen, its defaults where none are.
	Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error)
	Directory(ctx context.Context, source model.Source, name string) (string, error)
}

// Linter is a port reader that can run port lint on this Mac, on a
// directory's port as a source holds it, with --nitpick where asked
// (macports.Linter): reuse runs it on a Portfile whose change is inert,
// so the result carried over says what lint says of it now.
type Linter interface {
	Lint(ctx context.Context, source model.Source, directory, port string, nitpick bool) (macports.Lint, error)
}

// DependencyGraphReader is a port reader that can read every port's direct
// dependencies at once, by lowercased name, from the port index.
type DependencyGraphReader interface {
	DependencyGraph(ctx context.Context, source model.Source) (map[string][]string, error)
}

// run reads a run.
func (c *Service) run(ctx context.Context, id model.RunID) (model.Run, error) {
	var run model.Run
	err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		var err error
		run, err = r.Run(id)
		return err
	})
	return run, err
}

// plan reads a plan.
func (c *Service) plan(ctx context.Context, id model.PlanID) (model.Plan, error) {
	var plan model.Plan
	err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
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
func (c *Service) admit(tx store.Tx, session *coord.Session, id model.RunID) (model.Lease, error) {
	lease, err := session.AcquireIn(tx, RunResource(id))
	if err != nil {
		return lease, err
	}
	run, err := tx.Run(id)
	if err != nil || run.StartedAt != nil || run.State.Terminal() {
		return lease, err
	}
	run.StartedAt = new(c.Now())
	return lease, tx.UpdateRun(run)
}

// ports are the port reader and the dependency graph reader, none where
// Ports is unset.
func (c *Service) ports() (PortReader, DependencyGraphReader, error) {
	if c.Ports == nil {
		return nil, nil, nil
	}
	return c.Ports()
}

// Identity is what an environment's provider says it is now: empty where
// the provider can't say, and an error where it couldn't be read.
func (c *Service) Identity(ctx context.Context, environment model.Environment) (string, error) {
	provider, ok := c.Providers[environment.Provider].(buildenv.IdentityProvider)
	if !ok {
		return "", nil
	}
	return provider.Identity(ctx, environment)
}

// Identities asks each environment's provider what it is now: known with
// its value, none where the provider can't say, or unread with the cause,
// kept apart where they were one (#190). It is asked outside any
// transaction, since a transaction never calls a provider.
func (c *Service) Identities(ctx context.Context, environments []model.Environment) evidence.Identities {
	now := evidence.Identities{}
	for _, environment := range environments {
		identity, err := c.Identity(ctx, environment)
		switch {
		case err != nil:
			now[environment] = evidence.Identity{Unread: err.Error()}
		case identity != "":
			now[environment] = evidence.Identity{Value: identity}
		}
	}
	return now
}

// Remedy is how to give an environment what an unmet target needs, when
// its provider can say.
func (c *Service) Remedy(unmet model.Unmet) string {
	if remedier, ok := c.Providers[unmet.Environment.Provider].(buildenv.Remedier); ok {
		return remedier.Remedy(unmet)
	}
	return ""
}
