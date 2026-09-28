package github_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/github"
)

// A remote names a repository exactly, in the forms Git reaches GitHub
// by; nothing else is taken for one. (The private-helper review of
// 2026-09-28's table, and the code-organization review's finding 13.)
func TestARemoteNamesItsRepositoryExactly(t *testing.T) {
	for remote, name := range map[string]string{
		"git@github.com:Owner/ports.git":          "Owner/ports",
		"https://github.com/Owner/ports":          "Owner/ports",
		"https://github.com/Owner/ports.git/":     "Owner/ports",
		"ssh://git@github.com/Owner/ports.git":    "Owner/ports",
		"ssh://git@github.com:22/Owner/ports.git": "Owner/ports",
	} {
		got, err := github.RemoteRepository(remote)
		require.NoError(t, err, remote)
		require.Equal(t, name, got, remote)
	}
	for _, remote := range []string{
		"https://github.com.evil/owner/ports",
		"https://token@github.com/owner/ports",
		"https://github.com/owner/ports?x=y",
		"https://github.com/owner/ports/tree/master",
		"git@github.com:owner/../ports",
		"http://github.com/owner/ports",
		"git://github.com/owner/ports",
		"ssh://ada@github.com/owner/ports",
		"file:///repo",
	} {
		_, err := github.RemoteRepository(remote)
		require.Error(t, err, remote)
	}
}

// A page's address names the repository it belongs to, however a person
// copied it.
func TestAPageNamesItsRepository(t *testing.T) {
	for _, address := range []string{"https://github.com/rift-dev/rift", "github.com/rift-dev/rift", "https://www.github.com/rift-dev/rift.git", "https://github.com/rift-dev/rift/releases/tag/v0.4.2"} {
		name, err := github.PageRepository(address)
		require.NoError(t, err, address)
		require.Equal(t, "rift-dev/rift", name, address)
	}
	_, err := github.PageRepository("https://gitlab.com/rift-dev/rift")
	require.ErrorIs(t, err, github.ErrNotGitHub)
	for _, address := range []string{"https://github.com/rift-dev", "https://github.com/rift dev/rift"} {
		_, err = github.PageRepository(address)
		require.ErrorContains(t, err, "names no repository", address)
		require.NotErrorIs(t, err, github.ErrNotGitHub, address)
	}
}

func TestGitHubsAddresses(t *testing.T) {
	require.Equal(t, "git@github.com:ada/macports-ports.git", github.Remote("ada/macports-ports", true))
	require.Equal(t, "https://github.com/ada/macports-ports.git", github.Remote("ada/macports-ports", false))
	require.Equal(t, "https://github.com/macports/macports-ports/pull/35001", github.PullRequestURL("macports/macports-ports", 35001))
	require.Equal(t, "https://github.com/macports/macports-ports/pull/35001/checks", github.PullRequestChecksURL("macports/macports-ports", 35001))
	for _, address := range []string{github.Remote("ada/macports-ports", true), github.Remote("ada/macports-ports", false)} {
		name, err := github.RemoteRepository(address)
		require.NoError(t, err, address)
		require.Equal(t, "ada/macports-ports", name, "a remote reads back as its repository")
	}
}
