package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// servePrepared has serve prepare and check jq's update, and returns the
// branch.
func servePrepared(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	return preparedBy(t, e, model.OriginServe)
}

// preparedBy is servePrepared for a branch of the origin given.
func preparedBy(t *testing.T, e *Engine, origin model.Origin) model.Branch {
	t.Helper()
	return preparedWith(t, e, origin, &scriptedProvider{}, port("jq"))
}

// preparedWith is preparedBy with the provider and jq's evaluation given.
func preparedWith(t *testing.T, e *Engine, origin model.Origin, provider *scriptedProvider, jq macports.PortInfo) model.Branch {
	t.Helper()
	e.OutdatedReader = &newReleases{}
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {jq}}})
	e.Providers = map[string]buildenv.Provider{"command": provider}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	answered := model.AnsweredAsked
	if origin == model.OriginServe {
		answered = model.AnsweredNobody
	}
	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: origin, Answered: answered, To: ShipCheck, Where: Where{Environments: []model.Environment{{Provider: "command"}}}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	require.Empty(t, prepared[0].Problem)
	run, err := e.Drive(t.Context(), session(t, e), prepared[0].Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	return prepared[0].Branch
}

func TestServeSubmitsOnlyWhatPassedWithNothingToLookAt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := servePrepared(t, e)
	committedUpdate(t, e) // a person's branch, which serve never submits

	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1, "only the branch serve prepared, and only once its check passed")
	require.Equal(t, branch.ID, candidates[0].Branch.ID)
	require.Empty(t, candidates[0].Held)

	submitted, err := e.SubmitForServe(t.Context(), candidates[0])
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.Contains(t, fake.Created[0].Desired.Body, ServeNote, "the pull request says no person reviewed it")
	require.Contains(t, fake.Created[0].Desired.Body, "- [ ] tested basic functionality of all binary files?", "serve states nothing only a person can")

	again, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, again, "a branch with a pull request is done")

	// serve.submit_limit counts from the journal, so it holds across
	// restarts, and a new day starts a new count.
	now := e.now()
	opened, err := e.servedToday(t.Context(), now)
	require.NoError(t, err)
	require.Equal(t, 1, opened)
	opened, err = e.servedToday(t.Context(), now.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Zero(t, opened)
}

func TestServeHoldsAnUpdateWhoseUpstreamChangedItsLicense(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	p.upstream = [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL-3\n"}}
	branch := servePrepared(t, e)

	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, HoldLines(status.Held))
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, HoldLines(candidates[0].Held))
	require.Len(t, candidates[0].Plan.Upstream, 1)
	require.Equal(t, "jq", candidates[0].Plan.Upstream[0].Port)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true, Rule: assess.LicenseChanged, Class: model.Introduced}},
		candidates[0].Plan.Upstream[0].Comparison.Changes, "the plan says what was found, for a person's submission to show")
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "needs review before submission: upstream's LICENSE changed")
}

// Archives the update couldn't compare hold serve's submission as a
// finding would (D4): nobody looks, and the comparison couldn't.
func TestServeHoldsAnUpdateWhoseArchivesCouldNotBeCompared(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.Others = nil
	p.upstream = [2]map[string]string{nil, {"LICENSE": "MIT\n"}}
	branch := servePrepared(t, e)

	held := []string{"the upstream archives couldn't be compared: new.tar.gz replaces no archive dockhand could find, so it wasn't compared"}
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, held, HoldLines(status.Held), "the attention list says why")
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, held, HoldLines(candidates[0].Held))
	require.Equal(t, []PortComparison{{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{},
		Problem: "new.tar.gz replaces no archive dockhand could find, so it wasn't compared"}}}, candidates[0].Plan.Upstream)
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "needs review before submission: the upstream archives couldn't be compared")
	require.Empty(t, fake.Created)
}

// A Go release upstream's go.mod requires that the Portfile's minimum
// doesn't holds serve's submission: the builder's Go passes the build, and
// raising or declaring the minimum is a person's call.
func TestServeHoldsAnUpdateWhoseGoToolchainNeedsALook(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.Others = nil
	p.toolchain = &editprep.GoToolchain{Required: "1.25", Outcome: editprep.GoToolchainUndeclared}
	p.upstream = [2]map[string]string{{"go.mod": "module m\n\ngo 1.24\n"}, {"go.mod": "module m\n\ngo 1.25\n"}}
	branch := servePrepared(t, e)

	held := []string{"upstream: go.mod requires Go 1.25, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call"}
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, held, HoldLines(status.Held))
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, held, HoldLines(candidates[0].Held))
	require.Empty(t, fake.Created)
}

// A minimum the update raised, or one that already gates on the series
// go.mod requires, holds nothing, and is said where the submission is
// previewed, not only as the update ran (the ov run's finding 1).
func TestServeSaysAGoToolchainMinimumItNeedNotHold(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		toolchain editprep.GoToolchain
		message   string
	}{
		{editprep.GoToolchain{Required: "1.26.8", Declared: "1.25.8", Outcome: editprep.GoToolchainRaised},
			"upstream: go.mod requires Go 1.26.8, so go.toolchain_min is raised from 1.25.8"},
		{editprep.GoToolchain{Required: "1.26.8", Declared: "1.26", Outcome: editprep.GoToolchainCovered},
			"upstream: go.mod requires Go 1.26.8, which go.toolchain_min 1.26 already gates on"},
	} {
		t.Run(string(test.toolchain.Outcome), func(t *testing.T) {
			f := setup(t)
			e, p := f.withPreparer(t)
			fake := f.withFork(t, e)
			fake.Others = nil
			p.toolchain = &test.toolchain
			p.upstream = [2]map[string]string{{"go.mod": "module m\n\ngo 1.25.8\n"}, {"go.mod": "module m\n\ngo 1.26.8\n"}}
			servePrepared(t, e)

			candidates, _, err := e.serveCandidates(t.Context())
			require.NoError(t, err)
			require.Empty(t, candidates[0].Held)
			require.Len(t, candidates[0].Plan.Upstream, 1)
			require.Equal(t, []model.UpstreamChange{
				{Kind: "toolchain", Path: "go.mod", Message: test.message, Rule: assess.GoToolchainRule, Subject: "1.26.8", Class: model.Introduced}}, candidates[0].Plan.Upstream[0].Comparison.Changes)
		})
	}
}

// Another open pull request for the port holds serve's, which would
// otherwise open a second one for the same update; so does not knowing,
// when the search fails. One already open holds it before the edit, as it
// holds bump's, so nothing is prepared or checked (Codex's review of
// 832f059e, high 1); one opened while it was checked holds it at
// submission.
func TestServeHoldsAnUpdateAnotherPullRequestIsOpenFor(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.1"}}
	e.OutdatedReader = &newReleases{}
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	early := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Answered: model.AnsweredNobody, To: ShipCheck, Where: Where{Environments: []model.Environment{{Provider: "command"}}}, Tests: model.TestsDeclared})
	require.Len(t, early, 1)
	require.Equal(t, "jq needs review before Dockhand can prepare its update: #34777 is open for the same port: jq: update to 1.8.1; no branch was started", early[0].Problem)
	require.Nil(t, early[0].Run, "nothing is checked")

	fake.Others = nil
	servePrepared(t, e)
	fake.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.1"}}
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"#34777 is open for the same port: jq: update to 1.8.1"}, HoldLines(candidates[0].Held))
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "needs review before submission: #34777 is open")
	require.Empty(t, fake.Created)

	held := SubmitPlan{SearchProblem: "rate limited"}.Held()
	require.Equal(t, []string{"couldn't look for other open pull requests: rate limited"}, HoldLines(held))
}

// An update no person looks over, bump's, looks for the port's other open
// pull requests once its release is found, and is held there on one, or
// on not knowing, before its edit is prepared: nothing is downloaded, and
// no branch is started. Finding none, it prepares the edit without asking
// again. An update a person asked for names them after its edit, as
// before, and a port already current asks nothing (the update-workflow
// review's efficiency item).
func TestAnUnattendedUpdateLooksForOthersBeforeItsEdit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	bump := func(name string) UpdateRequest {
		return UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq", LookForOthers: true, Unattended: true}
	}

	_, err := e.Update(t.Context(), bump("jq-bump"))
	var held *HeldBeforeEdit
	require.ErrorAs(t, err, &held)
	require.Equal(t, "jq", held.Port)
	require.Equal(t, []string{"#34777 is open for the same port: jq: update to 1.8.0"}, HoldLines(held.Held))
	require.Empty(t, p.requests, "nothing was prepared")
	fake.Others, fake.SearchErr = nil, errors.New("rate limited")
	_, err = e.Update(t.Context(), bump("jq-bump"))
	require.ErrorAs(t, err, &held)
	require.Equal(t, []string{"couldn't look for other open pull requests: rate limited"}, HoldLines(held.Held))
	require.Empty(t, p.requests)
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	fake.SearchErr, fake.Searches = nil, 0
	current := bump("jq-current")
	current.Version, current.Release = "1.7.1", &model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: true}, Version: "1.7.1"}
	update, err := e.Update(t.Context(), current)
	require.NoError(t, err)
	require.True(t, update.Current)
	require.Zero(t, fake.Searches, "a current port has nothing to hold")
	update, err = e.Update(t.Context(), bump("jq-bump"))
	require.NoError(t, err)
	require.True(t, update.Applied)
	require.Equal(t, 1, fake.Searches, "looked for once, before the edit")

	fake.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	person := bump("jq-update")
	person.Unattended = false
	update, err = e.Update(t.Context(), person)
	require.NoError(t, err, "a person's update stops for none")
	require.Equal(t, fake.Others, update.Others)
}

// ship --passing and serve read one definition of a passing branch:
// editing a passed branch takes it out of both.
func TestPassingIsOneDefinitionForShipAndServe(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := servePrepared(t, e)
	committedUpdate(t, e) // checked by nobody, so neither passing nor not

	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Len(t, passing.Ready, 1)
	require.Equal(t, branch.ID, passing.Ready[0].Branch.ID)
	require.Zero(t, passing.Others)

	portfile := filepath.Join(branch.Worktree, "textproc", "jq", "Portfile")
	data, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(data, "# edited after the check\n"...), 0o644))
	passing, err = e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready)
	require.Equal(t, 1, passing.Others, "its check no longer covers its files")
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates, "and serve submits it no more than ship --passing would")
}

// A Git-fetched port whose update chose a commit its tag no longer named
// when the check was planned is a concern: the update prepared one
// source, and the check built another. The update is matched to the
// planned target by its port and the source its release names, the newest
// such update being the one the files carry; one for another tag, another
// repository, or another port, or that found no commit, says nothing. A
// target --only left out is compared as a built one is.
func TestATagMovedBetweenAnUpdateAndItsCheckIsAConcern(t *testing.T) {
	t.Parallel()
	chosen, planned := strings.Repeat("a", 40), strings.Repeat("b", 40)
	source := model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: model.ObjectID(planned), ResolvedAt: time.Now()}
	evidence := &Evidence{Run: model.Run{Number: 7}, Plan: model.Plan{Targets: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}}},
		Builds: []model.EnvironmentPlan{{Order: []model.TargetID{"libharbor"}, Git: map[model.TargetID]model.GitSource{"libharbor": source}}}}}
	release := func(tag, commit string) *model.Release {
		return &model.Release{Version: "4", Forge: "github", Instance: "https://github.com", Repository: "harbor/libharbor", Tag: tag, Commit: commit}
	}
	update := func(port string, release *model.Release) model.Edit {
		return model.Edit{Kind: model.EditUpdate, Port: port, Release: release}
	}

	require.Empty(t, preparedSources(evidence, []model.Edit{update("libharbor", release("v4", planned))}), "the check built what the update chose")
	moved := preparedSources(evidence, []model.Edit{update("libharbor", release("v4", chosen))})
	require.Equal(t, []model.Concern{{Origin: model.FromUpstream, Port: "libharbor", Rule: "release-moved", Subject: planned, Class: model.Introduced,
		Detail: "libharbor: git.branch v4 pointed to aaaaaaa when the update was prepared, then to bbbbbbb when check-7 was planned. The check used a different source from the update"}}, moved)
	require.Contains(t, HoldLines(SubmitPlan{Moved: moved}.Held()), moved[0].Detail, "it holds a submission nobody looks over")

	require.Empty(t, preparedSources(evidence, []model.Edit{update("libharbor", release("v4", chosen)), update("libharbor", release("v4", planned))}),
		"the newest update chose what the check built")
	require.Empty(t, preparedSources(evidence, []model.Edit{update("libharbor", release("v3", chosen))}), "an update to another tag, edited since")
	fork := release("v4", chosen)
	fork.Repository = "fork/libharbor"
	require.Empty(t, preparedSources(evidence, []model.Edit{update("libharbor", fork)}), "another repository's tag")
	require.Empty(t, preparedSources(evidence, []model.Edit{update("harbor-cli", release("v4", chosen))}), "another port's update")
	require.Empty(t, preparedSources(evidence, []model.Edit{update("libharbor", release("v4", ""))}), "an update that found no commit")
	unresolved := evidence.Plan
	unresolved.Builds = []model.EnvironmentPlan{{Order: []model.TargetID{"libharbor"}, Git: map[model.TargetID]model.GitSource{
		"libharbor": {URL: source.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: source.ResolvedAt}}}}
	require.Empty(t, preparedSources(&Evidence{Run: evidence.Run, Plan: unresolved}, []model.Edit{update("libharbor", release("v4", chosen))}), "a check that couldn't resolve the tag")
	require.Empty(t, preparedSources(nil, []model.Edit{update("libharbor", release("v4", chosen))}), "no check")

	evidence.Plan.Omitted = []model.PlanTarget{{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}}}
	evidence.Plan.Builds[0].Git["harbor-cli"] = source
	moved = preparedSources(evidence, []model.Edit{update("harbor-cli", release("v4", chosen))})
	require.Len(t, moved, 1, "--only left it out, and its tag was resolved with the rest")
	require.Equal(t, "harbor-cli", moved[0].Port)
}

// taggedRelease finds jq's release at a tag of a repository on disk, as a
// forge would, and the commit the tag named when it looked.
type taggedRelease struct{ forge, repository, commit string }

func (r taggedRelease) Outdated(context.Context, model.ObjectID, OutdatedRequest) ([]OutdatedPort, error) {
	return []OutdatedPort{{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true,
		Release: &model.Release{Version: "1.8.1", Forge: "github", Instance: r.forge, Repository: r.repository, Tag: "jq-1.8.1", Commit: r.commit}}}, nil
}

// serve holds an update of a Git-fetched port whose tag moved after the
// update chose it and before its check was planned: the check built
// another source than the update chose, and nobody looked.
func TestServeHoldsAnUpdateWhoseTagMovedBeforeItsCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	forge := t.TempDir()
	project := filepath.Join(forge, "jqlang", "jq")
	require.NoError(t, os.MkdirAll(project, 0o755))
	testsupport.Git(t, project, "init", "-q")
	testsupport.Git(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1")
	testsupport.Git(t, project, "tag", "jq-1.8.1")
	chosen := testsupport.Git(t, project, "rev-parse", "HEAD")
	var moved string
	p.during = func() {
		testsupport.Git(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1, again")
		testsupport.Git(t, project, "tag", "-f", "jq-1.8.1")
		moved = testsupport.Git(t, project, "rev-parse", "HEAD")
	}
	jq := port("jq")
	jq.Options = map[string]string{"fetch.type": "git", "git.url": project, "git.branch": "jq-1.8.1"}
	e.OutdatedReader = taggedRelease{forge: forge, repository: "jqlang/jq", commit: chosen}
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {jq}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Answered: model.AnsweredNobody, To: ShipCheck, Where: Where{Environments: []model.Environment{{Provider: "command"}}}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	require.Empty(t, prepared[0].Problem)
	checked, err := e.Drive(t.Context(), session(t, e), prepared[0].Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, checked.State, checked.Detail)

	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, []string{"jq: git.branch jq-1.8.1 pointed to " + chosen[:7] + " when the update was prepared, then to " + moved[:7] + " when " + checked.Name() + " was planned. The check used a different source from the update"}, HoldLines(candidates[0].Held))

	// Status says it too, from the store and the plan alone. With the
	// repository gone, it reads nothing of it, and says nothing of where
	// the tag points now (source-moved), which takes the network.
	testsupport.Git(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1, a third time")
	testsupport.Git(t, project, "tag", "-f", "jq-1.8.1")
	require.NoError(t, os.RemoveAll(forge))
	status, err := e.BranchStatus(t.Context(), candidates[0].Branch)
	require.NoError(t, err)
	require.Len(t, status.Moved, 1)
	require.Equal(t, model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "release-moved", Subject: moved, Class: model.Introduced, Detail: candidates[0].Held[0].Detail}, status.Moved[0])

	// Files changed since the check aren't what it planned for: the
	// question waits for a check of them.
	write(t, candidates[0].Branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# edited\n"})
	status, err = e.BranchStatus(t.Context(), candidates[0].Branch)
	require.NoError(t, err)
	require.False(t, status.Current)
	require.Empty(t, status.Moved)
}

// A Git-fetched port whose assessment read another commit than its check
// planned is a concern, whether an update or a hand edit made it: what
// upstream's change was judged of isn't the source the check built
// (batch 22's leftover, batch 32). One that read the planned commit, or
// kept none, as one assessed before it was kept, says nothing.
func TestAnAssessmentOfAnotherCommitIsAConcern(t *testing.T) {
	t.Parallel()
	read, planned := strings.Repeat("a", 40), strings.Repeat("b", 40)
	source := model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: model.ObjectID(planned), ResolvedAt: time.Now()}
	evidence := &Evidence{Run: model.Run{Number: 7}, Plan: model.Plan{Targets: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}}},
		Builds: []model.EnvironmentPlan{{Order: []model.TargetID{"libharbor"}, Git: map[model.TargetID]model.GitSource{"libharbor": source}}}}}
	assessed := func(commit string) []PortComparison {
		return []PortComparison{{Port: "libharbor", Comparison: model.UpstreamComparison{Commit: commit}}}
	}
	moved := assessedSources(evidence, assessed(read))
	require.Len(t, moved, 1)
	require.Equal(t, "assessment-moved", moved[0].Rule)
	require.Equal(t, "libharbor: the upstream assessment examined aaaaaaa, but check-7 planned to build bbbbbbb. The assessment and check refer to different source commits", moved[0].Detail)
	require.Empty(t, assessedSources(evidence, assessed(planned)), "it read what the check built")
	require.Empty(t, assessedSources(evidence, assessed("")), "it kept no commit")
	require.Empty(t, assessedSources(evidence, nil), "no assessment")
	require.Empty(t, assessedSources(nil, assessed(read)), "no check")
}

// A passing branch a person started isn't serve's to submit, and is said
// as theirs: update --outdated --check's seven passing branches waited,
// unsaid (field testing's seventh report, 2026-10-02).
func TestServeLeavesAPersonsPassingBranchToThem(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := preparedBy(t, e, model.OriginPerson)
	candidates, yours, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates)
	require.Len(t, yours, 1)
	require.Equal(t, branch.ID, yours[0].ID)
}

// A hold two ports share is said once: py-kubernetes's two subports' held
// line read "…follow; upstream's setup.py changed; …follow" (field testing
// at f41fbad6). Concerns stay one a port, each with its key.
func TestAHoldSharedByPortsIsSaidOnce(t *testing.T) {
	changed := model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "build", Rule: "build-file", Path: "setup.py", Hold: true, Message: "upstream's setup.py changed; the build may need the Portfile to follow"}}}
	plan := SubmitPlan{Upstream: []PortComparison{{Port: "py312-kubernetes", Comparison: changed}, {Port: "py313-kubernetes", Comparison: changed}}}
	held := plan.Held()
	require.Len(t, held, 2, "a concern a port")
	require.Equal(t, []string{"upstream's setup.py changed; the build may need the Portfile to follow"}, HoldLines(held))
}

// An update nobody looks over whose major version moves is held before
// its edit, so nothing is prepared, unless holds.majors allows it:
// hyperfine went from 1.21.0 to 2.0.0 and bump opened #35353 unlooked at
// (field testing at b8baa26b). A submission's concerns carry it too, from
// what the branch's update recorded, for serve's.
func TestAMajorVersionHoldsAnUnattendedUpdate(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	major := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "2.0.0", CurrentVersion: "1.7.1"}, Version: "2.0.0", Forge: "github", Tag: "jq-2.0.0"}
	_, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "jq-bump"}, Action: model.EditUpdate, Port: "jq", Version: "2.0.0", Release: &major, Unattended: true})
	var held *HeldBeforeEdit
	require.ErrorAs(t, err, &held)
	require.Equal(t, []string{"jq moves from 1.7.1 to 2.0.0, a major version update. holds.majors requires review before an unattended update"}, HoldLines(held.Held))
	require.Equal(t, model.FromHolds, held.Held[0].Origin)
	require.Empty(t, p.requests, "nothing was prepared")
	require.True(t, e.HoldsMajor("hyperfine", "1.7.1", "2.0.0"))
	require.False(t, e.HoldsMajor("hyperfine", "1.7.1", "1.8.1"))

	require.Equal(t, []string{"jq moves from 1.7.1 to 2.0.0, a major version update. holds.majors requires review before an unattended update"},
		HoldLines(e.majorHolds([]model.Edit{{Kind: model.EditUpdate, Port: "jq", Release: &major}})))

	// Who answered is the caller's to say, not read from the origin: a
	// preparation nobody looks over holds, whoever started it (the
	// architecture review of 5897a82, R3-36).
	planned := PlannedUpdate{Port: OutdatedPort{Port: "jq", Current: "1.7.1", Newest: "2.0.0", Release: &major}, Name: "jq-2.0.0"}
	done := e.prepareOne(t.Context(), planned, PrepareOptions{Origin: model.OriginPerson, Answered: model.AnsweredNobody})
	require.Contains(t, done.Problem, "jq needs review before Dockhand can prepare its update")

	e.options.AllowMajors = true
	require.Nil(t, e.majorHold("jq", "1.7.1", "2.0.0"), "holds.majors = \"allow\"")
}

// A commit-rule error holds a submission nobody looks over, as tidy
// rewrites the commit it's in; a warning is said and holds nothing, as
// tidy keeps the commit: serve held a branch over a body line tidy called
// nothing to tidy (the direction synthesis's first contradiction).
func TestOnlyACommitRuleErrorHolds(t *testing.T) {
	t.Parallel()
	wrap := commitrules.Finding{Code: "body-wrap", Severity: commitrules.Warning, Commit: "abc1234", Message: "body has lines over 72 characters"}
	merge := commitrules.Finding{Code: "merge", Severity: commitrules.Error, Commit: "def5678", Message: "is a merge; rebase instead"}
	require.Empty(t, SubmitPlan{Findings: []commitrules.Finding{wrap}}.Held())
	held := SubmitPlan{Findings: []commitrules.Finding{wrap, merge}}.Held()
	require.Len(t, held, 1)
	require.Equal(t, model.FromCommitRules, held[0].Origin)
	require.Equal(t, "merge", held[0].Rule)
}

// The configuration's hold list sets a rule apart: its concerns are said,
// and hold nothing; what blocks a submission holds whatever it says, as do
// archives that couldn't be compared (D4).
func TestTheHoldListSetsARuleApart(t *testing.T) {
	t.Parallel()
	license := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: assess.LicenseChanged, Detail: "upstream's LICENSE changed"}
	plan := SubmitPlan{Blocking: []string{"jq did not pass in check-1"}, Others: []forge.PullRequestSummary{{Number: 35001, Title: "jq: update to 1.8.2"}}}
	plan.Upstream = []PortComparison{{Port: "jq", Comparison: model.UpstreamComparison{Problem: "the archive couldn't be fetched",
		Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: license.Detail, Hold: true, Rule: license.Rule, Class: model.Introduced}}}}}
	plan.allowed = func(string) []string { return []string{"open", assess.LicenseChanged, "not-compared", "blocking"} }
	var rules []string
	for _, concern := range plan.Held() {
		rules = append(rules, concern.Rule)
	}
	require.Equal(t, []string{"blocking", "not-compared"}, rules)
	require.Len(t, plan.Concerns(), 3, "each is still said")
	require.NotContains(t, HoldRules(), "blocking")
	require.NotContains(t, HoldRules(), "not-compared")
	require.Subset(t, HoldRules(), []string{"open", "major-version", assess.LicenseChanged, "subject-port"})
}

// A port's own holds go over the global ones for it alone: its majors and
// its allow list, where it gives them (Then's item 2).
func TestAPortsOwnHoldsGoOverTheGlobalOnes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	e.options.AllowHolds = []string{assess.LicenseChanged}
	e.options.PortHolds = map[string]PortHolds{
		"jq":  {AllowMajors: new(true)},
		"fzf": {AllowMajors: new(false), Allow: []string{"open"}},
	}
	require.Nil(t, e.majorHold("jq", "1.7.1", "2.0.0"), "jq's majors = \"allow\"")
	require.NotNil(t, e.majorHold("hyperfine", "1.21.0", "2.0.0"), "held by the global majors")
	e.options.AllowMajors = true
	require.NotNil(t, e.majorHold("fzf", "0.9.0", "1.0.0"), "fzf's majors = \"hold\" over the global allow")
	require.Nil(t, e.majorHold("hyperfine", "1.21.0", "2.0.0"))

	open := func(port string) model.Concern {
		return model.Concern{Origin: model.FromOtherPullRequests, Port: port, Rule: "open", Detail: port + " has another open pull request"}
	}
	license := model.Concern{Origin: model.FromUpstream, Port: "fzf", Rule: assess.LicenseChanged, Detail: "upstream's LICENSE changed"}
	require.Equal(t, []model.Concern{open("jq"), license}, holding([]model.Concern{open("fzf"), open("jq"), license}, e.allowedFor),
		"fzf's allow replaces the global list for it")
}

// A port whose holds name another to submit first holds its unattended
// submission until that one has a pull request open at the same version:
// harfbuzz after harfbuzz-devel (Then's item 2).
func TestAPortWaitsForThePortItsHoldsNameFirst(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	e.options.PortHolds = map[string]PortHolds{"harfbuzz": {After: []string{"harfbuzz-devel"}}}
	release := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "11.2.0", CurrentVersion: "11.1.0"}, Version: "11.2.0"}
	edits := []model.Edit{{Kind: model.EditUpdate, Port: "harfbuzz", Release: &release}}
	held := e.afterHolds(t.Context(), edits)
	require.Equal(t, []string{`harfbuzz waits for harfbuzz-devel's update to 11.2.0, as its holds say (holds.ports."harfbuzz".after): with no pull request open at 11.2.0`}, HoldLines(held))
	require.Len(t, SubmitPlan{After: held}.Held(), 1, "it holds a submission nobody looks over")
	require.Contains(t, HoldRules(), "after", "holds.allow may name it")

	fake.Others = []forge.PullRequestSummary{{Number: 35100, Title: "harfbuzz-devel: update to 11.2.0"}}
	require.Empty(t, e.afterHolds(t.Context(), edits), "it goes once harfbuzz-devel's pull request is open")
	require.Empty(t, e.afterHolds(t.Context(), []model.Edit{{Kind: model.EditUpdate, Port: "harfbuzz-devel", Release: &release}}), "only the port whose holds say so waits")
}

// A build that links a library from a port its Portfile doesn't declare
// holds a submission nobody looks over, naming the port, unless
// holds.allow names link-undeclared; a declared link holds nothing (the
// person's "Hold it", 2026-10-27 12:53Z: rustup linked liblzma with no
// depends_lib on xz).
func TestAnUndeclaredLinkHoldsAnUnattendedSubmission(t *testing.T) {
	t.Parallel()
	lzma := map[model.TargetID][]model.Link{"jq": {{Binary: "/opt/local/bin/jq", Library: "/opt/local/lib/liblzma.5.dylib", Port: "xz"}}}
	held := []string{"jq links liblzma.5.dylib from xz, which it doesn't declare in depends_lib; review is required before automatic submission"}

	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	preparedWith(t, e, model.OriginServe, &scriptedProvider{active: []model.ActivePort{}, readsLinks: true, links: lzma}, port("jq"))
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, held, HoldLines(candidates[0].Held))
	require.Equal(t, "xz", candidates[0].Held[0].Subject)
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "needs review before submission: jq links liblzma.5.dylib from xz")
	require.Empty(t, fake.Created)
	require.Contains(t, HoldRules(), "link-undeclared")

	e.options.AllowHolds = []string{"link-undeclared"}
	candidates, _, err = e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates[0].Held, "holds.allow lets it go")
	require.Equal(t, held, HoldLines(candidates[0].Plan.Concerns()), "and it's still said")

	declared := setup(t)
	d, _ := declared.withPreparer(t)
	declared.withFork(t, d)
	preparedWith(t, d, model.OriginServe, &scriptedProvider{active: []model.ActivePort{}, readsLinks: true, links: lzma}, port("jq", "xz"))
	candidates, _, err = d.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates[0].Held, "a declared link holds nothing")
}

// A person's decision on one concern, with their reason: acceptable lets
// it go for serve, still said, and the pull request gives the reason
// under Reviewed; awaiting blocks the submission; a change to the port's
// own files lapses it (the person's voice, Then's item 13).
func TestADecisionOnAConcernCarriesThePersonsReason(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	p.upstream = [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL-3\n"}}
	branch := servePrepared(t, e)

	_, err := e.Decide(t.Context(), DecideRequest{Branch: branch, Rule: "no-such-rule", Answer: model.Accepted, Words: "fine"})
	require.ErrorContains(t, err, "is not a hold dockhand raises")
	decision, err := e.Decide(t.Context(), DecideRequest{Branch: branch, Rule: assess.LicenseChanged, Answer: model.Waiting, Words: "upstream's word on the COPYING change"})
	require.NoError(t, err)
	require.Equal(t, "jq license-changed", decision.Subject)
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Contains(t, strings.Join(candidates[0].Plan.Blocking, "\n"), "jq license-changed: you're awaiting upstream's word on the COPYING change")
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Len(t, status.Awaiting, 1)
	rows := branchstate.Attend(status.Facts())
	require.Equal(t, branchstate.Awaiting, rows[0].Reason)
	require.Equal(t, branchstate.Next{Verb: branchstate.VerbDecide, Branch: "jq-1.8.1", Args: []string{"license-changed", "jq", "--because", `"…"`}}, rows[0].Next)

	_, err = e.Decide(t.Context(), DecideRequest{Branch: branch, Rule: assess.LicenseChanged, Answer: model.Accepted, Words: "the new license is still free, and the Portfile says GPL-3"})
	require.NoError(t, err)
	candidates, _, err = e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates[0].Held, "a person looked, and said why")
	require.Empty(t, candidates[0].Plan.Blocking)
	require.Contains(t, candidates[0].Plan.Body, "> **Reviewed:**\n> - jq license-changed: acceptable because the new license is still free, and the Portfile says GPL-3\n")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.Held)
	require.Equal(t, []string{"jq license-changed: acceptable because the new license is still free, and the Portfile says GPL-3"}, []string{DecisionWords(status.Decided[0])})

	// A change to the port's own files lapses it.
	write(t, branch.Worktree, map[string]string{"textproc/jq/files/patch-x.diff": "--- a\n+++ b\n"})
	testsupport.Git(t, branch.Worktree, "add", "-A")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "jq: add a patch")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.Decided, "its evidence changed")
}
