package upstream_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gitprogram "github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// pinnedPort pins its GitHub source to commit and tracks branch head at
// url for newer ones, as wezterm does.
func pinnedPort(version, commit, url string) macports.PortInfo {
	port := githubPort()
	port.Version = version
	port.Options["github.version"], port.Options["git.branch"] = commit, commit
	port.Options["github.tag_prefix"], port.Options["github.tag_suffix"], port.Options["github.tarball_from"] = "", "", "archive"
	port.Options["livecheck.type"], port.Options["livecheck.url"], port.Options["livecheck.regex"] = "git", url, ""
	port.Options["livecheck.version"], port.Options["livecheck.branch"] = commit, ""
	return port
}

// A commit pin's update refuses what it can't move to: a commit the
// repository hasn't, one abbreviated, the commit it pins, a version the
// port's scheme can't give a commit, the version it's at, and a forge that
// can't date a commit where the version must come from its date, each
// saying what to name instead, where no test reached them (the test
// analysis's step b).
func TestACommitPinRefusesWhatItCantMoveTo(t *testing.T) {
	t.Parallel()
	pinned, newer := strings.Repeat("a", 40), strings.Repeat("b", 40)
	was, now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	c := &catalog{commits: map[string]bool{pinned: true, newer: true}, dates: map[string]time.Time{pinned: was, newer: now}}
	service := serviceWithCatalog(c)
	port := pinnedPort("20261005-aaaaaa", pinned, "https://github.com/owner/project.git")

	release, err := service.Resolve(t.Context(), port, newer)
	require.NoError(t, err)
	require.Equal(t, "20261010-bbbbbb", release.Version)
	require.True(t, release.Pinned)
	require.NoError(t, service.Check(t.Context(), port, release))

	_, err = service.Resolve(t.Context(), port, strings.Repeat("c", 40))
	require.ErrorContains(t, err, "upstream: owner/project has no commit "+strings.Repeat("c", 40))
	_, err = service.Resolve(t.Context(), port, "bbbbbbb")
	require.ErrorContains(t, err, "upstream: name fixture's commit whole, its 40 characters: bbbbbbb is abbreviated")
	_, err = service.Resolve(t.Context(), port, pinned)
	require.ErrorContains(t, err, "upstream: fixture is already at aaaaaaa, the commit it pins")
	_, err = service.Resolve(t.Context(), pinnedPort("1.0", pinned, "https://github.com/owner/project.git"), newer)
	require.ErrorContains(t, err, "upstream: fixture's version 1.0 doesn't read as its commit's date and hash; name the version: dockhand update fixture <version>")
	undated := serviceWithCatalog(tagFunc(nil))
	_, err = undated.Resolve(t.Context(), port, newer)
	require.ErrorContains(t, err, "upstream: fixture's forge can't say when a commit was made; name the version: dockhand update fixture <version>")

	// A named version takes the branch's head, read with git: the head
	// the port pins already, and the version it's at, are refused.
	head := gitRepository(t)
	local := pinnedPort("2026.10.05", pinned, head.dir)
	local.Options["livecheck.version"] = pinned
	service.Git = gitprogram.Program{Env: os.Environ()}
	_, err = service.Resolve(t.Context(), pinnedPort("20261005-aaaaaa", head.commit, head.dir), "2026.10.11")
	require.ErrorContains(t, err, "the commit it pins")
	_, err = service.Resolve(t.Context(), local, "2026.10.05")
	require.ErrorContains(t, err, "upstream: fixture is already at version 2026.10.05")
	_, err = service.Resolve(t.Context(), pinnedPort("20261005-aaaaaa", pinned, head.dir+"-gone"), "2026.10.11")
	require.ErrorContains(t, err, "-gone's HEAD with git")
}

// A saved pinned release is checked against the Portfile and the forge:
// one that no longer matches the pin is refused, and one whose commit the
// repository lost is the source changing.
func TestASavedPinnedReleaseIsChecked(t *testing.T) {
	t.Parallel()
	pinned, newer := strings.Repeat("a", 40), strings.Repeat("b", 40)
	c := &catalog{commits: map[string]bool{pinned: true, newer: true}, dates: map[string]time.Time{pinned: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), newer: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}}
	service := serviceWithCatalog(c)
	port := pinnedPort("20261005-aaaaaa", pinned, "https://github.com/owner/project.git")
	release, err := service.Resolve(t.Context(), port, newer)
	require.NoError(t, err)

	moved := release
	moved.Tag = strings.Repeat("d", 40)
	require.ErrorContains(t, service.Check(t.Context(), port, moved), "upstream: pinned release does not match the Portfile's commit pin")
	require.ErrorContains(t, service.Check(t.Context(), githubPort(), release), "upstream: pinned release does not match the Portfile's commit pin", "a port that no longer pins")

	delete(c.commits, newer)
	err = service.Check(t.Context(), port, release)
	require.ErrorIs(t, err, upstream.ErrSourceChanged)
	require.ErrorContains(t, err, "owner/project has no commit "+newer)
}

type localRepository struct{ dir, commit string }

// gitRepository is a repository on disk with one commit on its HEAD.
func gitRepository(t *testing.T) localRepository {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.org"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "one")
	return localRepository{dir: dir, commit: run("rev-parse", "HEAD")}
}
