package engine

// The worker audit: what the provider says it is running, minus what
// this repository's notes account for. It had no coverage at all while
// it ran tart directly — nothing could stand in for the backend — so
// every case here is a first, including the two the field cares about
// most: a kept failure's environment is accounted for, and this
// checkout's own workers are not labelled as somebody else's.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// keptNote records a failure holding its environment — a handle with no
// running job behind it, which is the shape the audit most easily
// misreads.
func keptNote(t *testing.T, repo *git.Repo, sha, handle string) {
	t.Helper()
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: record.Failed, Handle: handle,
		Detail: "Failed to build jq: boom"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
}

func TestOrphansSkipsWhatTheNotesAccountFor(t *testing.T) {
	repo, sha := engineRepo(t)
	runningNote(t, repo, sha, "dockhand-worker-running")
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "dockhand-worker-running"},
		{Name: "dockhand-worker-loose"},
	}}

	got := testState(t, repo, fake).Orphans(context.Background(), repo)
	assert.Equal(t, []render.Orphan{{Name: "dockhand-worker-loose"}}, got,
		"a running job's worker is this repository's, and only the rest are orphans")
}

// The regression the two-name tracked set exists for: a failure keeps
// its environment, and the note carries it as a handle with no job. An
// audit reading only job IDs reports every kept failure as an orphan
// and tells the user to delete their own debug environment.
func TestOrphansSkipsAKeptFailuresEnvironment(t *testing.T) {
	repo, sha := engineRepo(t)
	keptNote(t, repo, sha, "dockhand-worker-kept")
	fake := &verifytest.Fake{Live: []verify.Worker{{Name: "dockhand-worker-kept"}}}

	assert.Empty(t, testState(t, repo, fake).Orphans(context.Background(), repo),
		"a kept failure's environment is accounted for by its handle")
}

func TestOrphansNamesTheOwningCheckoutButNotThisOne(t *testing.T) {
	repo, _ := engineRepo(t)
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "dockhand-worker-elsewhere", Owner: "/elsewhere/ports"},
		{Name: "dockhand-worker-ours", Owner: repo.Root},
		{Name: "dockhand-worker-nameless"},
	}}

	got := testState(t, repo, fake).Orphans(context.Background(), repo)
	assert.Equal(t, []render.Orphan{
		{Name: "dockhand-worker-elsewhere", Owner: "/elsewhere/ports"},
		{Name: "dockhand-worker-ours"},
		{Name: "dockhand-worker-nameless"},
	}, got, "an owner is carried only when it points somewhere else")
}

// The machine the audit exists for: tart is installed and its base
// images are gone, so nothing can be verified and the workers cloned
// from those bases are still holding slots. Composing a verifier there
// fails, and the audit must not take that failure for an answer — it
// asks the backend, which needs no base to list what it is running.
func TestOrphansStillCountOnAMachineWithNoBaseImages(t *testing.T) {
	repo, sha := engineRepo(t)
	runningNote(t, repo, sha, "dockhand-worker-running")
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "dockhand-worker-running"},
		{Name: "dockhand-worker-loose"},
	}}

	e := testState(t, repo, fake)
	e.Verifier = func(context.Context) (verify.Verifier, error) {
		return nil, fmt.Errorf("%w: no base images; run `dockhand provision tart --macos <release>` first",
			verify.ErrNoEnvironment)
	}

	assert.Equal(t, []render.Orphan{{Name: "dockhand-worker-loose"}},
		e.Orphans(context.Background(), repo),
		"bases gone is not workers gone, and the slot is spent either way")
}

// A provider that cannot list its workers has told the audit nothing
// about them, which is not the same as telling it there are none — but
// it is the same rendering, because there is no honest sentence about
// workers nobody could count.
func TestOrphansAreSilentWhenNothingCanAnswer(t *testing.T) {
	repo, sha := engineRepo(t)
	runningNote(t, repo, sha, "dockhand-worker-running")
	live := []verify.Worker{{Name: "dockhand-worker-loose"}}

	for _, c := range []struct {
		name string
		e    *Engine
	}{
		{"no backend composed", testState(t, repo, nil)},
		{"no lister wired at all", func() *Engine {
			e := testState(t, repo, &verifytest.Fake{Live: live})
			e.Lister = nil
			return e
		}()},
		{"a backend without the capability", func() *Engine {
			e := testState(t, repo, nil)
			e.Lister = func(context.Context) (verify.Verifier, error) {
				return verifytest.Incapable{Fake: &verifytest.Fake{Live: live}}, nil
			}
			return e
		}()},
		{"a listing that failed", testState(t, repo,
			&verifytest.Fake{Live: live, WorkersErr: errors.New("tart: no such command")})},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Empty(t, c.e.Orphans(context.Background(), repo))
		})
	}
}
