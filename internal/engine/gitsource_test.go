package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// harborRepository is the repository libharbor's Git fetch clones, with
// its tag v4 at a first release, and a function that moves v4 to a new
// commit, as a project that re-tags a release does, returning that commit.
func harborRepository(t *testing.T) (url, first string, move func() string) {
	t.Helper()
	url = filepath.Join(t.TempDir(), "libharbor")
	require.NoError(t, os.MkdirAll(url, 0o755))
	run(t, url, "init", "-q")
	run(t, url, "commit", "-q", "--allow-empty", "-m", "4.0")
	run(t, url, "tag", "v4")
	first = run(t, url, "rev-parse", "HEAD")
	return url, first, func() string {
		run(t, url, "commit", "-q", "--allow-empty", "-m", "4.0, again")
		run(t, url, "tag", "-f", "v4")
		return run(t, url, "rev-parse", "HEAD")
	}
}

// gitHarbor is the harbor ports, with libharbor fetched with Git from url
// at ref, and harbor-cli too where cli is.
func gitHarbor(url, ref string, cli ...bool) fakePorts {
	ports := harborPorts()
	libharbor := port("libharbor")
	libharbor.Options = map[string]string{"fetch.type": "git", "git.url": url, "git.branch": ref}
	ports.directories["devel/libharbor"] = []macports.PortInfo{libharbor}
	if len(cli) > 0 && cli[0] {
		ports.directories["devel/harbor-cli"][0].Options = map[string]string{"fetch.type": "git", "git.url": url, "git.branch": ref}
	}
	return ports
}

// A Git-fetched target's plan expects the commit its tag names as the
// check is planned, in each environment that builds it, and the next
// check's what it names then (batch 20). The Portfile keeps its tag. A tag
// the repository lacks is said, and the check still runs.
func TestAPlanExpectsTheCommitAGitFetchedPortsTagNamesNow(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	revision := harborBranch(t, e)
	e.PortReader = gitHarbor(url, "v4")
	environments := []model.Environment{tahoeArm, tahoeX86}
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: environments})
	require.NoError(t, err)
	for _, environment := range environments {
		source, ok := plan.GitIn(environment, "libharbor")
		require.True(t, ok, "%v", environment)
		require.Equal(t, url, source.URL)
		require.Equal(t, "v4", source.Ref)
		require.Equal(t, model.ObjectID(first), source.Commit)
		require.False(t, source.ResolvedAt.IsZero(), "when it was resolved")
	}
	_, ok := plan.GitIn(tahoeArm, "harbor-cli")
	require.False(t, ok, "a port fetched otherwise expects no commit")

	second := move()
	plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: environments})
	require.NoError(t, err)
	source, _ := plan.GitIn(tahoeArm, "libharbor")
	require.Equal(t, model.ObjectID(second), source.Commit, "the next check expects what the tag names then")
	require.Equal(t, "git.branch v4 names "+second[:7]+" now, which its build must fetch", GitSourceWords(source))

	e.PortReader = gitHarbor(url, "v9")
	plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: environments})
	require.NoError(t, err)
	source, _ = plan.GitIn(tahoeArm, "libharbor")
	require.Empty(t, source.Expected())
	require.Contains(t, source.Unresolved, "has no branch or tag v9")
	require.True(t, plan.Runnable(), "the build says what it fetches, or fails to")
}

// The acceptance fixture of the assessment design's step 4: a moved tag
// doesn't let earlier build evidence stand for the newly resolved commit.
// The first check's build records the commit it fetched, and a check of
// the same files reuses it while the tag names that commit. Once the tag
// moves, the next check expects the new commit: libharbor builds again,
// and so does what was built against its old build, while the rest is
// reused; the new build records the new commit.
func TestAMovedTagDoesntLetEarlierEvidenceStand(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:libharbor"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}, "harbor-viewer": {lib}},
		// A commit said of harbor-cli, fetched otherwise, is none of its
		// inputs, and doesn't keep its build from being reused.
		fetches: map[model.TargetID]string{"libharbor": first, "harbor-cli": first}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	e.PortReader = gitHarbor(url, "v4")
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	check := func() model.Run {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		return run
	}
	fetched := func(run model.Run) model.ObjectID {
		t.Helper()
		var inputs model.TargetInputs
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			executions, err := r.Executions(run.ID)
			if err != nil {
				return err
			}
			results, err := r.Results(executions[0].ID)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(results, func(result model.TargetResult) bool { return result.Target == "libharbor" })
			inputs, err = r.Inputs(results[i].Inputs)
			return err
		}))
		return inputs.Fetched
	}
	built := func(job buildenv.Job) []model.TargetID {
		var ids []model.TargetID
		for _, target := range job.Targets {
			ids = append(ids, target.ID)
		}
		return ids
	}

	first1 := check()
	require.Len(t, provider.jobs, 1)
	require.Equal(t, model.ObjectID(first), fetched(first1), "the result records the commit its build fetched")
	require.Equal(t, &model.GitSource{URL: url, Ref: "v4", Commit: model.ObjectID(first), ResolvedAt: provider.jobs[0].Targets[0].Git.ResolvedAt}, provider.jobs[0].Targets[0].Git,
		"the provider is told the commit the build must fetch")
	check()
	require.Len(t, provider.jobs, 1, "the tag names the same commit: everything is reused")

	second := move()
	provider.fetches["libharbor"] = second
	moved := check()
	require.Len(t, provider.jobs, 2)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, built(provider.jobs[1]),
		"libharbor builds from its new commit, and what was built against its old build builds again")
	require.Equal(t, model.ObjectID(second), fetched(moved))

	// What a later check of the same files leaves out, an earlier check's
	// result fills, where it fetched the commit the later one expects.
	evidence, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, evidence.Failed())
	check()
	require.Len(t, provider.jobs, 2, "and the new build is reused in its turn")
}

// An earlier check of the same files fills in what a later one didn't
// build only where its build fetched the commit the later check expects
// (Counts): once the tag moved, libharbor's earlier result is no evidence
// for the files as they fetch now, and it asks for a check again.
func TestAnEarlierResultFillsInOnlyForTheCommitExpected(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	e.PortReader = gitHarbor(url, "v4")
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	// Each check after the first builds everything afresh, and its
	// environment fails every attempt: it has no result of its own.
	check := func() model.Run {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Fresh: len(provider.jobs) > 0})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		provider.failures = model.MaxAttempts
		return run
	}
	missing := func() []model.TargetID {
		t.Helper()
		evidence, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
		require.NoError(t, err)
		require.True(t, found)
		var ids []model.TargetID
		for _, target := range evidence.Missing() {
			ids = append(ids, target.Target.ID)
		}
		return ids
	}
	require.Equal(t, model.RunPassed, check().State)
	require.Equal(t, model.RunAttention, check().State, "the environment failed: the check built nothing")
	require.Empty(t, missing(), "the earlier check's results fill it in, libharbor's too: it fetched the commit the tag names")

	move()
	require.Equal(t, model.RunAttention, check().State)
	require.Equal(t, []model.TargetID{"libharbor"}, missing(), "the earlier build fetched another commit than the tag names now")
}

// A build that fetched another commit than its check expected, the tag
// moved while the check ran, built another source: it fails at fetch,
// whatever its provider said, and what needs it is blocked. A provider
// that doesn't say what a build fetched leaves its result standing for its
// own check, and says so; no later check reuses it.
func TestABuildThatFetchedAnotherCommitFailsAtFetch(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	second := move()
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	e.PortReader = gitHarbor(url, "v4", true)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	check := func() model.Run {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		return run
	}

	run := check()
	require.Equal(t, model.RunFailed, run.State)
	logs, err := e.Logs(t.Context(), run.ID)
	require.NoError(t, err)
	results := map[model.TargetID]model.TargetResult{}
	for _, result := range logs.Executions[0].Results {
		results[result.Target] = result
	}
	require.Equal(t, model.OutcomeFailed, results["libharbor"].Outcome)
	require.Equal(t, model.PhaseFetch, results["libharbor"].Phase)
	require.Equal(t, "the source moved: git.branch v4 named "+second[:7]+" when the check was planned, and the build fetched "+first[:7]+"; a new check builds what it names now", results["libharbor"].Detail)
	require.Equal(t, model.OutcomeBlocked, results["harbor-cli"].Outcome, "what needs it isn't built against another source")
	fetch := logs.Executions[0].Git["libharbor"]
	require.Equal(t, "fetched "+first[:7]+", not "+second[:7]+", which git.branch v4 named when the check was planned", FetchedWords(fetch.Expected, fetch.Fetched), "its evidence says what it fetched")
	_, found := logs.Executions[0].Git["harbor-cli"]
	require.False(t, found, "a blocked target, fetched with Git too, fetched nothing")
	said := func(run model.Run, words string) bool {
		t.Helper()
		events, err := e.RunEvents(t.Context(), run.ID, 0)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(event model.Event) bool { return strings.Contains(event.Message, words) })
	}
	require.False(t, said(run, "harbor-cli: its build didn't say"), "nor is it said not to have said what")

	provider.fetches["libharbor"] = "v4"
	run = check()
	require.Equal(t, model.RunPassed, run.State, "what isn't a commit is no answer, and the build stands, its source unknown")
	require.True(t, said(run, `libharbor: its provider said its build fetched "v4", which isn't a commit`))

	delete(provider.fetches, "libharbor")
	run = check()
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.True(t, said(run, "libharbor: its build didn't say which commit of git.branch v4 it fetched, so no later check can reuse its result"), "a provider that can't say is said to")
	logs, err = e.Logs(t.Context(), run.ID)
	require.NoError(t, err)
	fetch = logs.Executions[0].Git["libharbor"]
	require.Equal(t, "which commit of git.branch v4 it fetched isn't known: its provider didn't say; "+second[:7]+" was expected", FetchedWords(fetch.Expected, fetch.Fetched))
	jobs := len(provider.jobs)
	check()
	require.Len(t, provider.jobs, jobs+1, "no later check reuses a build that didn't say what it fetched")
	require.Equal(t, model.TargetID("libharbor"), provider.jobs[jobs].Targets[0].ID)
}
