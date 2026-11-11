package sweep

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/store"
)

// The seams reach every holder of a store or a Program: a write through
// the engine, its check service, its history, its session, the
// repository, and a worktree each appears in the log. A Program built
// somewhere new without the hook, or a service given the store before
// the wrap, fails here, where it would leave an effect unswept (#129).
func TestTheSeamsReachEveryHolder(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	p := newPlan()
	stands := cmdtest.Stands(t)
	stands.GitHook = p.gitHook
	stands.WrapStore = func(s store.Store) store.Store { return recordingStore{Store: s, plan: p} }
	for _, args := range [][]string{{"start", "jq-update"}, {"update", "jq", "-b", "jq-update"}, {"tidy", "-b", "jq-update", "-y"}, {"check", "-b", "jq-update"}} {
		_, errs, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err, "%v: %s", args, errs)
	}
	sites := func(boundary string) []string {
		var found []string
		for _, e := range p.effects() {
			if strings.HasPrefix(e.Boundary, boundary) && !slices.Contains(found, e.Site) {
				found = append(found, e.Site)
			}
		}
		return found
	}
	packages := func(sites []string) []string {
		var found []string
		for _, site := range sites {
			pkg, _, _ := strings.Cut(site, ".")
			if !slices.Contains(found, pkg) {
				found = append(found, pkg)
			}
		}
		return found
	}
	stores := sites("store.")
	for _, holder := range []string{"engine", "checks", "history", "coord"} {
		require.Contains(t, packages(stores), holder, "a store write through %s: %v", holder, stores)
	}
	gits := sites("git ")
	require.NotEmpty(t, gits)
	worktree := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	inWorktree := slices.ContainsFunc(p.effects(), func(e effect) bool {
		return strings.HasPrefix(e.Boundary, "git ") && strings.Contains(e.Detail, "worktree") || strings.Contains(e.Detail, worktree)
	})
	require.True(t, inWorktree || slices.Contains(packages(gits), "history"), "a git write in the worktree: %v", gits)
	t.Logf("store sites: %v", stores)
	t.Logf("git sites: %v", gits)
}
