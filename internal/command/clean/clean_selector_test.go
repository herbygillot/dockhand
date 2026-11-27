package clean_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// clean with a selector, -b, -p, or --pr, lists and removes only the
// branch it names: a second finished branch, merged with a fork branch
// dockhand pushed for it, is neither listed nor touched, with or without
// --plan and --delete. A clean with no selector offering such a branch is
// its stated rule (field testing's item 59, architecture's ruling of
// 2026-11-27), and this holds the selectors to theirs.
func TestASelectedCleanLeavesAnotherFinishedBranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	testsupport.WriteFiles(t, w.Upstream, map[string]string{"textproc/yq/Portfile": "name yq\nversion 4.0\n"})
	testsupport.Git(t, w.Upstream, "add", "textproc/yq/Portfile")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "yq: 4.0")
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)

	submitted := func(name, port, edit string) int {
		_, _, err := cmdtest.Dockhand(t, "start", name)
		require.NoError(t, err)
		dir := filepath.Join(w.Home, "Source", "macports-branches", name)
		cmdtest.Setenv(t, "MACPORTS_TREE", dir)
		testsupport.Git(t, dir, "sparse-checkout", "add", "textproc/"+port)
		testsupport.WriteFiles(t, dir, map[string]string{"textproc/" + port + "/Portfile": edit})
		testsupport.Git(t, dir, "commit", "-q", "-am", port+": update")
		_, _, err = cmdtest.Dockhand(t, "tidy", "--yes")
		require.NoError(t, err)
		before := slices.Collect(func(yield func(int) bool) {
			for n := range g.PRs {
				if !yield(n) {
					return
				}
			}
		})
		_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
		require.NoError(t, err)
		number := 0
		for n := range g.PRs {
			if !slices.Contains(before, n) {
				number = n
			}
		}
		require.NotZero(t, number, "%s opened a pull request", name)
		portfile := testsupport.Git(t, dir, "show", "HEAD:textproc/"+port+"/Portfile")
		testsupport.WriteFiles(t, w.Upstream, map[string]string{"textproc/" + port + "/Portfile": portfile + "\n"})
		testsupport.Git(t, w.Upstream, "add", "textproc/"+port+"/Portfile")
		testsupport.Git(t, w.Upstream, "commit", "-q", "-m", port+": update")
		g.PRs[number].State = forge.PullRequestMerged
		cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
		return number
	}
	jq := submitted("jq-update", "jq", "name jq\nversion 1.8.1\n")
	submitted("yq-update", "yq", "name yq\nversion 4.1\n")
	_, _, err := cmdtest.Dockhand(t, "start", "master-refresh")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.NotEmpty(t, g.ForkHead("dockhand/yq-update"), "the second branch's fork branch is dockhand's")

	selectors := [][]string{{"jq-update"}, {"-b", "jq-update"}, {"-p", "jq"}, {"--pr", fmt.Sprint(jq)}}
	for _, selector := range selectors {
		for _, mode := range [][]string{{"--plan"}, {"--delete", "--plan"}} {
			out, _, err := cmdtest.Dockhand(t, append(append([]string{"clean"}, mode...), selector...)...)
			require.NoError(t, err, "%v %v", selector, mode)
			require.Contains(t, out, "jq-update", "%v %v", selector, mode)
			require.NotContains(t, out, "yq-update", "%v %v lists only the branch it names", selector, mode)
		}
	}
	// Removing, jq-update is gone after the first selector; one removal by
	// -b and one forgetting by --pr hold the rest, and yq-update and its
	// fork branch stay throughout.
	out, _, err := cmdtest.Dockhand(t, "clean", "--yes", "-b", "jq-update")
	require.NoError(t, err)
	require.NotContains(t, out, "yq-update")
	require.Empty(t, g.ForkHead("dockhand/jq-update"), "the named branch's fork branch went")
	out, _, err = cmdtest.Dockhand(t, "clean", "--delete", "--yes", "--pr", fmt.Sprint(jq))
	require.NoError(t, err)
	require.Contains(t, out, "Forgot jq-update.")
	require.NotContains(t, out, "yq-update")
	require.NotEmpty(t, g.ForkHead("dockhand/yq-update"), "the unnamed branch's fork branch stays")
	require.Contains(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/yq-update"), "yq-update", "and its Git branch")
	out, _, err = cmdtest.Dockhand(t, "clean", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "yq-update", "a clean with no selector still offers it, by its rule")

	// A command that edits still refuses a merged branch by -b, in its
	// own words.
	_, _, err = cmdtest.Dockhand(t, "edit", "-b", "yq-update", "yq")
	require.ErrorContains(t, err, "yq-update was merged as #")
}
