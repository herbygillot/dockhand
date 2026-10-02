package upstream_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// noVersions stands for MacPorts' version comparison, which a port that
// tracks a branch doesn't ask.
type noVersions struct{}

func (noVersions) SelectVersion(context.Context, string, string, []macports.VersionCandidate) (macports.VersionSelection, error) {
	return macports.VersionSelection{}, nil
}

func (noVersions) ExtractVersions(context.Context, string, string, bool) ([]string, error) {
	return nil, nil
}

// A port that tracks a branch is current while the branch names the commit
// it pins, abbreviated or not, and Moved once the branch names a newer
// one, with the branch and its commit: Base's git livecheck, 19 of the
// person's ports read as unsupported (batch 30).
func TestAPortTrackingABranchSaysWhenItMoved(t *testing.T) {
	repository := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repository, "-c", "user.name=T", "-c", "user.email=t@example.org"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "one")
	pinned := git("rev-parse", "HEAD")
	port := macports.PortInfo{Name: "goat", Version: "20220814", Options: map[string]string{
		"livecheck.type": "git", "livecheck.url": repository, "livecheck.version": pinned[:7], "livecheck.branch": "", "distname": "goat-20220814",
	}}
	service := upstream.Service{EvaluateVersion: identityVersion, Versions: noVersions{}}

	current, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Current, current.Assessment)
	require.Equal(t, &upstream.Head{Branch: "HEAD", Commit: pinned}, current.Head)

	git("commit", "-q", "--allow-empty", "-m", "two")
	newer := git("rev-parse", "HEAD")
	moved, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Moved, moved.Assessment)
	require.Equal(t, newer[:12], moved.CandidateVersion)
	require.Equal(t, &upstream.Head{Branch: "HEAD", Commit: newer}, moved.Head)
}
