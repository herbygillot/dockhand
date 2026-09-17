package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func TestCommitTreesBatchesAndValidatesImmutableInputs(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("git")
	require.NoError(t, err)
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			command := exec.CommandContext(t.Context(), executable, "init", "--quiet", "--object-format="+format, root)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			repo, err := git.Open(t.Context(), root, executable)
			require.NoError(t, err)
			signature := git.Signature{Name: "Test", Email: "test@example.invalid", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			expected := make(map[string]string)
			var commits []string
			var blob string
			for _, content := range []string{"version 1", "version 2"} {
				blob, err = repo.WriteBlob(t.Context(), []byte(content))
				require.NoError(t, err)
				tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
				require.NoError(t, err)
				commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: content, Author: signature, Committer: signature})
				require.NoError(t, err)
				expected[commit] = tree
				commits = append(commits, commit)
			}
			log := filepath.Join(root, "calls")
			wrapper := filepath.Join(root, "git-wrapper")
			script := "#!/bin/sh\nprintf 'call\n' >> " + quote(log) + "\nexec " + quote(executable) + " \"$@\"\n"
			require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o700))
			repo.Executable = wrapper
			trees, err := repo.CommitTrees(t.Context(), []string{commits[1], commits[0], commits[1]})
			require.NoError(t, err)
			require.Equal(t, expected, trees)
			calls, err := os.ReadFile(log)
			require.NoError(t, err)
			require.Equal(t, "call\n", string(calls), "all lookups must share one Git process")
			for _, id := range []string{blob, expected[commits[0]], strings.Repeat("a", len(commits[0])), "HEAD", "1234567", commits[0] + "\nHEAD"} {
				trees, err := repo.CommitTrees(t.Context(), []string{commits[0], id})
				require.Error(t, err)
				require.Nil(t, trees, "a failed lookup must not return partial results")
			}
			repo.Executable = filepath.Join(root, "missing-git")
			trees, err = repo.CommitTrees(t.Context(), nil)
			require.NoError(t, err)
			require.Empty(t, trees)
		})
	}
}

func TestCommitTreesRejectsMalformedBatchResponses(t *testing.T) {
	t.Parallel()
	commit, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
	root := t.TempDir()
	wrapper := filepath.Join(root, "git-wrapper")
	repo := git.Repository{Root: root, Executable: wrapper}
	for _, output := range []string{
		commit + " commit\n",
		tree + " commit\n" + tree + " tree\n",
		commit + " tag\n" + tree + " tree\n",
		commit + " commit\nmissing tree\n",
		commit + " commit\n" + tree + " blob\n",
	} {
		require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' "+quote(output)+"\n"), 0o700))
		result, err := repo.CommitTrees(t.Context(), []string{commit})
		require.Error(t, err)
		require.Nil(t, result)
	}
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
