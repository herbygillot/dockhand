package changeset_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func correctionGit(t *testing.T, repo *git.Repository, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo.Root}, args...)...).CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}
func TestAmendCheckedOutBranchProtectsIndexAndWorkingEdits(t *testing.T) {
	repo, commit := fixture(t)
	base := commit("base", map[string]string{"Portfile": "version 1\n"})
	previous := commit("port: update", map[string]string{"Portfile": "version 2\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "Portfile"), []byte("version 3\n"), 0600))
	snapshot, err := changeset.CaptureCheckout(t.Context(), repo)
	require.NoError(t, err)
	sig := git.Signature{Name: "Test", Email: "test@example.invalid", When: time.Now()}
	candidate, err := changeset.Correct(t.Context(), repo, snapshot, base.Commit, base.Commit, "port: update", sig)
	require.NoError(t, err)
	replace := func() error {
		return repo.WithBranchLock(t.Context(), "main", func(ctx context.Context) error {
			return repo.ReplaceContribution(ctx, "main", string(previous.Commit), string(candidate.Commit), string(candidate.Tree))
		})
	}
	require.ErrorContains(t, replace(), "unstaged")
	correctionGit(t, repo, "add", "Portfile")
	require.NoError(t, replace())
	require.Empty(t, correctionGit(t, repo, "status", "--porcelain"))
	require.Equal(t, string(candidate.Commit), correctionGit(t, repo, "rev-parse", "HEAD"))
	require.Equal(t, string(base.Commit), correctionGit(t, repo, "rev-parse", "HEAD^"))
}
func TestRebaseSquashesWithoutChangingCheckoutAndPreservesConflicts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "conflict"}[conflict], func(t *testing.T) {
			repo, commit := fixture(t)
			base := commit("base", map[string]string{"Portfile": "version 1\n"})
			contribution := commit("port: update", map[string]string{"Portfile": "version 2\n"})
			snapshot, err := changeset.CaptureBranch(t.Context(), repo, "main")
			require.NoError(t, err)
			correctionGit(t, repo, "checkout", "--detach", string(base.Commit))
			contents := map[string]string{"unrelated": "upstream\n"}
			if conflict {
				contents = map[string]string{"Portfile": "version 9\n"}
			}
			// The fixture commit helper reads main, so obtain the detached commit explicitly.
			commit("upstream", contents)
			next := record.ObjectID(correctionGit(t, repo, "rev-parse", "HEAD"))
			sig := git.Signature{Name: "Test", Email: "test@example.invalid", When: time.Now()}
			candidate, err := changeset.Correct(t.Context(), repo, snapshot, base.Commit, next, "port: update", sig)
			require.Equal(t, string(next), correctionGit(t, repo, "rev-parse", "HEAD"))
			require.Equal(t, string(contribution.Commit), correctionGit(t, repo, "rev-parse", "main"))
			if conflict {
				require.ErrorContains(t, err, "retained workspace")
				listing := correctionGit(t, repo, "worktree", "list", "--porcelain")
				for _, line := range strings.Split(listing, "\n") {
					if strings.HasPrefix(line, "worktree ") && strings.Contains(line, "dockhand-rebase-") {
						root := strings.TrimPrefix(line, "worktree ")
						require.FileExists(t, filepath.Join(root, "Portfile"))
						correctionGit(t, repo, "worktree", "remove", "--force", root)
						os.Remove(filepath.Dir(root))
					}
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, next, candidate.Base)
				require.Equal(t, string(next), correctionGit(t, repo, "rev-parse", string(candidate.Commit)+"^"))
				require.Equal(t, "version 2", correctionGit(t, repo, "show", string(candidate.Commit)+":Portfile"))
				require.Equal(t, "upstream", correctionGit(t, repo, "show", string(candidate.Commit)+":unrelated"))
			}
		})
	}
}
func TestCorrectionRefusesOtherWorktree(t *testing.T) {
	repo, commit := fixture(t)
	base := commit("base", map[string]string{"Portfile": "version 1\n"})
	snapshot, err := changeset.CaptureBranch(t.Context(), repo, "main")
	require.NoError(t, err)
	candidate, err := changeset.Correct(t.Context(), repo, snapshot, base.Commit, base.Commit, "new", git.Signature{Name: "Test", Email: "t@example.invalid", When: time.Now()})
	require.NoError(t, err)
	correctionGit(t, repo, "checkout", "--detach")
	root := filepath.Join(t.TempDir(), "other")
	correctionGit(t, repo, "worktree", "add", root, "main")
	require.ErrorContains(t, repo.ReplaceContribution(t.Context(), "main", string(base.Commit), string(candidate.Commit), string(candidate.Tree)), "checked out at")
}
