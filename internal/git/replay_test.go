package git_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
)

// Replay does what git rebase does to a branch's commits, without a
// checkout or a ref: each is replayed onto master with its own author,
// date, and message; one master already has is dropped; one empty to
// begin with is kept; and a conflict, or a merge, is refused with nothing
// moved.
func TestReplayRebasesWithoutACheckout(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(text), 0o644))
	}
	commit := func(message string, args ...string) string {
		gitIn(t, root, append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.org", "commit", "-q", "--date=2026-09-20T10:00:00+02:00", "-m", message}, args...)...)
		return gitIn(t, root, "rev-parse", "HEAD")
	}
	write("a/Portfile", "a\n")
	write("b/Portfile", "b\n")
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", "-A")
	base := commit("base")
	gitIn(t, root, "switch", "-q", "-c", "branch")
	write("a/Portfile", "a2\n")
	commit("a: update", "-a")
	write("c/Portfile", "c\n")
	gitIn(t, root, "add", "c/Portfile")
	commit("c: new port")
	commit("a note", "--allow-empty")
	write("b/Portfile", "b-up\n")
	head := commit("b: update", "-a")
	gitIn(t, root, "switch", "-q", "master")
	write("b/Portfile", "b-up\n")
	write("d/Portfile", "d\n")
	gitIn(t, root, "add", "-A")
	master := commit("b: update, d: new port")

	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	committer := git.Signature{Name: "Dockhand Test", Email: "test@example.org", When: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	replayed, err := repo.Replay(t.Context(), master, base, head, committer)
	require.NoError(t, err)
	require.Equal(t, head, gitIn(t, root, "rev-parse", "branch"), "no ref moves")
	require.Equal(t, "a note\nc: new port\na: update", gitIn(t, root, "log", "--format=%s", master+".."+replayed), "b's update, which master has, is dropped; the empty note is kept")
	require.Equal(t, "Ada <ada@example.org> 2026-09-20T08:00:00Z|Dockhand Test", gitIn(t, root, "log", "-1", "--format=%an <%ae> %aI|%cn", replayed+"~2"), "each keeps its author and date")
	require.Equal(t, master, gitIn(t, root, "rev-parse", replayed+"~3"))

	// git rebase makes the same tree.
	gitIn(t, root, "switch", "-q", "branch")
	gitIn(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.org", "rebase", "-q", "master")
	require.Equal(t, gitIn(t, root, "rev-parse", "HEAD^{tree}"), gitIn(t, root, "rev-parse", replayed+"^{tree}"))

	gitIn(t, root, "switch", "-q", "-c", "conflicting", base)
	write("b/Portfile", "b-mine\n")
	conflicting := commit("b: mine", "-a")
	_, err = repo.Replay(t.Context(), master, base, conflicting, committer)
	require.ErrorIs(t, err, git.ErrRebaseConflict)
	require.ErrorContains(t, err, "in b/Portfile")

	gitIn(t, root, "switch", "-q", "-c", "merging", base)
	gitIn(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.org", "merge", "-q", "--no-ff", "-m", "merge", "branch")
	_, err = repo.Replay(t.Context(), master, base, gitIn(t, root, "rev-parse", "HEAD"), committer)
	require.ErrorContains(t, err, "is a merge commit")
}
