package checks

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// stopping is a provider whose work outlives its driver: Stop answers
// failing[ref] where it's set, else ended, leaving left; Execute names
// refer and builds until it's stopped.
type stopping struct {
	name string
	left []string

	mu      sync.Mutex
	failing map[string]error
	asked   []string
	refer   string
	started chan struct{}
}

func (s *stopping) Name() string { return s.name }

func (s *stopping) Execute(ctx context.Context, _ buildenv.Job, build buildenv.Build) error {
	if err := build.Refer(s.refer); err != nil {
		return err
	}
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func (s *stopping) Stop(_ context.Context, request buildenv.StopRequest) ([]string, error) {
	execution := request.Execution
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, execution.ProviderRef)
	if err := s.failing[execution.ProviderRef]; err != nil {
		return nil, err
	}
	return s.left, nil
}

func (s *stopping) fail(ref string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing == nil {
		s.failing = map[string]error{}
	}
	s.failing[ref] = err
}

func (s *stopping) took() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	asked := slices.Clone(s.asked)
	s.asked = nil
	return asked
}

// quiet is a provider whose work ends with its driver: no Stopper.
type quiet struct{}

func (quiet) Name() string                                                { return "quiet" }
func (quiet) Execute(context.Context, buildenv.Job, buildenv.Build) error { return nil }

// running records a run as running, as a driver that stopped left it,
// with an execution for each environment and reference.
func (c *Service) running(t *testing.T, run model.Run, attempts ...model.GuestExecution) {
	t.Helper()
	require.NoError(t, c.Store.Update(t.Context(), c.Repository, func(tx store.Tx) error {
		running, err := tx.Run(run.ID)
		if err != nil {
			return err
		}
		running.State, running.StartedAt = model.RunRunning, &at
		if err := tx.UpdateRun(running); err != nil {
			return err
		}
		attempt := map[model.Environment]int{}
		for _, execution := range attempts {
			attempt[execution.Environment]++
			execution.ID, execution.Run, execution.Attempt, execution.CreatedAt = model.ExecutionID(store.NewID("exec")), run.ID, attempt[execution.Environment], at
			if execution.State.Terminal() {
				execution.FinishedAt = &at
			}
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
		}
		return nil
	}))
}

// A cancel is finished only when every attempt answered that its work
// ended: each provider is asked once for each of its references, so two
// providers with one reference string are each asked and a matrix's
// cells that share one are asked once; a stop that fails doesn't keep
// the others from being asked, and leaves the check unfinished with every
// failure said; a second cancel asks again and finishes it, saying what
// ended work left (the lifecycle design's "Stopping", the one rule).
func TestACancelIsFinishedOnlyWhenEveryAttemptsWorkEnded(t *testing.T) {
	t.Parallel()
	c := service(t)
	alpha := &stopping{name: "alpha", left: []string{"the clone harbor-1, which dockhand clean removes"}}
	beta := &stopping{name: "beta"}
	c.Providers = map[string]buildenv.Provider{"alpha": alpha, "beta": beta, "quiet": quiet{}}
	on := func(provider, version string) model.Environment {
		return model.Environment{Provider: provider, Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}}
	}
	run := c.queued(t, "jq-cancel", on("alpha", "25"), on("alpha", "24"), on("beta", "25"), on("quiet", "25"))
	c.running(t, run,
		model.GuestExecution{Environment: on("alpha", "25"), State: model.ExecutionInfrastructure, ProviderRef: "shared"},
		model.GuestExecution{Environment: on("alpha", "24"), State: model.ExecutionRunning, ProviderRef: "shared"},
		model.GuestExecution{Environment: on("beta", "25"), State: model.ExecutionRunning, ProviderRef: "shared"},
		model.GuestExecution{Environment: on("beta", "25"), State: model.ExecutionInfrastructure, ProviderRef: "other"},
		model.GuestExecution{Environment: on("quiet", "25"), State: model.ExecutionRunning, ProviderRef: "quiet-1"},
	)
	beta.fail("shared", errors.New("GitHub answered 503"))
	beta.fail("other", errors.New("the run is still cancelling"))

	session := c.session(t)
	pending, err := c.RequestCancel(t.Context(), session, run.ID)
	require.ErrorContains(t, err, "check-1: a cancel was asked, and beta couldn't see shared stop: GitHub answered 503 and beta couldn't see other stop: the run is still cancelling; dockhand cancel check-1 asks again")
	require.Equal(t, model.RunRunning, pending.State)
	require.NotNil(t, pending.CancelRequested)
	require.Equal(t, []string{"shared"}, alpha.took(), "a matrix's cells that share a reference are asked once")
	require.Equal(t, []string{"shared", "other"}, beta.took(), "another provider's same string is its own, and a failure doesn't stop the asking")
	recorded, err := c.run(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, recorded.State)
	require.Equal(t, "a cancel was asked, and beta couldn't see shared stop: GitHub answered 503 and beta couldn't see other stop: the run is still cancelling", recorded.Detail, "status reads it")

	// Asked again: the stop that failed is asked again, and so is every
	// other, since a cancel is finished only when all answered.
	beta.fail("other", nil)
	_, err = c.RequestCancel(t.Context(), session, run.ID)
	require.ErrorContains(t, err, "beta couldn't see shared stop: GitHub answered 503; dockhand cancel check-1 asks again")
	require.Equal(t, []string{"shared"}, alpha.took())
	require.Equal(t, []string{"shared", "other"}, beta.took())

	beta.fail("shared", nil)
	canceled, err := c.RequestCancel(t.Context(), session, run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State)
	require.Equal(t, "canceled; finished results are kept; left: the clone harbor-1, which dockhand clean removes", canceled.Detail)
}

// A driver that takes a check whose cancel is recorded stops its work
// before it settles it, where it marked it canceled with its work running
// after a stop that failed: serve's loop, the worker and dockhand wait
// all come this way (the lifecycle design's "Stopping", the second path).
func TestADriverThatTakesACanceledCheckStopsItsWorkFirst(t *testing.T) {
	t.Parallel()
	c := service(t)
	alpha := &stopping{name: "alpha"}
	c.Providers = map[string]buildenv.Provider{"alpha": alpha}
	environment := model.Environment{Provider: "alpha", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	run := c.queued(t, "jq-taken", environment)
	c.running(t, run, model.GuestExecution{Environment: environment, State: model.ExecutionInfrastructure, ProviderRef: "run-7"})
	require.NoError(t, c.Store.Update(t.Context(), c.Repository, func(tx store.Tx) error {
		recorded, err := tx.Run(run.ID)
		if err != nil {
			return err
		}
		recorded.CancelRequested = &at
		return tx.UpdateRun(recorded)
	}))
	alpha.fail("run-7", errors.New("GitHub answered 503"))

	_, err := c.Drive(t.Context(), c.session(t), run.ID)
	require.ErrorContains(t, err, "alpha couldn't see run-7 stop: GitHub answered 503")
	require.Equal(t, []string{"run-7"}, alpha.took())
	recorded, err := c.run(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, recorded.State, "not canceled with its work running")

	// serve's loop leaves it until a provider's poll has passed.
	candidates, err := c.Candidates(t.Context(), c.session(t))
	require.NoError(t, err)
	require.Empty(t, candidates, "asked again no more often than a provider polls")

	alpha.fail("run-7", nil)
	canceled, err := c.Drive(t.Context(), c.session(t), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State)
}

// The live driver, once its providers return, keeps the same rule: its
// own cleanup's errors were dropped, and the check called canceled.
func TestTheLiveDriversCancelKeepsTheOneRule(t *testing.T) {
	t.Parallel()
	c := service(t)
	alpha := &stopping{name: "alpha", refer: "run-9", started: make(chan struct{})}
	c.Providers = map[string]buildenv.Provider{"alpha": alpha}
	environment := model.Environment{Provider: "alpha", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	run := c.queued(t, "jq-live", environment)
	alpha.fail("run-9", errors.New("the run is still cancelling"))

	type driven struct {
		run model.Run
		err error
	}
	done := make(chan driven, 1)
	go func() {
		run, err := c.Drive(context.Background(), c.session(t), run.ID)
		done <- driven{run, err}
	}()
	select {
	case <-alpha.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the provider was never given the job")
	}
	pending, err := c.RequestCancel(t.Context(), c.session(t), run.ID)
	require.NoError(t, err, "the driver holds it, and applies it")
	require.NotNil(t, pending.CancelRequested)
	var result driven
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the driver never ended after its cancel was asked for")
	}
	require.ErrorContains(t, result.err, "alpha couldn't see run-9 stop: the run is still cancelling")
	require.Equal(t, []string{"run-9"}, alpha.took())
	recorded, err := c.run(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, recorded.State)
	require.NotNil(t, recorded.CancelRequested)
}

// A provider this dockhand doesn't have, as after [providers.command] is
// removed while a script's driver is gone, is no evidence its work ended:
// not known, named. Only a provider present without a Stop is skipped
// (Codex's review of #193 steps 5 and 6, finding 2).
func TestAProviderThisDockhandHasntIsNotKnownToHaveStopped(t *testing.T) {
	t.Parallel()
	c := service(t)
	env := model.Environment{Provider: buildenv.Command, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	run := c.queued(t, "jq-missing-provider", env)
	c.running(t, run, model.GuestExecution{Environment: env, State: model.ExecutionRunning, Launch: `{"directory":"/a-checks-recorded-directory"}`})
	c.Providers = map[string]buildenv.Provider{}
	canceled, err := c.RequestCancel(t.Context(), c.session(t), run.ID)
	require.ErrorContains(t, err, "command isn't set up in this dockhand, so what attempt 1 launched there isn't known to have stopped")
	require.Equal(t, model.RunRunning, canceled.State)
}
