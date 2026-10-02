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

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
)

// servePrepared has serve prepare and check jq's update, and returns the
// branch.
func servePrepared(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	e.OutdatedReader = &newReleases{}
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Check: true, Environments: []model.Environment{{Provider: "command"}}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	require.Empty(t, prepared[0].Problem)
	run, err := e.Drive(t.Context(), session(t, e), prepared[0].Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	return prepared[0].Branch
}

func TestServeSubmitsOnlyWhatPassedWithNothingToLookAt(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := servePrepared(t, e)
	committedUpdate(t, e) // a person's branch, which serve never submits

	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1, "only the branch serve prepared, and only once its check passed")
	require.Equal(t, branch.ID, candidates[0].Branch.ID)
	require.Empty(t, candidates[0].Held)

	submitted, err := e.SubmitForServe(t.Context(), candidates[0])
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.Contains(t, fake.created[0].Desired.Body, ServeNote, "the pull request says no person reviewed it")
	require.Contains(t, fake.created[0].Desired.Body, "- [ ] tested basic functionality of all binary files?", "serve states nothing only a person can")

	again, err := e.ServeCandidates(t.Context())
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
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	p.upstream = [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL-3\n"}}
	branch := servePrepared(t, e)

	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, status.Held)
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, candidates[0].Held)
	require.Len(t, candidates[0].Plan.Upstream, 1)
	require.Equal(t, "jq", candidates[0].Plan.Upstream[0].Port)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true, Rule: assess.LicenseChanged, Class: model.Introduced}},
		candidates[0].Plan.Upstream[0].Comparison.Changes, "the plan says what was found, for a person's submission to show")
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "is held for a look: upstream's LICENSE changed")
}

// Archives the update couldn't compare hold serve's submission as a
// finding would (D4): nobody looks, and the comparison couldn't.
func TestServeHoldsAnUpdateWhoseArchivesCouldNotBeCompared(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.others = nil
	p.upstream = [2]map[string]string{nil, {"LICENSE": "MIT\n"}}
	branch := servePrepared(t, e)

	held := []string{"the upstream archives couldn't be compared: new.tar.gz replaces no archive dockhand could find, so it wasn't compared"}
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, held, status.Held, "the attention list says why")
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, held, candidates[0].Held)
	require.Equal(t, []PortComparison{{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{},
		Problem: "new.tar.gz replaces no archive dockhand could find, so it wasn't compared"}}}, candidates[0].Plan.Upstream)
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "is held for a look: the upstream archives couldn't be compared")
	require.Empty(t, fake.created)
}

// A Go release upstream's go.mod requires that the Portfile's minimum
// doesn't holds serve's submission: the builder's Go passes the build, and
// raising or declaring the minimum is a person's call.
func TestServeHoldsAnUpdateWhoseGoToolchainNeedsALook(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.others = nil
	p.toolchain = &preparation.GoToolchain{Required: "1.25", Outcome: preparation.GoToolchainUndeclared}
	p.upstream = [2]map[string]string{{"go.mod": "module m\n\ngo 1.24\n"}, {"go.mod": "module m\n\ngo 1.25\n"}}
	branch := servePrepared(t, e)

	held := []string{"upstream: go.mod requires Go 1.25, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call"}
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, held, status.Held)
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, held, candidates[0].Held)
	require.Empty(t, fake.created)
}

// A minimum the update raised, or one that already gates on the series
// go.mod requires, holds nothing, and is said where the submission is
// previewed, not only as the update ran (the ov run's finding 1).
func TestServeSaysAGoToolchainMinimumItNeedNotHold(t *testing.T) {
	for _, test := range []struct {
		toolchain preparation.GoToolchain
		message   string
	}{
		{preparation.GoToolchain{Required: "1.26.8", Declared: "1.25.8", Outcome: preparation.GoToolchainRaised},
			"upstream: go.mod requires Go 1.26.8, so go.toolchain_min is raised from 1.25.8"},
		{preparation.GoToolchain{Required: "1.26.8", Declared: "1.26", Outcome: preparation.GoToolchainCovered},
			"upstream: go.mod requires Go 1.26.8, which go.toolchain_min 1.26 already gates on"},
	} {
		t.Run(string(test.toolchain.Outcome), func(t *testing.T) {
			f := setup(t)
			e, p := f.withPreparer(t)
			fake := f.withFork(t, e)
			fake.others = nil
			p.toolchain = &test.toolchain
			p.upstream = [2]map[string]string{{"go.mod": "module m\n\ngo 1.25.8\n"}, {"go.mod": "module m\n\ngo 1.26.8\n"}}
			servePrepared(t, e)

			candidates, err := e.ServeCandidates(t.Context())
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
// when the search fails.
func TestServeHoldsAnUpdateAnotherPullRequestIsOpenFor(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.1"}}
	servePrepared(t, e)

	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"#34777 is open for the same port: jq: update to 1.8.1"}, candidates[0].Held)
	_, err = e.SubmitForServe(t.Context(), candidates[0])
	require.ErrorContains(t, err, "is held for a look: #34777 is open")
	require.Empty(t, fake.created)

	held := SubmitPlan{SearchProblem: "rate limited"}.held()
	require.Equal(t, []string{"couldn't look for other open pull requests: rate limited"}, held)
}

// An update no person looks over, bump's, looks for the port's other open
// pull requests once its release is found, and is held there on one, or
// on not knowing, before its edit is prepared: nothing is downloaded, and
// no branch is started. Finding none, it prepares the edit without asking
// again. An update a person asked for names them after its edit, as
// before, and a port already current asks nothing (the update-workflow
// review's efficiency item).
func TestAnUnattendedUpdateLooksForOthersBeforeItsEdit(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	bump := func(name string) UpdateRequest {
		return UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq", LookForOthers: true, Unattended: true}
	}

	_, err := e.Update(t.Context(), bump("jq-bump"))
	var held *HeldBeforeEdit
	require.ErrorAs(t, err, &held)
	require.Equal(t, &HeldBeforeEdit{Port: "jq", Held: []string{"#34777 is open for the same port: jq: update to 1.8.0"}}, held)
	require.Empty(t, p.requests, "nothing was prepared")
	fake.others, fake.searchErr = nil, errors.New("rate limited")
	_, err = e.Update(t.Context(), bump("jq-bump"))
	require.ErrorAs(t, err, &held)
	require.Equal(t, []string{"couldn't look for other open pull requests: rate limited"}, held.Held)
	require.Empty(t, p.requests)
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	fake.searchErr, fake.searches = nil, 0
	current := bump("jq-current")
	current.Version, current.Release = "1.7.1", &model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: true}, Version: "1.7.1"}
	update, err := e.Update(t.Context(), current)
	require.NoError(t, err)
	require.True(t, update.Current)
	require.Zero(t, fake.searches, "a current port has nothing to hold")
	update, err = e.Update(t.Context(), bump("jq-bump"))
	require.NoError(t, err)
	require.True(t, update.Applied)
	require.Equal(t, 1, fake.searches, "looked for once, before the edit")

	fake.others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	person := bump("jq-update")
	person.Unattended = false
	update, err = e.Update(t.Context(), person)
	require.NoError(t, err, "a person's update stops for none")
	require.Equal(t, fake.others, update.Others)
}

// submit --passing and serve read one definition of a passing branch:
// editing a passed branch takes it out of both.
func TestPassingIsOneDefinitionForSubmitAndServe(t *testing.T) {
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
	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates, "and serve submits it no more than submit --passing would")
}

// A Git-fetched port whose update chose a commit its tag no longer named
// when the check was planned is a concern: the update prepared one
// source, and the check built another. The update is matched to the
// planned target by its port and the source its release names, the newest
// such update being the one the files carry; one for another tag, another
// repository, or another port, or that found no commit, says nothing. A
// target --only left out is compared as a built one is.
func TestATagMovedBetweenAnUpdateAndItsCheckIsAConcern(t *testing.T) {
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
		Detail: "libharbor's git.branch v4 named aaaaaaa when its update chose it, and bbbbbbb when check-7 planned it: the check built another source than the update chose"}}, moved)
	require.Contains(t, SubmitPlan{Moved: moved}.held(), moved[0].Detail, "it holds a submission nobody looks over")

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
	f := setup(t)
	e, p := f.withPreparer(t)
	f.withFork(t, e)
	forge := t.TempDir()
	project := filepath.Join(forge, "jqlang", "jq")
	require.NoError(t, os.MkdirAll(project, 0o755))
	run(t, project, "init", "-q")
	run(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1")
	run(t, project, "tag", "jq-1.8.1")
	chosen := run(t, project, "rev-parse", "HEAD")
	var moved string
	p.during = func() {
		run(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1, again")
		run(t, project, "tag", "-f", "jq-1.8.1")
		moved = run(t, project, "rev-parse", "HEAD")
	}
	jq := port("jq")
	jq.Options = map[string]string{"fetch.type": "git", "git.url": project, "git.branch": "jq-1.8.1"}
	e.OutdatedReader = taggedRelease{forge: forge, repository: "jqlang/jq", commit: chosen}
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {jq}}}
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	prepared := e.PrepareOutdated(t.Context(), plan, PrepareOptions{Origin: model.OriginServe, Check: true, Environments: []model.Environment{{Provider: "command"}}, Tests: model.TestsDeclared})
	require.Len(t, prepared, 1)
	require.Empty(t, prepared[0].Problem)
	checked, err := e.Drive(t.Context(), session(t, e), prepared[0].Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, checked.State, checked.Detail)

	candidates, err := e.ServeCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, []string{"jq's git.branch jq-1.8.1 named " + chosen[:7] + " when its update chose it, and " + moved[:7] + " when " + checked.Name() + " planned it: the check built another source than the update chose"}, candidates[0].Held)

	// Status says it too, from the store and the plan alone. With the
	// repository gone, it reads nothing of it, and says nothing of where
	// the tag points now (source-moved), which takes the network.
	run(t, project, "commit", "-q", "--allow-empty", "-m", "1.8.1, a third time")
	run(t, project, "tag", "-f", "jq-1.8.1")
	require.NoError(t, os.RemoveAll(forge))
	status, err := e.BranchStatus(t.Context(), candidates[0].Branch)
	require.NoError(t, err)
	require.Len(t, status.Moved, 1)
	require.Equal(t, model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "release-moved", Subject: moved, Class: model.Introduced, Detail: candidates[0].Held[0]}, status.Moved[0])

	// Files changed since the check aren't what it planned for: the
	// question waits for a check of them.
	write(t, candidates[0].Branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# edited\n"})
	status, err = e.BranchStatus(t.Context(), candidates[0].Branch)
	require.NoError(t, err)
	require.False(t, status.Current)
	require.Empty(t, status.Moved)
}

// A target --only left out whose tag moved after the check is a concern as
// a built one is: its tag was resolved with the plan, and an earlier
// check's result of it stands for the commit it named then.
func TestASourceLeftOutThatMovedSinceItsCheckIsAConcern(t *testing.T) {
	project := t.TempDir()
	run(t, project, "init", "-q")
	run(t, project, "commit", "-q", "--allow-empty", "-m", "one")
	run(t, project, "tag", "v2")
	built := run(t, project, "rev-parse", "HEAD")
	f := setup(t)
	e := f.open(t)
	evidence := &Evidence{Run: model.Run{Number: 8}, Plan: model.Plan{Omitted: []model.PlanTarget{{ID: "tool", Target: model.Target{Name: "tool"}}},
		Builds: []model.EnvironmentPlan{{Git: map[model.TargetID]model.GitSource{"tool": {URL: project, Ref: "v2", Commit: model.ObjectID(built)}}}}}}
	require.Empty(t, e.movedSources(t.Context(), evidence))

	run(t, project, "commit", "-q", "--allow-empty", "-m", "two")
	run(t, project, "tag", "-f", "v2")
	now := run(t, project, "rev-parse", "HEAD")
	moved := e.movedSources(t.Context(), evidence)
	require.Len(t, moved, 1)
	require.Equal(t, "tool's git.branch v2 named "+built[:7]+" when check-8 planned it, and names "+now[:7]+" now: the check built another source than this would submit", moved[0].Detail)
}

// A Git-fetched port whose assessment read another commit than its check
// planned is a concern, whether an update or a hand edit made it: what
// upstream's change was judged of isn't the source the check built
// (batch 22's leftover, batch 32). One that read the planned commit, or
// kept none, as one assessed before it was kept, says nothing.
func TestAnAssessmentOfAnotherCommitIsAConcern(t *testing.T) {
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
	require.Equal(t, "libharbor's assessment read aaaaaaa, and check-7 planned bbbbbbb: what upstream's change was judged of isn't the source the check built", moved[0].Detail)
	require.Empty(t, assessedSources(evidence, assessed(planned)), "it read what the check built")
	require.Empty(t, assessedSources(evidence, assessed("")), "it kept no commit")
	require.Empty(t, assessedSources(evidence, nil), "no assessment")
	require.Empty(t, assessedSources(nil, assessed(read)), "no check")
}
