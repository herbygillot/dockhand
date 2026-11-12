package engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The readers of a check's resolved source, held to one table: the design's
// "What each reader does with each answer" (docs/designs/source-comparison.md),
// each row put through each reader's own entry point, joining through
// QueueCheck, counting a finished check through Covered, an earlier result
// through EvidenceFor, reuse through reuse.Choose (which asks Carry), and
// submit through movedSources. A reader that answers otherwise than the
// table fails by its name, so a reader added later, or an edit to one,
// can't drift from the others unseen.
//
// The per-reader tests stand beside it and cover the rest of the design's
// "It includes": the judgment's tables and old records (model,
// TestTwoReadingsOfASourceAreJudgedOneWay); a fetch-mode change and
// another release's tag (checks, TestAnUnresolvedGitSourceJoinsAndDoesntStand;
// evidence, TestAnEarlierPassStandsOnlyForTheSourceTheNewestPlanFetches);
// each Git result's own fetch (TestAFinishedCheckStandsWhereEachGitResultFetchedTheSource);
// change 4 and dependents of a moved source (evidence,
// TestAnEarlierReleasesResultIsJudgedAgainstTheNewestReading, and through
// status and submit, TestChange4ThroughStatusAndSubmit); a moved tag
// through a whole check and its plan note (TestAMovedGitHubTagIsSaidByCheckAndSubmit);
// and #114's admission (TestAdmissionHonoursTheResolvedSource).
func TestEveryReaderOfASourceAnswersAsTheDesignsTable(t *testing.T) {
	t.Parallel()
	url, first, move := harborRepository(t)
	gone := filepath.Join(t.TempDir(), "gone")
	second := move()
	a, b := model.ObjectID(first), model.ObjectID(second)
	at := time.Now().UTC().Truncate(time.Millisecond)
	reading := func(url, ref string, commit model.ObjectID) *model.GitSource {
		s := &model.GitSource{URL: url, Ref: ref, Commit: commit, ResolvedAt: at}
		if commit == "" {
			s.Unresolved = "the repository couldn't be read"
		}
		return s
	}
	rows := []readerRow{
		{model.SourceSame, reading(url, "v4", a), reading(url, "v4", a), a},
		{model.SourceMoved, reading(url, "v4", a), reading(url, "v4", b), a},
		{model.SourceOther, reading(url, "v4", a), reading(url, "v5", b), a},
		{model.SourceUnresolved, reading(url, "v4", ""), reading(url, "v4", ""), ""},
		{model.SourceUnknown, reading(url, "v4", a), reading(url, "v4", ""), a},
	}
	for _, r := range rows {
		require.Equal(t, r.answer, model.SourceAgainst(r.have, r.want), "the row is its answer")
	}
	words := map[model.SourceStanding]string{model.SourceSame: "same", model.SourceMoved: "moved", model.SourceOther: "other", model.SourceUnresolved: "unresolved", model.SourceUnknown: "not known"}

	// The design's table, Git and tag, reader by answer: true where the
	// reader takes the earlier reading as standing for the newer.
	type expect map[model.SourceStanding]bool
	readers := []struct {
		name string
		tag  bool
		want expect
		read func(t *testing.T, r readerRow, tag bool) bool
	}{
		{"join a queued check, Git", false, expect{model.SourceSame: true, model.SourceUnresolved: true}, joins},
		{"join a queued check, tag", true, expect{model.SourceSame: true, model.SourceUnresolved: true}, joins},
		{"count a finished check, Git", false, expect{model.SourceSame: true}, counts},
		{"count a finished check, tag", true, expect{model.SourceSame: true, model.SourceUnresolved: true}, counts},
		{"take an earlier result into evidence, Git", false, expect{model.SourceSame: true}, fills},
		{"take an earlier result into evidence, tag", true, expect{model.SourceSame: true, model.SourceOther: true, model.SourceUnresolved: true, model.SourceUnknown: true}, fills},
		{"reuse a build, Git", false, expect{model.SourceSame: true}, reuses},
		{"reuse a build, tag", true, expect{model.SourceSame: true, model.SourceOther: true, model.SourceUnresolved: true, model.SourceUnknown: true}, reuses},
	}
	for _, reader := range readers {
		for _, r := range rows {
			require.Equal(t, reader.want[r.answer], reader.read(t, r, reader.tag), "%s: %s", reader.name, words[r.answer])
		}
	}

	// Submit reads the plan's reading against the source read now: what
	// the plan planned (have) and what the repository names now, a
	// reading that can't be had where the repository is gone.
	for _, tag := range []bool{false, true} {
		rule := map[bool]string{false: model.RuleSourceMoved, true: model.RuleTagMoved}[tag]
		for _, c := range []struct {
			answer  model.SourceStanding
			planned *model.GitSource
			concern string
		}{
			{model.SourceSame, reading(url, "v4", b), ""},
			{model.SourceMoved, reading(url, "v4", a), rule},
			{model.SourceUnresolved, reading(gone, "v4", ""), ""},
			{model.SourceUnknown, reading(gone, "v4", a), sourceUnknownRule},
		} {
			require.Equal(t, c.concern, submitConcern(t, c.planned, tag), "submit, tag %v: %s", tag, words[c.answer])
		}
	}
}

// readerRow is one answer as a pair of plans' readings, the earlier (have)
// and the newer (want), and what the earlier build fetched.
type readerRow struct {
	answer     model.SourceStanding
	have, want *model.GitSource
	fetched    model.ObjectID
}

// libharborTarget is the one target the readers' plans build.
var libharborTarget = model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}

// readerPlan is a plan of libharbor on tahoeArm, fetched with Git or by a
// tag as the reading says; nil fetches it neither way.
func readerPlan(revision model.RevisionID, source *model.GitSource, tag bool) model.Plan {
	build := model.EnvironmentPlan{Environment: tahoeArm, Order: []model.TargetID{"libharbor"}}
	if source != nil {
		sources := map[model.TargetID]model.GitSource{"libharbor": *source}
		if tag {
			build.Tags = sources
		} else {
			build.Git = sources
		}
	}
	return model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision, Environments: []model.Environment{tahoeArm}, Tests: model.TestsDeclared, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		Targets: []model.PlanTarget{libharborTarget}, Builds: []model.EnvironmentPlan{build}}
}

// readerBranch is a fresh engine and a branch revision for one reader's row.
func readerBranch(t *testing.T) (*Engine, model.Branch, model.Revision) {
	t.Helper()
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	return e, branch, revision
}

// finish records a queued run's libharbor result in tahoeArm, passed, with
// what its build fetched, and ends the run passed; with no result, it
// ends the run having built nothing there.
func finish(t *testing.T, e *Engine, run model.Run, fetched model.ObjectID, built bool) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		if built {
			execution := model.GuestExecution{ID: model.ExecutionID(store.NewID("command")), Run: run.ID, Environment: tahoeArm, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			key, err := tx.RecordInputs(model.TargetInputs{Environment: "origin a", Tree: "1", Resources: "2", Active: []model.ActivePort{}, Fetched: fetched})
			if err != nil {
				return err
			}
			if err := tx.RecordResult(model.TargetResult{Execution: execution.ID, Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsNone, Inputs: key, RecordedAt: at}); err != nil {
				return err
			}
		}
		for _, state := range []model.RunState{model.RunRunning, model.RunPassed} {
			run.State = state
			if state == model.RunPassed {
				run.FinishedAt = &at
			}
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
		}
		return nil
	}))
}

// joins asks QueueCheck whether a request planned with want follows a
// queued check planned with have.
func joins(t *testing.T, r readerRow, tag bool) bool {
	t.Helper()
	e, branch, revision := readerBranch(t)
	_, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, r.have, tag), model.OriginPerson)
	require.NoError(t, err)
	got, err := e.QueueCheck(t.Context(), checks.QueueRequest{Branch: branch, Revision: revision, Plan: readerPlan(revision.ID, r.want, tag), Origin: model.OriginPerson, Beside: checks.BesideFollow})
	return err == nil && got.Followed
}

// counts asks Covered whether a finished check planned with have, whose
// build fetched what the row says, stands for a request planned with want.
func counts(t *testing.T, r readerRow, tag bool) bool {
	t.Helper()
	e, branch, revision := readerBranch(t)
	run, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, r.have, tag), model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, run, r.fetched, true)
	_, covered, err := e.Covered(t.Context(), branch.ID, revision.Source.Tree, readerPlan(revision.ID, r.want, tag))
	require.NoError(t, err)
	return covered
}

// fills asks EvidenceFor whether an earlier check's pass, planned with
// have, fills the newest check's empty cell, planned with want.
func fills(t *testing.T, r readerRow, tag bool) bool {
	t.Helper()
	e, branch, revision := readerBranch(t)
	earlier, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, r.have, tag), model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, earlier, r.fetched, true)
	newest, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, r.want, tag), model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, newest, "", false)
	judged, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	for _, target := range judged.Targets {
		if target.Target.ID == "libharbor" {
			return target.Outcomes[0].Kind == CellRecorded
		}
	}
	return false
}

// reuses asks reuse.Choose whether an earlier build, its check planned
// with have, is reused by a target planned with want.
func reuses(t *testing.T, r readerRow, tag bool) bool {
	t.Helper()
	trees := map[string]model.ObjectID{"devel/libharbor": "1", macports.ResourcesDirectory: "2"}
	inputs := model.NewTargetInputs("origin a", "devel/libharbor", "1", "2", nil, []model.ActivePort{})
	target := reuse.Target{PlanTarget: libharborTarget}
	candidate := reuse.Candidate{Result: model.TargetResult{Target: "libharbor", Outcome: model.OutcomePassed, Execution: "command_1"}}
	if tag {
		target.Tag, candidate.Tagged = r.want, r.have
	} else {
		target.Git = r.want
		inputs.Fetched = r.fetched
	}
	candidate.Inputs = inputs
	target.Earlier = []reuse.Candidate{candidate}
	choice := reuse.Choose([]reuse.Target{target}, "origin a", trees, reuse.Judge{}, func(model.TargetResult) bool { return true }, func(reuse.Candidate) bool { return false })
	_, reused := choice.Reused["libharbor"]
	return reused
}

// submitConcern is the rule of the concern movedSources raises for a
// plan's reading against what the repository names now, or nothing.
func submitConcern(t *testing.T, planned *model.GitSource, tag bool) string {
	t.Helper()
	e := setup(t).open(t)
	concerns := e.movedSources(t.Context(), &Evidence{Run: model.Run{Number: 1}, Plan: readerPlan("", planned, tag)})
	switch len(concerns) {
	case 0:
		return ""
	case 1:
		return concerns[0].Rule
	}
	return fmt.Sprintf("%d concerns", len(concerns))
}

// The reader table's rows beyond one earlier check (Codex's review of #192,
// finding 4): a reading that isn't known, beside another earlier check that
// shows the tag moved, stands for nothing through EvidenceFor and is reused
// by nothing through reuse.Choose; and an earlier extra built with Git,
// which the newest plan doesn't build, is judged against its own plan
// through EvidenceFor.
func TestTheReadersAgreeAcrossSeveralEarlierChecks(t *testing.T) {
	t.Parallel()
	url, first, move := harborRepository(t)
	a, b := model.ObjectID(first), model.ObjectID(move())
	at := time.Now().UTC().Truncate(time.Millisecond)
	reading := func(commit model.ObjectID) *model.GitSource {
		s := &model.GitSource{URL: url, Ref: "v4", Commit: commit, ResolvedAt: at}
		if commit == "" {
			s.Unresolved = "the repository couldn't be read"
		}
		return s
	}

	// Through EvidenceFor: an unresolved check's pass, then one at A, then
	// the newest at B, built nothing.
	e, branch, revision := readerBranch(t)
	for _, have := range []*model.GitSource{reading(""), reading(a)} {
		run, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, have, true), model.OriginPerson)
		require.NoError(t, err)
		finish(t, e, run, "", true)
	}
	newest, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, reading(b), true), model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, newest, "", false)
	judged, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEqual(t, CellRecorded, judged.Targets[0].Outcomes[0].Kind, "the unresolved check's pass doesn't fill in once another shows the tag moved")

	// Through reuse.Choose: an unresolved candidate beside one at A, for a
	// target at B.
	trees := map[string]model.ObjectID{"devel/libharbor": "1", macports.ResourcesDirectory: "2"}
	inputs := model.NewTargetInputs("origin a", "devel/libharbor", "1", "2", nil, []model.ActivePort{})
	candidate := func(tagged *model.GitSource) reuse.Candidate {
		return reuse.Candidate{Result: model.TargetResult{Target: "libharbor", Outcome: model.OutcomePassed, Execution: "command_1"}, Inputs: inputs, Tagged: tagged}
	}
	target := reuse.Target{PlanTarget: libharborTarget, Tag: reading(b), Earlier: []reuse.Candidate{candidate(reading(a)), candidate(reading(""))}}
	choice := reuse.Choose([]reuse.Target{target}, "origin a", trees, reuse.Judge{}, func(model.TargetResult) bool { return true }, func(reuse.Candidate) bool { return false })
	require.Empty(t, choice.Reused, "nothing stands for B")
}

// An earlier --also Git build the newest plan lacks, planned at A and
// having fetched B, counts for nothing through EvidenceFor, though the
// newest plan fetches nothing with Git (Codex's review of #192, findings 3
// and 4).
func TestAnEarlierExtraGitBuildCountsAgainstItsOwnPlan(t *testing.T) {
	t.Parallel()
	url, first, move := harborRepository(t)
	a, b := model.ObjectID(first), model.ObjectID(move())
	e, branch, revision := readerBranch(t)
	earlier := readerPlan(revision.ID, &model.GitSource{URL: url, Ref: "v4", Commit: a, ResolvedAt: time.Now()}, false)
	earlier.Targets[0].Role, earlier.Targets[0].Kind = model.Also, model.Unchanged
	zlib := model.PlanTarget{ID: "zlib", Target: model.Target{Name: "zlib"}, Directory: "archivers/zlib", Kind: model.Substantive, Role: model.Changed}
	earlier.Targets = append([]model.PlanTarget{zlib}, earlier.Targets...)
	earlier.Builds[0].Order = []model.TargetID{"zlib", "libharbor"}
	run, err := e.Enqueue(t.Context(), branch, earlier, model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, run, b, true)
	newest := readerPlan(revision.ID, nil, false)
	newest.Targets = []model.PlanTarget{zlib}
	newest.Builds[0].Order = []model.TargetID{"zlib"}
	later, err := e.Enqueue(t.Context(), branch, newest, model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, later, "", false)
	judged, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	for _, target := range judged.Targets {
		if target.Target.ID == "libharbor" {
			require.NotEqual(t, CellRecorded, target.Outcomes[0].Kind, "a build that fetched B for a plan that named A counts for nothing")
		}
	}
}

// Change 1 through QueueCheck, both ways: two requests whose Git source
// couldn't be resolved join one queued check, and a finished check of an
// unresolved source doesn't stand for another request (Codex's review of
// #192, finding 4).
func TestAnUnresolvedGitSourceThroughQueueCheck(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "gone")
	unresolved := &model.GitSource{URL: gone, Ref: "v4", Unresolved: "the repository couldn't be read", ResolvedAt: time.Now()}

	e, branch, revision := readerBranch(t)
	_, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, unresolved, false), model.OriginPerson)
	require.NoError(t, err)
	joined, err := e.QueueCheck(t.Context(), checks.QueueRequest{Branch: branch, Revision: revision, Plan: readerPlan(revision.ID, unresolved, false), Origin: model.OriginPerson, Beside: checks.BesideFollow})
	require.NoError(t, err)
	require.True(t, joined.Followed, "two unresolved requests join one check")

	e, branch, revision = readerBranch(t)
	run, err := e.Enqueue(t.Context(), branch, readerPlan(revision.ID, unresolved, false), model.OriginPerson)
	require.NoError(t, err)
	finish(t, e, run, model.ObjectID(strings.Repeat("a", 40)), true)
	queued, err := e.QueueCheck(t.Context(), checks.QueueRequest{Branch: branch, Revision: revision, Plan: readerPlan(revision.ID, unresolved, false), Origin: model.OriginPerson, Covered: true})
	require.NoError(t, err)
	require.Nil(t, queued.Passed, "a finished check of an unresolved source stands for no other request")
	require.NotEmpty(t, queued.Run.ID, "a check is queued instead")
}

// Change 4 through its entry points, for a Git source and for a GitHub
// tag (docs/designs/source-comparison.md, "The readers agree"): a check
// on one release, the ref moved, a second check on another release. On
// the earlier release the moved target's result and its dependent's read
// as not checked, an unrelated target's still counts, and the release
// stays required, in EvidenceFor, in status, and in submit's plan, each
// read from checks the engine ran; and status's cell and submit's hold
// say why each result doesn't count, alike, where the hold said the check
// had stopped before building it (architecture's judgment of #192's
// confirmation, and of the fault this test found).
func TestChange4ThroughStatusAndSubmit(t *testing.T) {
	t.Parallel()
	sequoia := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	for _, tagged := range []bool{false, true} {
		name := map[bool]string{false: "a Git source", true: "a GitHub tag"}[tagged]
		f := setup(t)
		// harbor-cli, built against libharbor, is the dependent; jq is
		// the unrelated target.
		testsupport.WriteFiles(t, f.upstream, map[string]string{"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 0\n"})
		testsupport.Git(t, f.upstream, "add", "-A")
		testsupport.Git(t, f.upstream, "commit", "-q", "-m", "harbor-cli: new port")
		e, _ := f.withPreparer(t)
		f.withFork(t, e)
		branch := committedUpdate(t, e)
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "devel/libharbor", "devel/harbor-cli")
		testsupport.WriteFiles(t, branch.Worktree, map[string]string{
			"devel/libharbor/Portfile":  "name libharbor\nversion 3\n",
			"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 1\n",
		})
		testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "libharbor: update to 3")
		url, first, move := harborRepository(t)
		libharbor := port("libharbor")
		if tagged {
			libharbor.Options = map[string]string{"fetch.type": "standard", "github.author": "harbor", "github.project": "libharbor", "git.url": url, "git.branch": "v4"}
		} else {
			libharbor.Options = map[string]string{"fetch.type": "git", "git.url": url, "git.branch": "v4"}
		}
		usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
			"textproc/jq": {port("jq")}, "devel/libharbor": {libharbor}, "devel/harbor-cli": {port("harbor-cli", "libharbor")},
		}})
		provider := &identified{identity: "origin a", scriptedProvider: scriptedProvider{active: []model.ActivePort{},
			consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {{Name: "libharbor", Archive: "sha256:libharbor"}}}}}
		e.Providers = map[string]buildenv.Provider{"command": provider}
		// build.default's profile requires both releases.
		e.required.mu.Lock()
		e.required.at, e.required.profile, e.required.environments = time.Now(), e.defaultProfile(), []model.Environment{tahoeArm, sequoia}
		e.required.mu.Unlock()
		checkOn := func(environment model.Environment, fetched string) model.Run {
			t.Helper()
			if !tagged {
				provider.fetches = map[model.TargetID]string{"libharbor": fetched}
			}
			capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
			require.NoError(t, err)
			plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{environment}})
			require.NoError(t, err)
			queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
			require.NoError(t, err)
			completed, err := e.Drive(t.Context(), session(t, e), queued.ID)
			require.NoError(t, err)
			require.Equal(t, model.RunPassed, completed.State, "%s: %s", name, completed.Detail)
			return completed
		}
		earlier := checkOn(sequoia, first)
		second := move()
		checkOn(tahoeArm, second)
		moved := "built by " + earlier.Name() + " from v4 at " + first[:7] + ", which now names " + second[:7] + ", so it doesn't count; dockhand check builds it again"
		against := "built by " + earlier.Name() + " against libharbor from v4 at " + first[:7] + ", which has moved, so it doesn't count; dockhand check builds it again"

		// judged reads one reader's evidence: on the earlier release the
		// moved target and its dependent aren't checked, and say why, and
		// the unrelated target's result counts; the newest check's all do.
		judged := func(reader string, evidence Evidence) {
			t.Helper()
			on := slices.Index(evidence.Plan.Environments, sequoia)
			require.GreaterOrEqual(t, on, 0, "%s, %s: the earlier release stays required", name, reader)
			words := map[model.TargetID]string{}
			for _, target := range evidence.Targets {
				words[target.Target.ID] = EvidenceWords(evidence, target, on, AcceptedFailure{})
				if target.Target.ID == "jq" {
					require.Equal(t, CellRecorded, target.Outcomes[on].Kind, "%s, %s: the unrelated target's result on the earlier release counts", name, reader)
				} else {
					require.Equal(t, CellNotRun, target.Outcomes[on].Kind, "%s, %s: %s's result there reads as not checked", name, reader, target.Target.ID)
				}
				newest := slices.Index(evidence.Plan.Environments, tahoeArm)
				require.Equal(t, CellRecorded, target.Outcomes[newest].Kind, "%s, %s: the newest check's %s counts", name, reader, target.Target.ID)
			}
			require.Equal(t, "! not run: "+moved, words["libharbor"], "%s, %s", name, reader)
			require.Equal(t, "! not run: "+against, words["harbor-cli"], "%s, %s", name, reader)
			require.Equal(t, "✓", words["jq"], "%s, %s: the unrelated target says nothing more", name, reader)
		}

		evidence, found, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
		require.NoError(t, err)
		require.True(t, found, name)
		judged("EvidenceFor", evidence)

		status, err := e.BranchStatus(t.Context(), branch)
		require.NoError(t, err)
		require.NotNil(t, status.Evidence, name)
		judged("status", *status.Evidence)

		submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor, harbor-cli: update"})
		require.NoError(t, err)
		require.NotNil(t, submission.Evidence, name)
		judged("submit", *submission.Evidence)
		require.Equal(t, []string{
			"libharbor has changed, but these files haven't been built on command macOS 15 (Sequoia) arm64: " + moved + ". Run dockhand check, or share the branch with --draft",
			"harbor-cli has changed, but these files haven't been built on command macOS 15 (Sequoia) arm64: " + against + ". Run dockhand check, or share the branch with --draft",
		}, submission.Blocking, "%s: submit holds for the moved target and its dependent, as status says, and not for jq", name)
	}
}
