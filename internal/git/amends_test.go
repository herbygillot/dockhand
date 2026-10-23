package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// A commit amends another made on the same parent, or one whose changes
// it holds; an empty commit is amended only on its parent, since no tree
// holds anything of it (the rc4 full stage, D-S1).
func TestAmendsIsTheirCommitAmended(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testsupport.Git(t, root, "init", "-q", "-b", "master")
	commit := func(file, text, message string, args ...string) string {
		if file != "" {
			require.NoError(t, os.WriteFile(filepath.Join(root, file), []byte(text), 0o644))
			testsupport.Git(t, root, "add", file)
		}
		testsupport.Git(t, root, append([]string{"-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", message}, args...)...)
		return strings.TrimSpace(testsupport.Git(t, root, "rev-parse", "HEAD"))
	}
	base := commit("Portfile", "version 1\n", "base")
	ours := commit("Portfile", "version 2\n", "ours")
	theirs := commit("README", "theirs\n", "theirs")
	empty := commit("", "", "empty", "--allow-empty")
	testsupport.Git(t, root, "reset", "-q", "--hard", theirs)
	amended := commit("Portfile", "version 3\n", "amended", "--amend")
	testsupport.Git(t, root, "reset", "-q", "--hard", base)
	carried := commit("README", "theirs\n", "carried")
	testsupport.Git(t, root, "reset", "-q", "--hard", base)
	other := commit("Portfile", "version 4\n", "other")
	repo, err := git.Open(t.Context(), root, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	for _, c := range []struct {
		commit, of string
		amends     bool
		why        string
	}{
		{amended, theirs, true, "on their parent"},
		{carried, theirs, true, "their change in its tree"},
		{other, theirs, false, "neither"},
		{other, empty, false, "an empty commit, elsewhere"},
		{ours, ours, true, "itself"},
	} {
		got, err := repo.Amends(t.Context(), c.commit, c.of)
		require.NoError(t, err)
		require.Equal(t, c.amends, got, c.why)
	}
}
