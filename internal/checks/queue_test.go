package checks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check in a prefix on this Mac runs its Portfiles as root there, so
// it's refused for a pull request someone else wrote, which the person
// adopted, and for serve, unless [providers.local] serve says it may.
func TestALocalCheckIsRefusedWhereItsPortfilesArentTheirs(t *testing.T) {
	t.Parallel()
	local := model.Plan{Environments: []model.Environment{{Provider: buildenv.Local, Prefix: "/opt/local"}}}
	adopted := model.Branch{Name: "dockhand/jq-4f2a", PullRequest: &model.PullRequest{Number: 35968, Adopted: true}}
	c := &Service{}
	require.ErrorContains(t, c.localAllowed(QueueRequest{Branch: adopted, Plan: local}), "is a pull request you adopted, whose Portfiles someone else wrote. A local check would run them as root on this Mac; check it with --on tart or --on github instead")
	require.NoError(t, c.localAllowed(QueueRequest{Branch: adopted, Plan: model.Plan{Environments: []model.Environment{{Provider: buildenv.Tart}}}}))
	require.ErrorContains(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}), "unless [providers.local] serve = true")
	c.LocalServe = true
	require.NoError(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}))
	require.NoError(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginPerson}))
}

// A plan resolving a target's Git source, or its tag, to another commit
// isn't covered by one that resolved the old: a moved tag over the same
// Portfile is another source (#114; Codex's review of b2e7b616, its
// probe).
func TestPlanCoverageRequiresTheSameResolvedSources(t *testing.T) {
	t.Parallel()
	for _, tagged := range []bool{false, true} {
		environment := model.Environment{Provider: "command"}
		have := model.Plan{Environments: []model.Environment{environment}, Builds: []model.EnvironmentPlan{{Environment: environment, Order: []model.TargetID{"libharbor"}}}, Targets: []model.PlanTarget{{ID: "libharbor"}}}
		want := have
		want.Builds = []model.EnvironmentPlan{{Environment: environment, Order: []model.TargetID{"libharbor"}}}
		old := map[model.TargetID]model.GitSource{"libharbor": {URL: "https://example.invalid/repo", Ref: "v4", Commit: "old"}}
		newer := map[model.TargetID]model.GitSource{"libharbor": {URL: "https://example.invalid/repo", Ref: "v4", Commit: "new"}}
		if tagged {
			have.Builds[0].Tags, want.Builds[0].Tags = old, newer
		} else {
			have.Builds[0].Git, want.Builds[0].Git = old, newer
		}
		require.False(t, PlanCovers(have, want), "tagged %v", tagged)
		require.True(t, PlanCovers(have, have), "tagged %v", tagged)
	}
}

// Two plans that both failed to read a target's Git source join one
// running check, which shares a request and claims nothing of the source;
// a finished check like that stands for no other request, since what
// isn't known of a Git fetch stands for nothing. A tag's unresolved
// readings stand either way, the Portfile's checksums saying what was
// built (#192).
func TestAnUnresolvedGitSourceJoinsAndDoesntStand(t *testing.T) {
	t.Parallel()
	environment := model.Environment{Provider: "command"}
	unresolved := map[model.TargetID]model.GitSource{"libharbor": {URL: "https://example.invalid/repo", Ref: "v4", Unresolved: "the repository couldn't be read"}}
	plan := func(git, tags map[model.TargetID]model.GitSource) model.Plan {
		return model.Plan{Environments: []model.Environment{environment}, Targets: []model.PlanTarget{{ID: "libharbor"}},
			Builds: []model.EnvironmentPlan{{Environment: environment, Order: []model.TargetID{"libharbor"}, Git: git, Tags: tags}}}
	}
	git, tag := plan(unresolved, nil), plan(nil, unresolved)
	require.True(t, SourcesCover(git, git), "two unresolved Git readings join")
	require.False(t, FinishedSourcesCover(git, git), "a finished one stands for no other request")
	require.True(t, SourcesCover(tag, tag))
	require.True(t, FinishedSourcesCover(tag, tag), "a tag's unresolved readings stand")
	// A fetch mode changed between Git and an archive by tag is other.
	require.False(t, SourcesCover(git, tag))
	require.False(t, FinishedSourcesCover(git, tag))
}

// Environments requested only on one side do not show a changed source;
// a moved source in a shared environment does (#259).
func TestSharedEnvironmentSourcesDistinguishOtherProvidersFromMovedSources(t *testing.T) {
	t.Parallel()
	provider := func(name string) model.Environment { return model.Environment{Provider: name} }
	base, otherProvider := provider("github"), provider("tart")
	otherMac := base
	otherMac.Platform = model.Platform{OS: "darwin", Version: "15", Architecture: "arm64"}
	plan := func(envs []model.Environment, source string) model.Plan {
		builds := make([]model.EnvironmentPlan, 0, len(envs))
		for _, env := range envs {
			builds = append(builds, model.EnvironmentPlan{Environment: env, Order: []model.TargetID{"libharbor"}, Tags: map[model.TargetID]model.GitSource{"libharbor": {URL: "https://example.invalid/repo", Ref: "v4", Commit: model.ObjectID(source)}}})
		}
		return model.Plan{Environments: envs, Builds: builds, Targets: []model.PlanTarget{{ID: "libharbor"}}}
	}
	for _, tc := range []struct {
		name       string
		have, want model.Plan
		moved      bool
	}{
		{"different provider", plan([]model.Environment{base}, "old"), plan([]model.Environment{otherProvider}, "new"), false},
		{"another macOS", plan([]model.Environment{base}, "old"), plan([]model.Environment{otherMac}, "new"), false},
		{"moved in shared environment", plan([]model.Environment{base}, "old"), plan([]model.Environment{base}, "new"), true},
		{"moved and different provider", plan([]model.Environment{base}, "old"), plan([]model.Environment{base, otherProvider}, "new"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.moved, SharedSourcesDiffer(tc.have, tc.want))
		})
	}
}

// A request for another environment beside a running check is uncovered,
// but the environment difference does not say its source moved (#259).
func TestBesideAnotherEnvironmentDoesNotReportAnotherSource(t *testing.T) {
	t.Parallel()
	c := service(t)
	runningEnvironment := model.Environment{Provider: "github", Platform: model.Platform{OS: "linux", Version: "24.04", Architecture: "arm64"}}
	requestedEnvironment := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "15", Architecture: "arm64"}}
	run := c.queued(t, "another-environment", runningEnvironment)
	runner := c.session(t)
	_, err := runner.Acquire(t.Context(), RunResource(run.ID))
	require.NoError(t, err)
	require.NoError(t, c.Store.Update(t.Context(), c.Repository, func(tx store.Tx) error {
		run.State, run.StartedAt = model.RunRunning, &at
		return tx.UpdateRun(run)
	}))
	var branch model.Branch
	var revision model.Revision
	var plan model.Plan
	require.NoError(t, c.Store.View(t.Context(), c.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(run.Branch)
		if err != nil {
			return err
		}
		revision, err = r.Revision(run.Revision)
		if err != nil {
			return err
		}
		plan, err = r.Plan(run.Plan)
		return err
	}))
	plan.ID, plan.Environments = "requested-plan", []model.Environment{requestedEnvironment}
	plan.Builds = []model.EnvironmentPlan{{Environment: requestedEnvironment, Order: []model.TargetID{"jq"}}}
	_, err = c.beside(t.Context(), QueueRequest{Branch: branch, Revision: revision, Plan: plan, Beside: BesideFollow})
	var active *ActiveRunError
	require.ErrorAs(t, err, &active)
	require.True(t, active.Uncovered)
	require.False(t, active.OtherSource)
}

// Matching target sets do not cover a request when its dependent is
// planned to consume another build of the prerequisite (#236).
func TestPlanCoverageRequiresTheSameConsumedBuilds(t *testing.T) {
	t.Parallel()
	environment := model.Environment{Provider: "command"}
	targets := []model.PlanTarget{
		{ID: "a", Target: model.Target{Name: "a"}},
		{ID: "b", Target: model.Target{Name: "b"}},
		{ID: "b +docs", Target: model.Target{Name: "b", Variants: map[string]bool{"docs": true}}},
	}
	plan := func(dependency model.TargetID) model.Plan {
		return model.Plan{Targets: targets, Environments: []model.Environment{environment}, Builds: []model.EnvironmentPlan{{
			Environment: environment, Order: []model.TargetID{"b", "b +docs", "a"}, Dependencies: map[model.TargetID][]model.TargetID{"a": {dependency}},
		}}}
	}
	require.True(t, PlanCovers(plan("b +docs"), plan("b +docs")))
	require.False(t, PlanCovers(plan("b"), plan("b +docs")))
	require.False(t, PlanCovers(plan("b +docs"), plan("b")))
}
