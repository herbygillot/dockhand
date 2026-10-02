package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// commitNaming adds an empty commit to the branch whose Generated-By
// names a build, and returns its name.
func commitNaming(t *testing.T, dir, subject, build string) string {
	t.Helper()
	testsupport.Git(t, dir, "commit", "-q", "--allow-empty", "-m", fmt.Sprintf("%s\n\nGenerated-By: Dockhand %s (https://github.com/herbygillot/dockhand)", subject, build))
	return testsupport.Git(t, dir, "rev-parse", "HEAD")
}

// A Generated-By naming a build dockhand's repository on GitHub doesn't
// have names one nobody else can find, as a build of uncommitted source
// does: the hugo exercise's tidy named a build of a commit six ahead of
// origin, which a rebase then left unreachable (finding 1). Submit's plan
// asks GitHub about each build its commits name, once: an older build's
// commit by the twelve characters its tag has, and a release by its tag.
// It says those GitHub doesn't have, and one that recorded no commit; a
// build of uncommitted source is ModifiedBuilds', and isn't asked about.
// None of it blocks the submission, or holds one nobody looks over.
func TestSubmitSaysABuildGitHubDoesntHave(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	fake.Dockhand.Commits = []string{"2bbcfdb76480" + strings.Repeat("a", 28)}
	fake.Dockhand.Tags = []string{"v0.0.0-20260924.0"}
	devel := testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")
	require.Contains(t, testsupport.Git(t, branch.Worktree, "log", "-1", "--format=%B"), "Generated-By: Dockhand devel (", "a test's build records no revision")
	unpushed := "v0.0.0-20260924.0.0.20260928190000-14320eb7c0de"
	first := commitNaming(t, branch.Worktree, "jq: a first look", unpushed)
	commitNaming(t, branch.Worktree, "jq: a pushed build", "v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480")
	dirty := commitNaming(t, branch.Worktree, "jq: a dirty build", "v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480+dirty")
	second := commitNaming(t, branch.Worktree, "jq: a second look", unpushed)
	commitNaming(t, branch.Worktree, "jq: a release", "v0.0.0-20260924.0")
	local := commitNaming(t, branch.Worktree, "jq: a local tag", "v0.3.0")

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1"})
	require.NoError(t, err)
	require.Equal(t, []UnfoundBuild{
		{Build: "devel", Commits: []string{devel}},
		{Build: unpushed, Commits: []string{first, second}, Source: buildinfo.Source{Commit: "14320eb7c0de"}},
		{Build: "v0.3.0", Commits: []string{local}, Source: buildinfo.Source{Release: "v0.3.0"}},
	}, plan.UnfoundBuilds)
	require.Empty(t, plan.BuildsProblem)
	require.Equal(t, []string{"14320eb7c0de", "2bbcfdb76480", "v0.0.0-20260924.0", "v0.3.0"}, fake.Dockhand.Asked, "each build once, by what finds it")
	require.Equal(t, "herbygillot/dockhand", fake.Dockhand.Name())
	require.Equal(t, []string{dirty}, plan.ModifiedBuilds)
	require.Empty(t, plan.Blocking)
	for _, held := range plan.held() {
		for _, said := range []string{"Generated-By", unpushed, "v0.3.0", short(model.ObjectID(first)), short(model.ObjectID(local))} {
			require.NotContains(t, held, said, "nothing holds a submission nobody looks over")
		}
	}

	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.True(t, submitted.Created)
}

// Where GitHub can't be asked, for want of a login, the network, or its
// rate limit, that is said once and the builds after aren't asked; a
// build that recorded no commit is still said, needing no answer. The
// submission goes ahead.
func TestSubmitSaysOnceThatGitHubCouldntBeAskedAboutItsBuilds(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	fake.Dockhand.Err = &forge.RateLimitError{RetryAt: time.Now().Add(time.Hour), Err: errors.New("github: API rate limit exceeded")}
	commitNaming(t, branch.Worktree, "jq: a first look", "devel+14320eb7c0de")
	commitNaming(t, branch.Worktree, "jq: a second look", "v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480")

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1"})
	require.NoError(t, err)
	require.Equal(t, "github: API rate limit exceeded", plan.BuildsProblem)
	require.Equal(t, []string{"14320eb7c0de"}, fake.Dockhand.Asked)
	require.Len(t, plan.UnfoundBuilds, 1)
	require.Equal(t, "devel", plan.UnfoundBuilds[0].Build)
	require.Empty(t, plan.Blocking)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
}
