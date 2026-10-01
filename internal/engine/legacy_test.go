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
	branch := func(name string, files map[string]string, message string) {
		t.Helper()
		run(t, f.clone, "switch", "-q", "-c", name, "master")
		write(t, f.clone, files)
		run(t, f.clone, "add", "-A")
		run(t, f.clone, "commit", "-q", "-m", message)
		run(t, f.clone, "switch", "-q", "master")
	}
	// Upstream made this same change as libharbor: update to 2.
	branch("dockhand/bump/libharbor-4f2a", map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"}, "libharbor: update to 2, mine")
	run(t, f.clone, "push", "-q", "fork", "dockhand/bump/libharbor-4f2a")
	branch("dockhand/bump/jq-9c1d", map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.0\n"}, "jq: update to 1.8.0")
	branch("dockhand/bump/newport-77aa", map[string]string{"devel/newport/Portfile": "name newport\nversion 1\n"}, "newport: new port")
	branch("dockhand/bump/adopted-5e5e", map[string]string{"devel/adopted/Portfile": "name adopted\n"}, "adopted: new port")
	_, err := e.Adopt(t.Context(), AdoptRequest{Branch: "dockhand/bump/adopted-5e5e"})
	require.NoError(t, err)
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	run(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.1")

	names, err := e.LegacyBranchNames(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"dockhand/bump/jq-9c1d", "dockhand/bump/libharbor-4f2a", "dockhand/bump/newport-77aa"}, names, "not the tracked one")

	plans, err := e.PlanLegacy(t.Context())
	require.NoError(t, err)
	require.Len(t, plans, 3)
	jq, libharbor, newport := plans[0], plans[1], plans[2]
	require.Equal(t, LegacySuperseded, jq.Kind)
	require.Equal(t, "master has jq at 1.8.1, where the branch took 1.7.1 to 1.8.0", jq.Detail)
	require.Equal(t, LegacyOnMaster, libharbor.Kind)
	require.Equal(t, "master has its 1 commit, by their changes", libharbor.Detail)
	require.Equal(t, "ada/macports-ports:dockhand/bump/libharbor-4f2a", libharbor.Fork)
	require.Equal(t, LegacyUnfinished, newport.Kind)
	require.Equal(t, "1 commit master hasn't", newport.Detail)

	done, err := e.RemoveLegacy(t.Context(), plans)
	require.NoError(t, err)
	require.True(t, done[1].Done)
	require.False(t, done[0].Done, "a look first")
	require.False(t, done[2].Done, "never")
	require.Empty(t, run(t, f.clone, "branch", "--list", "dockhand/bump/libharbor-4f2a"))
	require.NotEmpty(t, run(t, f.clone, "branch", "--list", "dockhand/bump/jq-9c1d"))
	require.Empty(t, run(t, f.clone, "ls-remote", "fork", "refs/heads/dockhand/bump/libharbor-4f2a"), "and your fork's")
}
