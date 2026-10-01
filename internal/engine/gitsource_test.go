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
// for the files as they fetch now, and it asks for a check again. Nor are
// the results of what was built against it, harbor-cli with libharbor
// active, and harbor-viewer against that harbor-cli too, as reuse would
// build them again (reuse.AgainstOtherSources).
func TestAnEarlierResultFillsInOnlyForTheCommitExpected(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:libharbor"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}, "harbor-viewer": {lib}},
		fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
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
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, missing(),
		"the earlier build fetched another commit than the tag names now, and the others were built against it")
}

// harborChecks checks the harbor branch on one environment with a
// provider and ports of its own, and reads what the evidence of its files
// still misses.
type harborChecks struct {
	t        *testing.T
	e        *Engine
	branch   model.Branch
	revision model.Revision
}

func newHarborChecks(t *testing.T, e *Engine, ports fakePorts, provider buildenv.Provider) harborChecks {
	t.Helper()
	e.Providers = map[string]buildenv.Provider{"command": provider}
	c := harborChecks{t: t, e: e, revision: harborBranch(t, e)}
	e.PortReader = ports
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		c.branch, err = r.Branch(c.revision.Branch)
		return err
	}))
	return c
}

// check plans and drives a check, fresh where asked, of the ports named,
// or of every one.
func (c harborChecks) check(fresh bool, only ...string) (model.Run, model.Plan) {
	c.t.Helper()
	plan, err := c.e.PlanCheck(c.t.Context(), PlanRequest{Revision: c.revision, Environments: []model.Environment{tahoeArm}, Only: only, Fresh: fresh})
	require.NoError(c.t, err)
	queued, err := c.e.Enqueue(c.t.Context(), c.branch, plan, model.OriginPerson)
	require.NoError(c.t, err)
	run, err := c.e.Drive(c.t.Context(), session(c.t, c.e), queued.ID)
	require.NoError(c.t, err)
	return run, plan
}

// missing are the targets no check of the files built where the evidence
// asks for them (Evidence.Missing).
func (c harborChecks) missing() []model.TargetID {
	c.t.Helper()
	evidence, found, err := c.e.EvidenceFor(c.t.Context(), c.branch.ID, c.revision.Source.Tree)
	require.NoError(c.t, err)
	require.True(c.t, found)
	var ids []model.TargetID
	for _, target := range evidence.Missing() {
		ids = append(ids, target.Target.ID)
	}
	return ids
}

// A Git-fetched target --only leaves out still has the commit its tag
// names expected of it (planning.OmittedSources): the check doesn't build
// it, but an earlier check's result of it, or of what was built against
// it, stands only for that commit. Here harbor-cli needs neither of the
// others, and a check of it alone leaves libharbor and harbor-viewer to an
// earlier full check, which stands while the tag names what it built.
func TestATargetLeftOutExpectsTheCommitItsTagNames(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:libharbor"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-viewer": {lib}},
		fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	ports := gitHarbor(url, "v4")
	ports.directories["devel/harbor-cli"][0] = port("harbor-cli")
	c := newHarborChecks(t, e, ports, provider)

	run, _ := c.check(false)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	_, plan := c.check(false, "harbor-cli")
	require.Equal(t, []string{"libharbor:substantive:changed", "harbor-viewer:substantive:changed"}, names(plan.Omitted))
	source, ok := plan.GitIn(tahoeArm, "libharbor")
	require.True(t, ok, "left out, and expected all the same")
	require.Equal(t, model.ObjectID(first), source.Commit)
	require.Empty(t, c.missing(), "the full check fetched the commit the tag names")

	second := move()
	_, plan = c.check(false, "harbor-cli")
	source, _ = plan.GitIn(tahoeArm, "libharbor")
	require.Equal(t, model.ObjectID(second), source.Commit)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-viewer"}, c.missing(),
		"the full check's libharbor fetched another commit than the tag names now, and harbor-viewer was built against it")
}

// A failure is judged as a pass is: harbor-cli's build that failed against
// libharbor's build of the old commit is no evidence once the tag moved.
// A blocked result built nothing, and isn't judged by what it was built
// against, but stands only with what blocked it (standing): harbor-viewer,
// blocked by harbor-cli, stands while nothing moved, and once harbor-cli's
// failure no longer stands, its block doesn't either.
func TestAnEarlierFailureAgainstAnOldCommitDoesntStand(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:libharbor"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}},
		outcomes: map[model.TargetID]model.Outcome{"harbor-cli": model.OutcomeFailed}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	c := newHarborChecks(t, e, gitHarbor(url, "v4"), provider)

	run, _ := c.check(false)
	require.Equal(t, model.RunFailed, run.State)
	// Each check after the first has no result of its own: its
	// environment fails every attempt.
	provider.failures = model.MaxAttempts
	run, _ = c.check(true)
	require.Equal(t, model.RunAttention, run.State)
	require.Empty(t, c.missing(), "harbor-cli's failure and harbor-viewer's block stand")

	move()
	provider.failures = model.MaxAttempts
	c.check(true)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, c.missing(),
		"harbor-cli failed against the old commit's build, and harbor-viewer was blocked by that failure")
}

// A blocked result stands only with what blocked it (standing). Here
// libharbor's build of the tag's first commit fails, and blocks harbor-cli
// and harbor-viewer. While the tag names that commit, the failure and the
// blocks stand for a later check that builds nothing; once the tag moves,
// the failure is no evidence for the files as they fetch now, and neither
// is what it blocked: all three ask for a check, rather than harbor-cli
// reading as blocked by a failure that no longer stands.
func TestABlockStandsOnlyWithWhatBlockedIt(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{},
		outcomes: map[model.TargetID]model.Outcome{"libharbor": model.OutcomeFailed}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	c := newHarborChecks(t, e, gitHarbor(url, "v4"), provider)

	run, _ := c.check(false)
	require.Equal(t, model.RunFailed, run.State)
	provider.failures = model.MaxAttempts
	run, _ = c.check(true)
	require.Equal(t, model.RunAttention, run.State)
	evidence, found, err := e.EvidenceFor(t.Context(), c.branch.ID, c.revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, evidence.Missing(), "the failure and what it blocked stand while the tag names what was built")
	for _, target := range evidence.Targets {
		if target.Target.ID == "harbor-cli" || target.Target.ID == "harbor-viewer" {
			require.Equal(t, model.OutcomeBlocked, target.Outcomes[0].Outcome, target.Target.ID)
		}
	}

	move()
	provider.failures = model.MaxAttempts
	c.check(true)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, c.missing(),
		"libharbor's failure was of the old commit, and the blocks stand with it")
}

// A block stands only with what blocked it, whether or not anything is
// fetched with Git: once a check --only narrows to the blocker passes, the
// earlier check's block of what it left out stands for nothing, and asks
// for a check, rather than reading as blocked by a failure the evidence no
// longer holds.
func TestABlockDoesntOutliveALaterPassOfItsBlocker(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &identified{scriptedProvider: scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"libharbor": model.OutcomeFailed}}, identity: "origin a"}
	c := newHarborChecks(t, e, harborPorts(), provider)

	run, _ := c.check(false)
	require.Equal(t, model.RunFailed, run.State)
	delete(provider.outcomes, "libharbor")
	run, _ = c.check(false, "libharbor")
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Equal(t, []model.TargetID{"harbor-cli", "harbor-viewer"}, c.missing(), "libharbor passes now, so nothing blocks them")
}

// A fetch that failed because the source moved built nothing, so it
// stands for no later check, even one that expects the commit it fetched:
// a tag that moved between a check's plan and its fetch fails that check
// at fetch, and the next check, which expects what the tag names now,
// builds it rather than reading that failure as its own.
func TestAFetchThatFoundItsSourceMovedStandsForNone(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, move := harborRepository(t)
	move()
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	c := newHarborChecks(t, e, gitHarbor(url, "v4"), provider)

	checked, _ := c.check(false)
	require.Equal(t, model.RunFailed, checked.State, "libharbor fetched another commit than its check expected")
	run(t, url, "tag", "-f", "v4", first)
	provider.failures = model.MaxAttempts
	c.check(true)
	require.Contains(t, c.missing(), model.TargetID("libharbor"), "the fetch that found its source moved built nothing")
}

// A build whose provider didn't say which ports were active can't be
// established to have been built against the commit a Git-fetched target
// it needs is expected at, as reuse can't establish it, so an earlier
// check's result of it doesn't stand for a later check, though the tag
// names what it did.
func TestADependentWhoseProviderDidntSayWhatWasActiveDoesntStand(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, first, _ := harborRepository(t)
	provider := &identified{scriptedProvider: scriptedProvider{fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	c := newHarborChecks(t, e, gitHarbor(url, "v4"), provider)

	run, _ := c.check(false)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	provider.failures = model.MaxAttempts
	c.check(true)
	require.Equal(t, []model.TargetID{"harbor-cli", "harbor-viewer"}, c.missing(), "libharbor's result says what it fetched; what needs it can't say what it had")
}

// A build that had active a Git-fetched target its check didn't build, as
// harbor-cli had libharbor through a port the branch doesn't change while
// --only left libharbor out, had an archive no build dockhand can place
// made, so its result doesn't stand once the plan expects libharbor at a
// commit.
func TestABuildAgainstAGitFetchedPortItsCheckDidntBuildDoesntStand(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	url, _, _ := harborRepository(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:master's libharbor"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}}}, identity: "origin a"}
	ports := gitHarbor(url, "v4")
	ports.directories["devel/harbor-cli"][0] = port("harbor-cli")
	ports.directories["graphics/harbor-viewer"][0] = port("harbor-viewer")
	c := newHarborChecks(t, e, ports, provider)

	run, _ := c.check(false, "harbor-cli")
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	run, _ = c.check(false, "harbor-viewer")
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Equal(t, []model.TargetID{"harbor-cli", "libharbor"}, c.missing(), "no check built libharbor, and harbor-cli had one nobody can place")
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
