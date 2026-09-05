package cmd

// D27 at the verbs: `status` observes and settles; `cycle` acts.
//
// The engine holds the same ruling over its own pass
// (TestStatusObservesAndSettlesWhileCycleActs); this is the verb
// layer's half — the two Actions a person types, against fakes that
// record every call, over one field that shows every part of the
// ruling at once.

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// d27Field is the checkout every part of the ruling can be seen in at
// once: a minted branch whose run is still going and whose pull
// request merged; a second minted branch whose run is queued; a
// hand-made branch carrying a passed note, pushed, with a merged pull
// request of its own (the fold-in); and two workers the backend is
// running that no note claims — one this checkout's own, one another
// checkout's.
type d27Field struct {
	repo           *git.Repo
	fork           string
	merged, queued string // tips of dockhand/jq-1.8 and dockhand/jq-1.9
	hand           string // tip of erasure-test
	gh             *goldenGh
	fake           *verifytest.Fake
}

func d27Repo(t *testing.T) *d27Field {
	t.Helper()
	ctx := context.Background()
	repo, merged := lifecycleRepo(t)
	fork := gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))
	writeRuns(t, repo, merged, map[string]platRun{"Testos": runningOn("fake-1")})

	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	queued := gittest.Commit(t, repo, "dockhand/jq-1.9", primary, "sysutils/jq/Portfile",
		"version 1.9\n", "jq: update to 1.9")
	deferredNote(t, repo, queued, "all 2 verification slots are busy (2 VMs running)")

	hand := gittest.Commit(t, repo, "erasure-test", primary, "sysutils/jq/Portfile",
		"version 1.8.1\n", "jq: update to 1.8.1")
	writeRuns(t, repo, hand, map[string]platRun{"Testos": passedOn("fake-hand")})
	require.NoError(t, repo.Push(ctx, "herby", "erasure-test"))

	gh := &goldenGh{login: "herbygillot", prs: map[string]string{
		"dockhand/jq-1.8": `[{"number":9,"title":"jq: update to 1.8","state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`,
		"erasure-test":    `[{"number":12,"title":"jq: update to 1.8.1","state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/12"}]`,
	}}
	worker := func(name, owner string) verify.Worker {
		return verify.Worker{Name: name, Owner: owner, Job: verify.Job{Provider: "fake", ID: name}}
	}
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
		Live: []verify.Worker{
			worker("dockhand-worker-ours", repo.Root),
			worker("dockhand-worker-elsewhere", "/elsewhere/ports"),
		},
	}
	return &d27Field{repo: repo, fork: fork, merged: merged, queued: queued, hand: hand, gh: gh, fake: fake}
}

// state wires a run over the field with every seam counted: how many
// times the provider was composed, how many times the audit's lister
// was, and every gh invocation (the goldenGh records those itself).
func (f *d27Field) state(t *testing.T) (rs *runstate.Context, out, errb *bytes.Buffer, composed *int) {
	t.Helper()
	out, errb = &bytes.Buffer{}, &bytes.Buffer{}
	n := 0
	rs = &runstate.Context{TreeRoot: f.repo.Root, Tools: testFinder(), Out: out, Err: errb, Version: "1.2.3",
		Gh: f.gh.run,
		Verifier: func(context.Context) (verify.Verifier, error) {
			n++
			return f.fake, nil
		}}
	rs.Lister = rs.Verifier
	t.Cleanup(rs.Close)
	return rs, out, errb, &n
}

// forkRefs is the fork as it stands: proof of what a pass pushed or
// deleted there, or did not.
func (f *d27Field) forkRefs(t *testing.T) string {
	t.Helper()
	refs, err := exec.Command("git", "-C", f.fork, "for-each-ref", "--format=%(refname)").Output()
	require.NoError(t, err)
	return string(refs)
}

// writes is every gh invocation that was not a read.
func (f *d27Field) writes() [][]string {
	var out [][]string
	for _, c := range f.gh.calls {
		if len(c) == 0 || c[0] != "api" {
			out = append(out, c)
		}
	}
	return out
}

func (f *d27Field) run(t *testing.T, sha, port string) record.Run {
	t.Helper()
	n, err := ledger.Open(f.repo).Read(context.Background(), sha)
	require.NoError(t, err)
	return testosRun(n, port)
}

// The ruling, at the verbs. status settles the running run and
// releases its guest — the last step of the verdict being written —
// and does nothing anybody else can see: no submit, no deletion, no
// push, no forge write, no reclaim; where work is waiting it names
// cycle. status --no-update touches nothing at all: no provider, no
// forge, no write. cycle does what status named: the merged branch
// goes locally and off the fork, the queued run starts, and asked to,
// the workers this checkout may claim are reclaimed — while the
// hand-made branch, merged pull request and all, is left where it is.
func TestStatusObservesAndSettlesAndCycleActsAsTyped(t *testing.T) {
	tartOnPath(t)
	ctx := context.Background()

	t.Run("status acts on nothing", func(t *testing.T) {
		f := d27Repo(t)
		rs, out, errb, composed := f.state(t)
		before := f.forkRefs(t)

		require.NoError(t, statusAction{}.Execute(ctx, rs))

		// Settled: the running run's verdict is written and its guest
		// went back, because that is the verdict being written and not
		// an act of its own.
		assert.Equal(t, record.Passed, f.run(t, f.merged, "jq").State)
		assert.Equal(t, []string{"fake-1"}, f.fake.Released, "the settled job's guest, and only that")
		assert.Positive(t, *composed, "the provider was composed to poll")
		// And nothing acted on.
		assert.Empty(t, f.fake.Submitted, "status starts nothing")
		assert.Equal(t, record.Queued, f.run(t, f.queued, "jq").State)
		assert.True(t, f.repo.HasBranch(ctx, "dockhand/jq-1.8"), "status deletes nothing")
		assert.True(t, f.repo.HasBranch(ctx, "erasure-test"))
		assert.Equal(t, before, f.forkRefs(t), "status pushes nothing")
		assert.Empty(t, f.writes(), "status reads the forge and writes nothing to it")
		said := out.String() + errb.String()
		assert.NotContains(t, said, "discarded")
		assert.NotContains(t, said, "verify: submitted")
		assert.NotContains(t, said, "reclaimed")
		// Where work is waiting, the report names the verb.
		assert.Contains(t, out.String(), "PR #9 merged — `dockhand cycle` retires the branch")
		assert.Contains(t, out.String(), "`dockhand cycle` starts it")
		assert.Contains(t, out.String(), "`dockhand cycle --reclaim-orphans` frees the slot")
		// The fold-in: the hand-made branch is shown, and its merged pull
		// request is worded as nothing here removes.
		assert.Contains(t, out.String(), "erasure-test")
		assert.Contains(t, out.String(), "PR #12 merged — not a dockhand branch, so nothing here removes it")

		t.Run("cycle acts", func(t *testing.T) {
			f.gh.calls = nil
			require.NoError(t, cycleAction{reclaimOrphans: true}.Execute(ctx, rs))

			assert.False(t, f.repo.HasBranch(ctx, "dockhand/jq-1.8"), "the merged branch is retired")
			assert.NotContains(t, f.forkRefs(t), "dockhand/jq-1.8", "locally and off the fork")
			assert.True(t, f.repo.HasBranch(ctx, "erasure-test"), "deletion stays in the namespace")
			assert.Contains(t, f.forkRefs(t), "erasure-test", "and its fork copy stands too")
			require.Len(t, f.fake.Submitted, 1, "the queued run is started")
			assert.Equal(t, []string{"jq"}, f.fake.Submitted[0].Ports)
			assert.Equal(t, record.Running, f.run(t, f.queued, "jq").State)
			assert.Contains(t, f.fake.Released, "dockhand-worker-ours", "this checkout's own orphan is reclaimed")
			assert.NotContains(t, f.fake.Released, "dockhand-worker-elsewhere", "another checkout's is not")
			assert.Empty(t, f.writes(), "a person's cycle publishes nothing")
			assert.Contains(t, out.String(), "discarded dockhand/jq-1.8")
			assert.Contains(t, out.String(), "PR #9 merged — branch cleaned")
			assert.Contains(t, out.String(), "PR #12 merged — not a dockhand branch, so nothing here removes it")
			assert.Contains(t, out.String(), "reclaimed dockhand-worker-ours")
			assert.Contains(t, out.String(), "dockhand-worker-elsewhere is a worker from /elsewhere/ports — its own `dockhand cycle --reclaim-orphans` reclaims it")
			assert.Contains(t, errb.String(), `removed dockhand/jq-1.8 from "herby"`)
			assert.Contains(t, errb.String(), "verify: submitted jq on Testos")
			assert.NotContains(t, errb.String(), "machine publish road",
				"a person's cycle never reaches the publish road's gate")
		})
	})

	t.Run("status --no-update touches nothing", func(t *testing.T) {
		f := d27Repo(t)
		rs, out, errb, _ := f.state(t)
		fail := func(what string) func(context.Context) (verify.Verifier, error) {
			return func(context.Context) (verify.Verifier, error) {
				t.Fatalf("--no-update composed %s", what)
				return nil, nil
			}
		}
		rs.Verifier, rs.Lister = fail("the verifier"), fail("the lister")
		rs.Gh = func(_ context.Context, args ...string) (string, error) {
			t.Fatalf("--no-update asked the forge: %v", args)
			return "", nil
		}
		before := f.forkRefs(t)

		require.NoError(t, statusAction{noUpdate: true}.Execute(ctx, rs))

		assert.Equal(t, record.Running, f.run(t, f.merged, "jq").State, "the note is shown as written, and not settled")
		assert.Empty(t, f.fake.Released)
		assert.Empty(t, f.fake.Submitted)
		assert.Equal(t, before, f.forkRefs(t))
		assert.True(t, f.repo.HasBranch(ctx, "dockhand/jq-1.8"))
		lines := strings.Split(out.String(), "\n")
		assert.Contains(t, lines[0], "as recorded — nothing polled (--no-update)", "said first, so nothing below is misread")
		assert.Contains(t, out.String(), "verifying", "the running run as recorded")
		assert.Contains(t, out.String(), "promoted; PR not checked (--no-update)", "a pushed branch is not called PR-less")
		assert.Contains(t, out.String(), "`dockhand cycle` starts it", "the queue still names the verb")
		assert.NotContains(t, out.String(), "untracked worker", "no worker audit")
		assert.NotContains(t, out.String(), "dockhand-worker-ours")
		assert.Empty(t, errb.String())
	})
}
