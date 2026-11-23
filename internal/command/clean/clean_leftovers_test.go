package clean

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// leavingBuilder passes every target, names its environment on the
// execution, and leaves the environment behind, as a process that died
// would. Beside them it lists one no check of this checkout made.
type leavingBuilder struct {
	mu     sync.Mutex
	left   []string
	keptAt map[string]time.Time
	failed map[string]bool
	// running are clones dockhand shell is in, which the provider
	// refuses to remove.
	running map[string]bool
}

func (p *leavingBuilder) Name() string { return "command" }

func (p *leavingBuilder) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	ref := "vm-" + string(job.Execution.ID)
	if err := build.Refer(ref); err != nil {
		return err
	}
	p.mu.Lock()
	p.left = append(p.left, ref)
	p.mu.Unlock()
	for _, target := range job.Targets {
		if err := build.Record(buildenv.Reported(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone})); err != nil {
			return err
		}
	}
	return nil
}

func (p *leavingBuilder) Leftovers(context.Context) ([]buildenv.Leftover, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var found []buildenv.Leftover
	for _, ref := range append(slices.Clone(p.left), "vm-elsewhere") {
		found = append(found, buildenv.Leftover{Ref: ref, What: "VM " + ref, KeptAt: p.keptAt[ref], Failed: p.failed[ref]})
	}
	return found, nil
}

func (p *leavingBuilder) keep(ref string, at time.Time, failed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keptAt == nil {
		p.keptAt = map[string]time.Time{}
		p.failed = map[string]bool{}
	}
	p.keptAt[ref], p.failed[ref] = at, failed
}

func (p *leavingBuilder) RemoveLeftover(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[ref] {
		return buildenv.ErrLeftoverRunning
	}
	p.left = slices.DeleteFunc(p.left, func(left string) bool { return left == ref })
	return nil
}

// clean shows what checks left in providers beside the branches, removes
// what a check of this checkout left once no process runs it, and keeps
// what it can't vouch for.
func TestCleanRemovesWhatChecksLeft(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	require.Len(t, builder.left, 1)
	vm := builder.left[0]

	out, _, err := cmdtest.Dockhand(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err), "a script's clean without --yes removes nothing, and refuses")
	require.Equal(t, "Left by checks\n"+
		"  remove   VM "+vm+", left by check-1\n"+
		"  keep     VM vm-elsewhere: no check of this checkout made it\n", out)

	result, err := cmdtest.JSONOf(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Equal(t, vm, cmdtest.Dig(t, result.Result, "leftovers", 0, "ref"))
	require.Equal(t, "check-1", cmdtest.Dig(t, result.Result, "leftovers", 0, "check"))
	require.Equal(t, false, cmdtest.Dig(t, result.Result, "leftovers", 0, "removed"))

	out, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "\nRemoved 1 thing a check left.\n", "one line, once done")
	require.Empty(t, builder.left)

	out, _, err = cmdtest.Dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Left by checks\n  keep     VM vm-elsewhere: no check of this checkout made it\nNothing to remove.\n", out)
}

func TestCleanKeptWithoutSelectorRunsExpiredCacheCleanup(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
}

func TestPlainCleanKeepsAskedCloneAndBranchCleanRemovesIt(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	clone := builder.left[0]
	builder.keep(clone, time.Now(), false)

	out, _, err := cmdtest.Dockhand(t, "clean")
	require.NoError(t, err)
	require.Contains(t, out, "keep     VM "+clone+": kept for dockhand shell")
	require.Contains(t, builder.left, clone, "plain clean leaves an asked clone")

	_, _, err = cmdtest.Dockhand(t, "archive", "jq-update")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "clean", "jq-update", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "remove   VM "+clone+", kept by check-1 for dockhand shell", "the branch cleanup plan names its asked clone")
	require.Contains(t, builder.left, clone, "the plan does not remove it")
	_, _, err = cmdtest.Dockhand(t, "clean", "jq-update", "--yes")
	require.NoError(t, err)
	require.NotContains(t, builder.left, clone, "cleaning its own branch removes the asked clone")
}

func TestPlainCleanKeepsOnlyUnexpiredInCapFailureClones(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	for _, args := range [][]string{{"check"}, {"check", "--fresh"}} {
		_, _, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err)
	}
	require.Len(t, builder.left, 2)
	expired, current := builder.left[0], builder.left[1]
	builder.keep(expired, time.Now().Add(-8*24*time.Hour), true)
	builder.keep(current, time.Now().Add(-2*24*time.Hour), true)

	out, _, err := cmdtest.Dockhand(t, "clean", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "remove   VM "+expired)
	require.Contains(t, out, "keep     VM "+current+": kept for dockhand shell until")
	require.Contains(t, builder.left, expired)
	require.Contains(t, builder.left, current)

	_, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.NotContains(t, builder.left, expired)
	require.Contains(t, builder.left, current)
}

func TestCleanBranchSelectorCleansTheSelectedBranch(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "tidy", "--yes")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept", "-p", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
	_, _, err = cmdtest.Dockhand(t, "archive", "-b", "jq-update", "--keep-worktree")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "clean", "-b", "jq-update", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update")
	out, _, err = cmdtest.Dockhand(t, "clean", "--delete", "-b", "jq-update", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · archived")
}

func TestCleanKeptAllowsOneBranchSelector(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept", "-b", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
}

func TestCleanSelectorsResolveForBranchesAndKeptClones(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "tidy", "--yes")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "archive", "-b", "jq-update", "--keep-worktree")
	require.NoError(t, err)
	for _, args := range [][]string{{"-b", "jq-update"}, {"-p", "jq"}} {
		out, _, err := cmdtest.Dockhand(t, append([]string{"clean"}, append(args, "--plan")...)...)
		require.NoError(t, err, "%v", args)
		require.Contains(t, out, "jq-update")
	}
}

func TestCleanDeleteSelectorsForgetOpenBranchWhoseChangesAreOnMaster(t *testing.T) {
	t.Parallel()
	for _, selector := range [][]string{{"-b", "jq-update"}, {"-p", "jq"}} {
		name := strings.Join(selector, "-")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := cmdtest.NewWorld(t)
			cmdtest.Versioned(t, w)
			cmdtest.WithBumper(t)
			_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
			require.NoError(t, err)
			dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
			cmdtest.Setenv(t, "MACPORTS_TREE", dir)
			_, _, err = cmdtest.Dockhand(t, "update", "jq")
			require.NoError(t, err)
			testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1")
			portfile := testsupport.Git(t, dir, "show", "HEAD:textproc/jq/Portfile")
			testsupport.WriteFiles(t, w.Upstream, map[string]string{"textproc/jq/Portfile": portfile + "\n"})
			testsupport.Git(t, w.Upstream, "add", "textproc/jq/Portfile")
			testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "jq: update to 1.8.1")
			cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
			_, _, err = cmdtest.Dockhand(t, "start", "master-refresh")
			require.NoError(t, err)

			args := append([]string{"clean", "--delete", "--plan"}, selector...)
			out, _, err := cmdtest.Dockhand(t, args...)
			require.NoError(t, err)
			require.Contains(t, out, "jq-update · open")
			require.Contains(t, out, "Nothing was forgotten (--plan).")

			args = append([]string{"clean", "--delete", "--yes"}, selector...)
			out, _, err = cmdtest.Dockhand(t, args...)
			require.NoError(t, err)
			require.Contains(t, out, "Forgot jq-update.")
		})
	}
}

func TestCleanPortSelectorFindsMergedBranchWhoseGitBranchWasDeleted(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1")
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	portfile := testsupport.Git(t, dir, "show", "HEAD:textproc/jq/Portfile")
	testsupport.WriteFiles(t, w.Upstream, map[string]string{"textproc/jq/Portfile": portfile + "\n"})
	testsupport.Git(t, w.Upstream, "add", "textproc/jq/Portfile")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "jq: update to 1.8.1")
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err = cmdtest.Dockhand(t, "start", "master-refresh")
	require.NoError(t, err)
	testsupport.Git(t, w.Clone, "worktree", "remove", "--force", dir)
	testsupport.Git(t, w.Clone, "branch", "-D", "dockhand/jq-update")
	g.PRs[34901].State = forge.PullRequestMerged
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "clean", "--delete", "--plan", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · merged")
	require.Contains(t, out, "Nothing was forgotten (--plan).")
	out, _, err = cmdtest.Dockhand(t, "clean", "--delete", "--yes", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Forgot jq-update.")
}

func TestCleanRefusesWordsAndBranchSelectorTogether(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "clean", "jq-update", "-b", "jq-update")
	require.EqualError(t, err, "clean takes branch words or one selector, not both")
	_, _, err = cmdtest.Dockhand(t, "clean", "--kept", "jq-update", "-b", "jq-update")
	require.EqualError(t, err, "clean takes branch words or one selector, not both")
}

func TestCleanKeptSelectsPullRequestBranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/35174/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{35174: {
		Ref:            forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 35174},
		HeadRepository: "ada/macports-ports", HeadBranch: "contrib", BaseBranch: "master", State: forge.PullRequestOpen,
	}}
	_, _, err := cmdtest.Dockhand(t, "adopt", "--pr", "35174")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept", "--pr", "35174", "--plan")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
	_, _, err = cmdtest.Dockhand(t, "archive", "--pr", "35174", "--keep-worktree")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "clean", "--pr", "35174", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "pr-35174")
}

func TestKeepPlanAndCheckAnnounceAskedCloneReplacement(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check", "--keep")
	require.NoError(t, err)
	clone := builder.left[0]
	builder.keep(clone, time.Now(), false)

	out, _, err := cmdtest.Dockhand(t, "check", "--keep", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `check-1's kept clone of .+ would be removed: this check keeps its own\.`, out, "a plan says what it would do")
	require.NotContains(t, out, "is removed")
	require.Contains(t, builder.left, clone, "--plan never removes the earlier clone")

	out, _, err = cmdtest.Dockhand(t, "check", "--keep")
	require.NoError(t, err)
	require.Regexp(t, `check-1's kept clone of .+ is removed: this check keeps its own\.`, out)
	require.NotContains(t, builder.left, clone, "the queued check replaces the earlier clone")
}

// A kept clone the provider won't remove, since dockhand shell is in it,
// is said as not removed, and a removal that removed nothing exits
// non-zero: "Removed 1 kept clone." was said of one left in place (field
// testing's kept-clone pass, #291).
func TestCleanKeptSaysAClonesInUseWasNotRemoved(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check", "--keep")
	require.NoError(t, err)
	clone := builder.left[0]
	builder.keep(clone, time.Now(), false)
	builder.running = map[string]bool{clone: true}

	out, _, err := cmdtest.Dockhand(t, "clean", "--kept", "check-1", "--yes")
	require.Error(t, err, "nothing asked for was removed")
	require.Contains(t, out, "VM "+clone+", kept by check-1, was not removed: running in dockhand shell")
	require.Contains(t, out, "Nothing was removed.")
	require.NotContains(t, out, "Removed")
	require.Contains(t, builder.left, clone)

	out, _, err = cmdtest.Dockhand(t, "--json", "clean", "--kept", "check-1", "--yes")
	require.Error(t, err)
	require.Contains(t, out, `"kept": "running in dockhand shell"`)
	require.NotContains(t, out, `"removed": true`)
}
