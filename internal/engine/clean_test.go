package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// mergedBranch submits an update and has GitHub merge it.
func mergedBranch(t *testing.T) (fixture, *Engine, *forgetest.GitHub, model.Branch) {
	t.Helper()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	fake.PRs[submitted.PullRequest.Ref.Number].State = forge.PullRequestMerged
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, model.BranchMerged, refreshed.Branches[0].Branch.State)
	return f, e, fake, refreshed.Branches[0].Branch
}

func whats(plans []CleanBranch) map[string]string {
	all := map[string]string{}
	for _, plan := range plans {
		for _, step := range plan.Steps {
			all[step.What] = step.Kept
		}
	}
	return all
}

func TestCleanRemovesWhatAMergedBranchLeaves(t *testing.T) {
	t.Parallel()
	_, e, fake, branch := mergedBranch(t)
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree:           "",
		"branch dockhand/jq-update":             "",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(plans))
	require.DirExists(t, branch.Worktree, "a plan removes nothing")

	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	for _, step := range done[0].Steps {
		require.True(t, step.Done, step.What)
	}
	require.NoDirExists(t, branch.Worktree)
	require.Empty(t, fake.ForkHead("dockhand/jq-update"), "the fork's branch is gone")
	// Nothing of the branch's is left; the master dockhand last fetched
	// is the repository's, and stays.
	require.Equal(t, "refs/dockhand/master", testsupport.Git(t, e.Repo.Root, "for-each-ref", "--format=%(refname)", "refs/dockhand/", "refs/heads/dockhand/"))

	again, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, again, "nothing left")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Len(t, merged, 1, "the record stays")
}

// A merged branch retidied after its submit, whose tip isn't the merged
// commit but whose every changed file master has as it has it, goes, saying
// its changes are on master: xapian-core-dsq3 and the others were kept on
// every clean (field testing). One with a change master lacks stays
// (TestCleanKeepsWorkOfItsOwn).
func TestCleanRemovesARetidiedBranchWhoseChangesAreOnMaster(t *testing.T) {
	t.Parallel()
	f, e, _, branch := mergedBranch(t)
	portfile := testsupport.Git(t, branch.Worktree, "show", "HEAD:textproc/jq/Portfile")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": portfile + "\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.1")
	_, err := e.fetchMaster(t.Context())
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "commit", "-q", "--amend", "-m", "jq: update to 1.8.1\n\nRetidied after the submit.")

	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "", whats(plans)["worktree "+branch.Worktree])
	require.Equal(t, "", whats(plans)["branch dockhand/jq-update"])
	for _, step := range plans[0].Steps[:2] {
		require.Equal(t, "its changes are on master", step.Why, step.What)
	}
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.NoDirExists(t, branch.Worktree)
}

// A fork branch no remote pushes to is a step no clean can take: the clean
// that first keeps it says so and records it, and later cleans don't list
// it again, as pr-34539's was on every one (field testing). clean --delete
// then forgets the merged branch, so a person can end its listing.
func TestAnUnreachableForkBranchIsSaidOnce(t *testing.T) {
	t.Parallel()
	f, e, _, _ := mergedBranch(t)
	testsupport.Git(t, f.clone, "remote", "remove", "fork")
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "no Git remote pushes to ada/macports-ports", whats(plans)["ada/macports-ports:dockhand/jq-update"])
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)

	again, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, again, "said once, kept, and not listed again")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Equal(t, "fork branch kept: no Git remote pushes to ada/macports-ports", merged[0].Branch.CleanKept, "status --all still says it")

	plan, err := e.PlanForget(t.Context(), merged[0].Branch)
	require.NoError(t, err)
	_, err = e.Forget(t.Context(), &plan)
	require.NoError(t, err)
	left, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Empty(t, left, "forgotten")
}

func TestCleanKeepsWorkOfItsOwn(t *testing.T) {
	t.Parallel()
	_, e, _, branch := mergedBranch(t)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it has untracked files: notes.txt", whats(plans)["worktree "+branch.Worktree])
	require.Equal(t, "the worktree it is checked out in is kept", whats(plans)["branch dockhand/jq-update"], "planned so, not only found so as clean runs (an escaped mutant)")

	require.NoError(t, os.Remove(filepath.Join(branch.Worktree, "notes.txt")))
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.2\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: update to 1.8.2")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	kept := whats(plans)
	require.Equal(t, "it has commits beyond what was merged", kept["worktree "+branch.Worktree])
	require.Equal(t, "it has commits beyond what was merged", kept["branch dockhand/jq-update"])
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.DirExists(t, branch.Worktree)
	require.NotEmpty(t, testsupport.Git(t, e.Repo.Root, "for-each-ref", "refs/dockhand/checkpoints/"), "checkpoints stay while work does")
	require.True(t, done[0].Steps[2].Done, "the fork's branch still held the merge, so it went")
}

func TestCleanupRemovesWhatCleanWouldAndOldIndexes(t *testing.T) {
	t.Parallel()
	_, e, fake, branch := mergedBranch(t)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	cache := t.TempDir()
	e.options.Indexes = cache
	profile := filepath.Join(cache, strings.Repeat("ab", 32))
	old, fresh := filepath.Join(profile, "generations", strings.Repeat("1", 40)), filepath.Join(profile, "generations", strings.Repeat("2", 40))
	for _, dir := range []string{old, fresh} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(profile, "cache.lock"), nil, 0o644))
	require.NoError(t, os.Chtimes(old, at.Add(-30*24*time.Hour), at.Add(-30*24*time.Hour)))
	require.NoError(t, os.Chtimes(fresh, at, at))

	report, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{old}, report.Indexes)
	require.NoDirExists(t, old)
	require.DirExists(t, fresh)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree:           "it has untracked files: notes.txt",
		"branch dockhand/jq-update":             "the worktree it is checked out in is kept",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(report.Branches))
	require.Equal(t, 2, report.Removed(), "the fork's branch and the old index")
	require.Equal(t, "dockhand/jq-update", testsupport.Git(t, branch.Worktree, "branch", "--show-current"), "the kept worktree keeps its branch")
	require.FileExists(t, filepath.Join(branch.Worktree, "notes.txt"), "work of its own is kept")
	require.Empty(t, fake.ForkHead("dockhand/jq-update"))

	again, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Zero(t, again.Removed(), "what is kept stays kept")
}

// A branch checked out in a worktree clean keeps, one dockhand didn't make
// as an adopted branch's, is kept: deleting it would leave that worktree on
// a branch that's gone, which git branch -d refuses to do. So is one
// checked out in your checkout. (The hugo exercise's cleanup after
// beekeeper-studio and ov, where clean left an adopted branch's worktree
// with nothing to stand on.)
func TestCleanKeepsABranchCheckedOutWhereItStays(t *testing.T) {
	t.Parallel()
	_, e, fake, branch := mergedBranch(t)
	branch.Managed = false
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"branch dockhand/jq-update":             "it is checked out in " + branch.Worktree + "; switch away there first",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(plans), "the worktree isn't dockhand's to remove")
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", strings.TrimSpace(testsupport.Git(t, branch.Worktree, "branch", "--show-current")))
	require.NotEmpty(t, testsupport.Git(t, e.Repo.Root, "for-each-ref", "refs/heads/dockhand/jq-update"), "the branch stays")
	require.Empty(t, fake.ForkHead("dockhand/jq-update"), "the fork's branch still went")

	// Checked out in your checkout instead, it's kept too.
	testsupport.Git(t, branch.Worktree, "switch", "-q", "--detach")
	testsupport.Git(t, e.Repo.Root, "switch", "-q", "dockhand/jq-update")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it is checked out in your checkout; switch away first", whats(plans)["branch dockhand/jq-update"])

	// Planned while nowhere else, then checked out before clean ran: the
	// branch is looked for again before it goes.
	testsupport.Git(t, e.Repo.Root, "switch", "-q", "--detach")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, whats(plans)["branch dockhand/jq-update"])
	testsupport.Git(t, branch.Worktree, "switch", "-q", "dockhand/jq-update")
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it is checked out in "+branch.Worktree+"; switch away there first", done[0].Steps[0].Kept)
	require.NotEmpty(t, testsupport.Git(t, e.Repo.Root, "for-each-ref", "refs/heads/dockhand/jq-update"))
}

// Status only reads: the worktree clean removed from an archived branch
// isn't checked out again for it, as status --all did for duckdb-cxx14,
// undoing clean --archived. Its files are the branch's committed ones.
// Work on the branch still checks it out again (the hugo exercise's
// review, "status --all checks an archived branch out again").
func TestStatusDoesntCheckOutAgainWhatCleanRemoved(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	archived, err := e.Archive(t.Context(), branch, false)
	require.NoError(t, err)
	plans, err := e.PlanClean(t.Context(), model.BranchArchived)
	require.NoError(t, err)
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.NoDirExists(t, archived.Worktree)

	statuses, err := e.Status(t.Context(), model.BranchArchived)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.NoDirExists(t, archived.Worktree, "status checks nothing out")
	head, tree, err := e.Repo.Branch(t.Context(), archived.Name)
	require.NoError(t, err)
	require.Equal(t, head, statuses[0].Head)
	require.Equal(t, tree, statuses[0].Tree, "the committed files")
	require.Empty(t, statuses[0].Edited)

	directory, err := e.Edit(t.Context(), archived, "jq")
	require.NoError(t, err)
	require.DirExists(t, archived.Worktree, "work on it checks it out again")
	require.DirExists(t, filepath.Join(archived.Worktree, directory))
}

// An archived branch's Git branch with nothing master lacks goes with its
// worktree, and status reads it as cleaned, not lost: clean --archived
// kept duckdb-cxx14's, which held no commit beyond master (cleaning up
// duckdb-cxx14, finding 2). One with a commit of its own stays, for path
// to check it out again.
func TestAnArchivedBranchWithNothingMasterLacksGoesWithItsWorktree(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	empty, err := e.Start(t.Context(), StartRequest{Name: "duckdb-cxx14"})
	require.NoError(t, err)
	working, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Edit(t.Context(), working, "jq")
	require.NoError(t, err)
	write(t, working.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	commitAs(t, working.Worktree, "Ada ada@example.org", "jq: update to 1.8.1")
	for _, branch := range []model.Branch{empty, working} {
		_, err = e.Archive(t.Context(), branch, false)
		require.NoError(t, err)
	}
	plans, err := e.PlanClean(t.Context(), model.BranchArchived)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	steps := map[string][]CleanStep{}
	for _, plan := range plans {
		steps[plan.Branch.Name] = plan.Steps
	}
	require.Len(t, steps["dockhand/duckdb-cxx14"], 2)
	require.Equal(t, "branch dockhand/duckdb-cxx14", steps["dockhand/duckdb-cxx14"][1].What)
	require.Equal(t, "it has nothing master lacks", steps["dockhand/duckdb-cxx14"][1].Why)
	require.Len(t, steps["dockhand/jq-update"], 1, "its work stays, for path")

	// One whose port master has since moved on is said, for a look, as the
	// adopted and archived pre-v3 zola branch was (the dogfood run with
	// fb2d195f).
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.2\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.2")
	_, err = e.fetchMaster(t.Context())
	require.NoError(t, err)
	plans, err = e.PlanClean(t.Context(), model.BranchArchived)
	require.NoError(t, err)
	for _, plan := range plans {
		if plan.Branch.Name == "dockhand/jq-update" {
			require.Equal(t, "master has jq at 1.8.2, where the branch took 1.7.1 to 1.8.1", plan.Superseded)
		}
	}

	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Empty(t, testsupport.Git(t, e.Repo.Root, "branch", "--list", "dockhand/duckdb-cxx14"))
	require.NotEmpty(t, testsupport.Git(t, e.Repo.Root, "branch", "--list", "dockhand/jq-update"))
	archived, err := e.Branch(t.Context(), empty.ID)
	require.NoError(t, err)
	status, err := e.BranchStatus(t.Context(), archived)
	require.NoError(t, err)
	require.True(t, status.Cleaned(), "cleaned, not lost")

	// One with no worktree is said all the same, as zola, adopted with
	// none, wasn't (the dogfood run with bf711891).
	adopted, err := e.Branch(t.Context(), working.ID)
	require.NoError(t, err)
	adopted.Managed, adopted.Worktree = false, ""
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(adopted) }))
	plans, err = e.PlanClean(t.Context(), model.BranchArchived)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, "dockhand/jq-update", plans[0].Branch.Name)
	require.Empty(t, plans[0].Steps)
	require.Equal(t, "master has jq at 1.8.2, where the branch took 1.7.1 to 1.8.1", plans[0].Superseded)
}

// Cleanup keeps every finished check's logs compressed, and removes those
// that stand for nothing past the cutoff (D6): an open branch keeps its
// newest check in each environment and its three newest; an ended
// branch's go once it's been ended past the cutoff.
func TestCleanupKeepsLogsThatCount(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	var runs []model.Run
	for range 5 {
		runs = append(runs, checkHead(t, e, branch))
	}
	for _, run := range runs {
		path := filepath.Join(e.LogDirectory(), run.Name(), "tart-26-arm64-1", "target-1.log")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("checking for gcc... gcc\n", 500)), 0o644))
	}
	logOf := func(run model.Run) string {
		return filepath.Join(e.LogDirectory(), run.Name(), "tart-26-arm64-1", "target-1.log")
	}

	done, err := e.cleanLogs(t.Context(), at.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []string{runs[1].Name(), runs[0].Name()}, done.Removed, "the two oldest, superseded and counting for nothing")
	require.Equal(t, 3, done.Compressed.Logs)
	for _, run := range runs[2:] {
		require.FileExists(t, logOf(run)+".gz")
	}
	data, err := buildlog.ReadFile(logOf(runs[4]))
	require.NoError(t, err)
	require.Contains(t, string(data), "checking for gcc")
	require.Contains(t, LogCleanupWords(done, 15*24*time.Hour), "compressed 3 logs")
	require.Contains(t, LogCleanupWords(done, 15*24*time.Hour), "removed 2 checks' logs")

	again, err := e.cleanLogs(t.Context(), at.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, LogCleanup{}, again)

	branch.State, branch.EndedAt = model.BranchArchived, at
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	kept, err := e.cleanLogs(t.Context(), at)
	require.NoError(t, err)
	require.Empty(t, kept.Removed, "ended only since the cutoff")
	ended, err := e.cleanLogs(t.Context(), at.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, ended.Removed, 3)
	require.NoDirExists(t, filepath.Join(e.LogDirectory(), runs[4].Name()))
}

// What a branch ended past the cutoff recorded of its checks goes, but its
// newest, which status shows as it ended, and what reuse may choose; a
// check that's gone is said to be, rather than never there (D6).
func TestCleanupKeepsAnEndedBranchsNewestCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Indexes = t.TempDir()
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	var runs []model.Run
	for range 3 {
		runs = append(runs, checkHead(t, e, branch))
	}
	branch.State, branch.EndedAt = model.BranchArchived, at.Add(-20*24*time.Hour)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))

	report, err := e.Cleanup(t.Context(), session(t, e), 15*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, report.History.Runs, "all but the newest")
	require.Contains(t, HistoryWords(report.History, report.Assessments, 15*24*time.Hour), "removed 2 checks, ")
	require.Contains(t, HistoryWords(report.History, report.Assessments, 15*24*time.Hour), " that branches ended past 15 days had recorded, keeping what reuse may choose and each branch's newest check")
	// What wasn't removed isn't counted: "recorded of 0 checks: 1 result"
	// read as a contradiction (the v0.3.1-rc1 quick stage).
	require.Equal(t, "removed 1 result that branches ended past 1 day had recorded, keeping what reuse may choose and each branch's newest check",
		HistoryWords(store.Pruned{Results: 1}, 0, 24*time.Hour))
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, status.Pruned)
	require.Nil(t, status.Evidence)
	require.Equal(t, runs[2].ID, status.Latest.ID)
	_, err = e.RunNamed(t.Context(), runs[0].Name())
	require.ErrorContains(t, err, runs[0].Name()+" is no longer recorded")
	_, err = e.RunNamed(t.Context(), "check-99")
	require.ErrorContains(t, err, "there is no run check-99")
}

// clean --delete keeps a worktree holding work of its own, as clean and
// archive do, where its forced removal lost uncommitted edits with no
// word; one made dirty after the plan is refused at the removal too.
func TestForgetKeepsAWorktreeWithWorkOfItsOwn(t *testing.T) {
	t.Parallel()
	_, e, _, branch := mergedBranch(t)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	original, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(original, "# mine\n"...), 0o644))
	_, err = e.PlanForget(t.Context(), branch)
	require.ErrorContains(t, err, "jq-update's worktree "+branch.Worktree+" is kept: it has uncommitted edits to textproc/jq/Portfile, which forgetting it would lose")
	require.Equal(t, 3, refusal.Of(err).Kind().Exit(), "a person's look")

	require.NoError(t, os.WriteFile(portfile, original, 0o644))
	plan, err := e.PlanForget(t.Context(), branch)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	_, err = e.Forget(t.Context(), &plan)
	require.ErrorContains(t, err, "it has untracked files: notes.txt")
	require.FileExists(t, filepath.Join(branch.Worktree, "notes.txt"), "kept")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Len(t, merged, 1, "nor is the record forgotten")
}

// Forget deletes the Git branch only at the commit planned: one moved
// since stays, and its record isn't forgotten, where nothing tested it.
func TestForgetKeepsABranchMovedSinceThePlan(t *testing.T) {
	t.Parallel()
	_, e, _, branch := mergedBranch(t)
	plan, err := e.PlanForget(t.Context(), branch)
	require.NoError(t, err)
	require.NotEmpty(t, plan.Head)
	moved := testsupport.Git(t, e.Repo.Root, "commit-tree", plan.Head+"^{tree}", "-p", plan.Head, "-m", "moved since the plan")
	testsupport.Git(t, e.Repo.Root, "update-ref", "refs/heads/"+branch.Name, moved)
	_, err = e.Forget(t.Context(), &plan)
	require.ErrorContains(t, err, "deleting "+branch.Name)
	require.Equal(t, moved, testsupport.Git(t, e.Repo.Root, "rev-parse", "refs/heads/"+branch.Name), "the moved branch stays")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Len(t, merged, 1, "its record isn't forgotten")
}

// clean --delete removes only a worktree dockhand made: a --here branch's
// checkout, the person's own, stays, said in the plan, and while the
// branch is checked out there, or in any checkout clean --delete doesn't
// remove, nothing is forgotten, the refusal naming it and git switch,
// where the branch was deleted from under it.
func TestForgetKeepsACheckoutDockhandDidntMake(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-here", Here: true})
	require.NoError(t, err)
	branch, err = e.Archive(t.Context(), branch, false)
	require.NoError(t, err)
	_, err = e.PlanForget(t.Context(), branch)
	require.ErrorContains(t, err, "dockhand/jq-here is checked out in ")
	require.ErrorContains(t, err, "git switch to another branch there")
	require.Equal(t, 3, refusal.Of(err).Kind().Exit())

	testsupport.Git(t, e.Repo.Root, "switch", "-q", "master")
	plan, err := e.PlanForget(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, plan.Worktree)
	require.Equal(t, branch.Worktree, plan.KeptWorktree)
	_, err = e.Forget(t.Context(), &plan)
	require.NoError(t, err)
	require.DirExists(t, e.Repo.Root, "the person's checkout stays")
	require.Empty(t, testsupport.Git(t, e.Repo.Root, "branch", "--list", "dockhand/jq-here"))
}

// A branch the person checked out in a checkout of their own, after
// archive removed dockhand's, isn't deleted from under it.
func TestForgetKeepsABranchCheckedOutElsewhere(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	archived, err := e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: branch})
	require.NoError(t, err)
	require.NotEmpty(t, archived.Removed)
	own := filepath.Join(t.TempDir(), "mine")
	testsupport.Git(t, e.Repo.Root, "worktree", "add", "-q", own, branch.Name)
	_, err = e.PlanForget(t.Context(), archived.Branch)
	require.ErrorContains(t, err, branch.Name+" is checked out in ")
	require.ErrorContains(t, err, "mine, which clean --delete doesn't remove")
	require.NotEmpty(t, testsupport.Git(t, own, "rev-parse", "HEAD"))
}
