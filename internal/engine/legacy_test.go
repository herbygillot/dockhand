package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Branches from before v3, which nothing tracks, are sorted by what master
// has of each: one whose change master has, by patch-id, goes with your
// fork's branch holding the same commit; one whose port master has at
// another version is for a person to look at; the rest are left for
// adopt. A tracked one isn't from before v3 (the cleanup, finding 3).
func TestBranchesFromBeforeV3AreSortedByWhatMasterHas(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	f.withFork(t, e)
	branchFrom := func(name, start string, files map[string]string, message string) {
		t.Helper()
		run(t, f.clone, "switch", "-q", "-c", name, start)
		write(t, f.clone, files)
		run(t, f.clone, "add", "-A")
		run(t, f.clone, "commit", "-q", "-m", message)
		run(t, f.clone, "switch", "-q", "master")
	}
	branch := func(name string, files map[string]string, message string) {
		t.Helper()
		branchFrom(name, "master", files, message)
	}
	// Upstream made this same change as libharbor: update to 2.
	branch("dockhand/bump/libharbor-4f2a", map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"}, "libharbor: update to 2, mine")
	run(t, f.clone, "push", "-q", "fork", "dockhand/bump/libharbor-4f2a")
	branch("dockhand/bump/jq-9c1d", map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.0\n"}, "jq: update to 1.8.0")
	branch("dockhand/bump/newport-77aa", map[string]string{"devel/newport/Portfile": "name newport\nversion 1\n"}, "newport: new port")
	branch("dockhand/bump/adopted-5e5e", map[string]string{"devel/adopted/Portfile": "name adopted\n"}, "adopted: new port")
	_, err := e.Adopt(t.Context(), AdoptRequest{Branch: "dockhand/bump/adopted-5e5e"})
	require.NoError(t, err)
	// One your fork has and this checkout doesn't, whose change master has.
	branch("dockhand/bump/libharbor-f0f0", map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"}, "libharbor: update to 2, on the fork")
	run(t, f.clone, "push", "-q", "fork", "dockhand/bump/libharbor-f0f0")
	run(t, f.clone, "branch", "-q", "-D", "dockhand/bump/libharbor-f0f0")
	// One that takes a port master hasn't moved.
	write(t, f.upstream, map[string]string{"textproc/yq/Portfile": "name yq\nversion 4.54.1\n"})
	run(t, f.upstream, "add", "-A")
	run(t, f.upstream, "commit", "-q", "-m", "yq: new port")
	run(t, f.clone, "fetch", "-q", "origin")
	branchFrom("dockhand/bump/yq-1111", "origin/master", map[string]string{"textproc/yq/Portfile": "name yq\nversion 4.55.0\n"}, "yq: update to 4.55.0")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	run(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.1")

	names, err := e.LegacyBranchNames(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"dockhand/bump/jq-9c1d", "dockhand/bump/libharbor-4f2a", "dockhand/bump/newport-77aa", "dockhand/bump/yq-1111"}, names, "this checkout's, not the tracked one")

	plans, err := e.PlanLegacy(t.Context())
	require.NoError(t, err)
	byName := map[string]LegacyBranch{}
	for _, plan := range plans {
		byName[plan.Name] = plan
	}
	require.Len(t, byName, 5)
	jq, libharbor, fork := byName["dockhand/bump/jq-9c1d"], byName["dockhand/bump/libharbor-4f2a"], byName["dockhand/bump/libharbor-f0f0"]
	require.Equal(t, LegacySuperseded, jq.Kind)
	require.Equal(t, "master has jq at 1.8.1, where the branch took 1.7.1 to 1.8.0", jq.Detail)
	require.Equal(t, LegacyOnMaster, libharbor.Kind)
	require.Equal(t, `master has the same change as its "libharbor: update to 2, mine"`, libharbor.Detail)
	require.Equal(t, "ada/macports-ports:dockhand/bump/libharbor-4f2a", libharbor.Fork)
	require.Equal(t, LegacyOnMaster, fork.Kind)
	require.True(t, fork.ForkOnly)
	require.Equal(t, LegacyUnfinished, byName["dockhand/bump/newport-77aa"].Kind)
	require.Equal(t, "1 commit master hasn't", byName["dockhand/bump/newport-77aa"].Detail)
	require.Equal(t, "takes yq from 4.54.1 to 4.55.0, which master still has at 4.54.1", byName["dockhand/bump/yq-1111"].Detail)

	done, err := e.RemoveLegacy(t.Context(), plans)
	require.NoError(t, err)
	for _, branch := range done {
		require.Equal(t, branch.Kind == LegacyOnMaster, branch.Done, branch.Name)
	}
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/bump/libharbor-4f2a"))
	require.NotEmpty(t, run(t, f.clone, "branch", "--list", "dockhand/bump/jq-9c1d"), "a look first")
	require.Empty(t, run(t, f.clone, "ls-remote", "fork", "refs/heads/dockhand/bump/libharbor-4f2a"), "and your fork's")
	require.Empty(t, run(t, f.clone, "ls-remote", "fork", "refs/heads/dockhand/bump/libharbor-f0f0"), "and the one only your fork had")
}
