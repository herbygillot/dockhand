package portedit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
)

func gitRelease(commit string) *model.Release {
	return &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.2.4", CurrentVersion: "1.2.3"}, Version: "1.2.4", Tag: "v1.2.4", Commit: commit}
}

// A port fetched with git bumps through its version alone: nothing is
// downloaded, and the evaluated git.branch must land on the resolved tag.
func TestGitFetchedPortBumpsThroughItsBranch(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
fetch.type git
git.url https://example.invalid/fixture.git
git.branch v${version}
`)
	r.Release = gitRelease(strings.Repeat("a", 40))
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Empty(t, *requests, "a git fetch downloads nothing")
	require.Empty(t, result.Downloads)
	require.Len(t, result.Commits, 1)
	require.Equal(t, "fixture: update to 1.2.4", result.Commits[0].Subject)
	after := string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.4")
	require.Contains(t, after, "revision 0")
	require.Contains(t, after, "git.branch v${version}")
	require.Equal(t, "v1.2.4", result.Fidelity[len(result.Fidelity)-1].After.Ports["fixture"].Options["git.branch"])
	require.Contains(t, result.Fidelity[0].ExpectedChanges, "fixture.git.branch -> v1.2.4")
	require.Len(t, result.Coverage, 1)
}

// A literal commit pin moves to the resolved commit; a pin carried any
// other way is refused rather than guessed.
func TestGitFetchedPortMovesALiteralCommitPin(t *testing.T) {
	t.Parallel()
	old, next := strings.Repeat("0", 40), strings.Repeat("a", 40)
	s, r, _ := archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
git.branch `+old+`
`)
	r.Release = gitRelease(next)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Contains(t, after, "git.branch "+next)
	require.NotContains(t, after, old)
	require.Contains(t, result.Fidelity[0].ExpectedChanges, "fixture.git.branch -> "+next)

	s, r, _ = archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
set pin `+old+`
git.branch ${pin}
`)
	r.Release = gitRelease(next)
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrUnsupported)
	require.Contains(t, err.Error(), "no single literal declaration")

	s, r, _ = archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
git.branch `+old+`
`)
	r.Release = gitRelease("")
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrUnsupported)
}

// A git-fetched port whose branch does not follow the version is a
// fidelity failure, and one that mixes an archive into the clone is refused
// before any edit.
func TestGitFetchedPortRefusalsAndFidelity(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
git.branch release-branch
`)
	r.Release = gitRelease(strings.Repeat("a", 40))
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.Contains(t, err.Error(), "git.branch differs from selected tag")

	s, r, _ = archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
git.branch v${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
`)
	r.Release = gitRelease(strings.Repeat("a", 40))
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrUnsupported)
	require.Contains(t, err.Error(), "mixes an archive into the clone")
}

// A git fetch isn't extracted here, so its patches aren't checked before
// the build, which applies them: the result records each as unchecked.
func TestAGitFetchedPortsPatchesAreRecordedUnchecked(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
fetch.type git
git.url https://example.invalid/fixture.git
git.branch v${version}
patchfiles fix.patch
`)
	r.Release = gitRelease(strings.Repeat("a", 40))
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []patchcheck.Result{{Name: "fix.patch", Detail: "a git fetch isn't extracted here; the build applies it"}}, result.Patches)
}
