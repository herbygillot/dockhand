package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Model reaches its private name rule through Branch.Validate; the shared
// package test holds it together with git.ValidBranchName.
func TestModelBranchNamesMatchGitRule(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"dockhand/jq-1.8.1", "dockhand/jq-4k2p", "dockhand/a/b", "dockhand/py313-x",
		"-dockhand/jq", "dockhand/invalid\xff",
		"dockhand/a.lock/b", "dockhand/jq.lock", "dockhand/.hidden", "dockhand/a/.b", "dockhand//jq",
		"dockhand/jq/", "/dockhand/jq", "dockhand/jq.", "dockhand/a..b", "dockhand/a@{b",
		"dockhand/a b", "dockhand/a~b", "dockhand/a^b", "dockhand/a:b", "dockhand/a?b", "dockhand/a*b", "dockhand/a[b", "dockhand/a\\b",
		"dockhand/a\x7fb", "dockhand/a\tb",
	} {
		t.Run(name, func(t *testing.T) {
			b := model.Branch{ID: "b1", Repository: "r1", Name: name, Base: "base", Worktree: "/w/jq", Managed: true, State: model.BranchOpen, CreatedAt: time.Now()}
			require.Equal(t, git.ValidBranchName(name), b.Validate() == nil, "model name validation differs from git.ValidBranchName")
		})
	}
}
