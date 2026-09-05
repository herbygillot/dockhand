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
	n := mintedNote(t, repo, sha)
	started(&n, "Testos", "", record.Run{State: record.Failed, Detail: "Failed to build jq: boom"})
	n.Jobs["Testos"] = record.JobRecord{Handle: handle}
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

// The control the release order is paid for with. ReleaseJob puts the
// flag down before the provider is asked, on the argument that a leak
// can be found and a double release cannot be undone — and that
// argument only holds if something finds the leak. A guest whose note
// says it went back but whose worker is still standing is exactly what
// a crash in that window, or a release the provider refused, leaves
// behind, and it must read as an orphan.
func TestOrphansReportAGuestTheNoteSaysWentBackButDidNot(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := mintedNote(t, repo, sha)
	started(&n, "Testos", "dockhand-worker-leaked", record.Run{State: record.Passed})
	job := n.Jobs["Testos"]
	job.Released = true
	n.Jobs["Testos"] = job
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	fake := &verifytest.Fake{Live: []verify.Worker{{Name: "dockhand-worker-leaked"}}}

	assert.Equal(t, []render.Orphan{{Name: "dockhand-worker-leaked"}},
		testState(t, repo, fake).Orphans(ctx, repo),
		"a released job accounts for nothing; the worker outliving it is the leak")
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

// `cycle --reclaim-orphans` (D27): the untracked workers this checkout
// may claim go back through the backend's Release, keyed by the job the
// backend named for each — never by a name the kernel guessed at.
// Another checkout's worker is left standing and its checkout is named.
func TestReclaimOrphansFreesOnlyWhatThisCheckoutMayClaim(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	runningNote(t, repo, sha, "dockhand-worker-running")
	job := func(name string) verify.Job { return verify.Job{Provider: "fake", ID: name} }
	fake := &verifytest.Fake{Live: []verify.Worker{
		{Name: "dockhand-worker-running", Job: job("dockhand-worker-running")},
		{Name: "dockhand-worker-loose", Job: job("dockhand-worker-loose")},
		{Name: "dockhand-worker-ours", Owner: repo.Root, Job: job("dockhand-worker-ours")},
		{Name: "dockhand-worker-elsewhere", Owner: "/elsewhere/ports", Job: job("dockhand-worker-elsewhere")},
		{Name: "dockhand-worker-jobless"},
	}}

	said := testState(t, repo, fake).ReclaimOrphans(ctx, repo)

	assert.Equal(t, []string{"dockhand-worker-loose", "dockhand-worker-ours"}, fake.Released,
		"the unattributed worker and this checkout's own, through Release, by job")
	text := proseText(said)
	assert.Contains(t, text, "reclaimed dockhand-worker-loose")
	assert.Contains(t, text, "reclaimed dockhand-worker-ours")
	assert.Contains(t, text, "dockhand-worker-elsewhere is a worker from /elsewhere/ports — its own `dockhand cycle --reclaim-orphans` reclaims it")
	assert.Contains(t, text, "warning: dockhand-worker-jobless cannot be reclaimed: the backend named no job for it")
	assert.NotContains(t, text, "reclaimed dockhand-worker-running", "a tracked worker is not an orphan")
}

func TestReclaimOrphansSaysWhenItCannotAnswerOrHasNothingToDo(t *testing.T) {
	repo, _ := engineRepo(t)
	ctx := context.Background()

	said := testState(t, repo, &verifytest.Fake{}).ReclaimOrphans(ctx, repo)
	assert.Equal(t, "no untracked workers reclaimed\n", proseText(said))

	refusing := &verifytest.Fake{WorkersErr: errors.New("tart list: exit status 1")}
	said = testState(t, repo, refusing).ReclaimOrphans(ctx, repo)
	assert.Contains(t, proseText(said), "warning: no untracked worker reclaimed: tart list: exit status 1",
		"a person asked; silence would read as nothing needing doing")

	failing := &verifytest.Fake{
		Live:       []verify.Worker{{Name: "dockhand-worker-stuck", Job: verify.Job{Provider: "fake", ID: "dockhand-worker-stuck"}}},
		ReleaseErr: map[string]error{"dockhand-worker-stuck": errors.New("vm is busy")},
	}
	said = testState(t, repo, failing).ReclaimOrphans(ctx, repo)
	assert.Contains(t, proseText(said), "warning: reclaiming dockhand-worker-stuck: vm is busy")
	assert.Contains(t, proseText(said), "no untracked workers reclaimed")
}
