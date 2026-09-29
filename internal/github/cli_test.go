package github_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/github"
)

// The GitHub CLI is run by its documented commands: gh api for who it's
// signed in as, and gh pr ready, saying what gh said when it fails (D8).
func TestTheGitHubCLIRunsItsDocumentedCommands(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := func(name, login string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho \"$*\" >> '"+log+"'\ncase \"$1 $2 $3\" in\n"+
			"  'api --hostname github.com') echo '"+login+"' ;;\n  'pr ready 7') ;;\n  *) echo 'no pull requests found for 8' >&2; exit 1 ;;\nesac\n"), 0o755))
		return path
	}
	cli := github.CLI{Path: script("gh", "ada")}
	login, err := cli.Login(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ada", login)
	ref := forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 7}
	require.NoError(t, cli.MarkReady(t.Context(), ref))
	ref.Number = 8
	require.EqualError(t, cli.MarkReady(t.Context(), ref), "gh pr ready: no pull requests found for 8")
	logged, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "api --hostname github.com user --jq .login\npr ready 7 --repo macports/macports-ports\npr ready 8 --repo macports/macports-ports\n", string(logged))

	_, err = github.CLI{Path: script("silent", "")}.Login(t.Context())
	require.EqualError(t, err, "the GitHub CLI named no account it's signed in as")
	t.Setenv("PATH", filepath.Join(dir, "nothing here"))
	_, err = github.CLI{}.Login(t.Context())
	require.EqualError(t, err, "the GitHub CLI isn't installed")
}
