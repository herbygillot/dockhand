package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// unreadWords is what an identity read that failed says.
const unreadWords = "reading the image record: unexpected end of JSON input"

// unreadEnvironment is a provider whose environment's identity is never
// read: each read fails with err, unreadWords where it's nil.
type unreadEnvironment struct {
	buildenv.Provider
	err error
}

func (p unreadEnvironment) Identity(context.Context, model.Environment) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return "", errors.New(unreadWords)
}

// identityHolds are the identity-unread concerns among a plan's holds.
func identityHolds(concerns []model.Concern) []model.Concern {
	return slices.DeleteFunc(slices.Clone(concerns), func(c model.Concern) bool { return c.Rule != identityUnreadRule })
}

// A submission nobody attends is held, where it is planned (PlanSubmit),
// by an environment its evidence rests on whose identity can't be read
// now: the hold names the environment and the cause, the results stand as
// they were, and holds.allow can't let it go, even were it to name it,
// since what couldn't be checked holds (docs/designs/check-lifecycle.md at bbb4c0b7,
// "Identity"; #190). It lifts by itself once the identity reads again.
func TestPlanSubmitHoldsWhereAnIdentityCouldntBeRead(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	provider := &identified{identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	run := checkHead(t, e, branch)

	provider.unreadable = 1 << 20
	e.options.AllowHolds = []string{identityUnreadRule}
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking, "its results stand as they were, never remade")
	held := identityHolds(plan.Held())
	require.Len(t, held, 1, "%v", plan.Held())
	require.Equal(t, checks.DescribeEnvironment(tahoeArm)+": could not read what the environment is now, so whether "+run.Name()+"'s results there were built in this same environment isn't known: "+unreadWords, held[0].Detail)
	require.Equal(t, model.FromSubmission, held[0].Origin)
	require.NotContains(t, HoldRules(), identityUnreadRule, "no rule holds.allow or decide may name")

	provider.unreadable = 0
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, identityHolds(plan.Held()), "it lifts once the identity reads again")
	require.Empty(t, plan.Held())
}

// A read that was itself canceled is an unread identity, with its cause:
// never taken for none, nor for another environment, which would mark the
// results remade; a submission nobody attends is held by it.
func TestACanceledIdentityReadIsUnread(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": &identified{identity: "origin a"}}
	run := checkHead(t, e, branch)

	e.Providers["command"] = unreadEnvironment{Provider: e.Providers["command"], err: fmt.Errorf("tart get: %w", context.Canceled)}
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking, "its results stand as they were, never remade")
	require.Equal(t, map[model.Environment]string{tahoeArm: "tart get: context canceled"}, plan.Evidence.UnreadIdentities())
	held := identityHolds(plan.Held())
	require.Len(t, held, 1)
	require.Contains(t, held[0].Detail, run.Name()+"'s results there were built in this same environment isn't known: tart get: context canceled")
}

// The hold is for the environments the submission's evidence rests on and
// no others: an environment whose identity can't be read, which the plan
// asks for and where it builds nothing, every port being left out there,
// holds nothing.
func TestAnUnreadEnvironmentTheEvidenceDoesntRestOnHoldsNothing(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	sequoia := model.Environment{Provider: "other", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	e.Providers = map[string]buildenv.Provider{"command": &identified{identity: "origin a"}, "other": &identified{identity: "origin b"}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm, sequoia}})
	require.NoError(t, err)
	for i := range plan.Builds {
		if plan.Builds[i].Environment != sequoia {
			continue
		}
		for _, id := range plan.Builds[i].Order {
			target, _ := plan.Target(id)
			plan.Builds[i].Exclusions = append(plan.Builds[i].Exclusions, model.Exclusion{Target: target.Target, Reason: "its platforms exclude macOS 15"})
		}
		plan.Builds[i].Order = nil
	}
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)

	e.Providers["other"] = unreadEnvironment{Provider: e.Providers["other"]}
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.Contains(t, submission.Evidence.Plan.Environments, sequoia, "the plan asks for it")
	require.Empty(t, identityHolds(submission.Held()), "nothing the submission rests on was built there")

	e.Providers["command"] = unreadEnvironment{Provider: e.Providers["command"]}
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	held := identityHolds(submission.Held())
	require.Len(t, held, 1, "the environment it rests on holds, and only it")
	require.True(t, strings.HasPrefix(held[0].Detail, checks.DescribeEnvironment(tahoeArm)+": "), held[0].Detail)
}

// anotherCanReuse counts neither an environment that has no identity nor
// one whose identity couldn't be read: an environment with none says it
// builds every target only where another of the check has a known identity
// (TestAnEnvironmentThatCantReuseSaysSo), and an unread one is no such
// environment.
func TestAnUnreadIdentityIsNoEnvironmentThatCanReuse(t *testing.T) {
	t.Parallel()
	github := model.Environment{Provider: "github", Platform: tahoeArm.Platform}
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": unreadEnvironment{Provider: &scriptedProvider{}}, "github": &scriptedProvider{}}
	run, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm, github).ID)
	require.NoError(t, err)
	events, err := e.Events(t.Context(), 0, "")
	require.NoError(t, err)
	var said []string
	for _, event := range events {
		if event.Run == run.ID && strings.Contains(event.Message, "building every target") {
			said = append(said, event.Message)
		}
	}
	require.Empty(t, said, "an unread identity isn't counted as one another environment could reuse by")
	var executions []model.GuestExecution
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		executions, err = r.Executions(run.ID)
		return err
	}))
	failed := slices.ContainsFunc(executions, func(execution model.GuestExecution) bool {
		return execution.Environment == tahoeArm && execution.Detail == "could not identify the build environment: "+unreadWords
	})
	require.True(t, failed, "the unread environment's own attempt fails as the environment's, apart")
}

// serve's submitting pass holds a branch whose evidence rests on an
// environment it couldn't identify, saying so, and opens nothing for it.
// A publication serve already accepted, its push made and its opening
// owed, goes on by its record (#191) and is not judged again: the opening
// is made though the identity can't be read now. A record of another
// commit is no continuation: the submission is judged, and held.
func TestServeHoldsAnUnreadIdentityButNotAPublicationItAccepted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	servePrepared(t, e)
	readable := e.Providers["command"]
	var said []string
	pass := func() {
		said = nil
		s := newServer(e, ServeOptions{SubmitLimit: 5, Now: e.now, Say: func(line string) { said = append(said, line) }})
		(&passingSubmitter{s: s}).maybe(t.Context())
	}

	e.Providers["command"] = unreadEnvironment{Provider: readable}
	pass()
	require.Empty(t, fake.Created, "%q", said)
	require.True(t, slices.ContainsFunc(said, func(line string) bool {
		return strings.Contains(line, "needs review before submission: ") && strings.Contains(line, "could not read what the environment is now") && strings.Contains(line, unreadWords)
	}), "%q", said)

	// Read again, serve accepts it and records its publication; the
	// opening fails, and is owed.
	e.Providers["command"] = readable
	fake.CreateFails = true
	pass()
	require.Empty(t, fake.PRs, "%q", said)
	branches, err := e.OpenBranches(t.Context())
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.NotNil(t, branches[0].Publication, "%q", said)
	require.True(t, branches[0].Publication.Served)
	require.Contains(t, branches[0].Publication.Owes, model.StepOpen)

	// A record of another commit is no continuation of this one: this
	// submission is a new one, judged, and held where the identity can't
	// be read.
	accepted := *branches[0].Publication
	other := accepted
	other.Commit = model.ObjectID(strings.Repeat("b", 40))
	setPublication := func(next model.Publication) {
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			current, err := tx.Branch(branches[0].ID)
			if err != nil {
				return err
			}
			was := ""
			if current.Publication != nil {
				was = current.Publication.ID
			}
			return tx.SetPublication(branches[0].ID, was, &next)
		}))
	}
	setPublication(other)
	e.Providers["command"] = unreadEnvironment{Provider: readable}
	current, err := e.Branch(t.Context(), branches[0].ID)
	require.NoError(t, err)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current})
	require.NoError(t, err)
	require.Nil(t, plan.Carried, "another commit carries nothing")
	require.NotEmpty(t, identityHolds(plan.Held()), "a new submission is judged, and held: %v", plan.Held())
	setPublication(accepted)

	pass()
	require.Len(t, fake.PRs, 1, "the publication it accepted goes on: %q", said)
}
