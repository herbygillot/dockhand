package model_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// A branch name is held to one rule: what model stores, git takes, and Git
// itself, git check-ref-format --branch, agree on each, where
// dockhand/a.lock/b passed model's check and Git refused it (the
// architecture review's H8).
func TestABranchNameIsHeldToGitsRule(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"dockhand/jq-1.8.1", "dockhand/jq-4k2p", "dockhand/a/b", "dockhand/py313-x",
		"dockhand/a.lock/b", "dockhand/jq.lock", "dockhand/.hidden", "dockhand/a/.b", "dockhand//jq",
		"dockhand/jq/", "/dockhand/jq", "dockhand/jq.", "dockhand/a..b", "dockhand/a@{b",
		"dockhand/a b", "dockhand/a~b", "dockhand/a^b", "dockhand/a:b", "dockhand/a?b", "dockhand/a*b", "dockhand/a[b", "dockhand/a\\b",
		"dockhand/a\x7fb", "dockhand/a\tb",
	} {
		t.Run(name, func(t *testing.T) {
			byGit := exec.Command("git", "check-ref-format", "--branch", name).Run() == nil
			b := model.Branch{ID: "b1", Repository: "r1", Name: name, Base: "base", Worktree: "/w/jq", Managed: true, State: model.BranchOpen, CreatedAt: time.Now()}
			require.Equal(t, byGit, b.Validate() == nil, "model, where Git says %v", byGit)
			require.Equal(t, byGit, git.ValidBranchName(name), "git.ValidBranchName, where Git says %v", byGit)
		})
	}
}
