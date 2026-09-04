package engine

// The cohort corpus, composed. What the judge makes of a cohort log is
// verdict's own sweep; what runs here is the settle that carries it out
// — the states written to the note, the details they carry, and what
// happens to the one guest they all shared. The corpus itself stays at
// testdata/cohorts and verdict sweeps the same files from the other
// side, so a real capture dropped there is picked up by both with no
// code change.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/corpustest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestCohortCorpusSettles(t *testing.T) {
	dir := filepath.Join("testdata", "cohorts")
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs, "%s must hold at least the synthesized shapes", dir)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			exp := corpustest.ReadCohort(t, strings.TrimSuffix(path, ".log")+".expect")

			st := verify.Status{State: verify.Failed, Handle: "fake-1"}
			if exp.Outcome == "passed" {
				st = verify.Status{State: verify.Passed, Handle: "fake-1"}
			}
			repo, sha := engineRepo(t)
			fake := &verifytest.Fake{
				States: map[string]verify.Status{"fake-1": st},
				Logs:   map[string]string{"fake-1": string(raw)},
			}
			n := cohortNote(t, repo, sha, exp.Members...)

			require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

			kept := false
			for _, m := range exp.Members {
				want, r := exp.Verdict[m], runFor(n, m, "Testos")
				assert.Equal(t, want.State, string(r.State), "%s: state", m)
				assert.Equal(t, want.Detail, r.Detail, "%s: detail", m)
				assert.Equal(t, want.Blamed, r.Blamed, "%s: blamed", m)
				if want.State == "passed" {
					assert.Equal(t, want.Lint, r.Lint, "%s: lint evidence is read before the release", m)
				} else {
					assert.Empty(t, r.Lint, "%s: lint is corroborated on a pass", m)
				}
				kept = kept || want.State == "failed"
			}

			// One guest, one answer about it. A member that failed keeps
			// the environment for everybody — it is the debug handle, and
			// there is only one of it however many siblings passed — and a
			// cohort with nothing of its own to debug hands it back exactly
			// once.
			if kept {
				assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle, "the failure's environment is the debug handle")
				assert.False(t, n.Jobs["Testos"].Released)
				assert.Empty(t, fake.Released, "a failed member keeps the guest for the whole cohort")
			} else {
				assert.Empty(t, n.Jobs["Testos"].Handle, "nothing of this change's to debug")
				assert.True(t, n.Jobs["Testos"].Released)
				assert.Equal(t, []string{"fake-1"}, fake.Released, "one guest, one release")
			}

			// The settle was written back: a fresh read agrees with the one
			// in hand, member for member.
			again, err := ledger.Open(repo).Read(ctx, sha)
			require.NoError(t, err)
			for _, m := range exp.Members {
				assert.Equal(t, runFor(n, m, "Testos"), runFor(again, m, "Testos"), "%s: written back", m)
			}

			// And the promote gate reads the whole cohort: a pass
			// somewhere, no failure anywhere, and every member answered
			// for. The last clause is what plurality added — a member
			// blocked by a stranger or never announced has no evidence
			// behind it, and a gate that summed only the runs would
			// publish it on a sibling's pass.
			// The gate, restated here from the states alone so the corpus
			// checks the rule rather than repeating the implementation.
			// One platform, so a member's own run is its whole story.
			//
			// The dependents are best effort (2026-09-04): they are asked
			// for an outcome and not for a good one. The headline is not,
			// and some run somewhere still has to have passed, or the
			// change has no evidence behind it at all.
			terminal := func(state string) bool {
				switch state {
				case "queued", "submitting", "running", "":
					return false
				}
				return true
			}
			anyPassed := false
			for _, m := range exp.Members {
				if exp.Verdict[m].State == "passed" {
					anyPassed = true
				}
			}
			head := exp.Verdict[exp.Members[0]].State
			headOK := head == "passed" || head == "unsupported" || head == "withheld"
			depsSettled := true
			for _, m := range exp.Members[1:] {
				if !terminal(exp.Verdict[m].State) {
					depsSettled = false
				}
			}
			assert.Equal(t, anyPassed && headOK && depsSettled, again.Promotable(), "promotable")
		})
	}
}

// counting is a Verifier that records how often a settle went to the
// guest. Everything but the two questions a settle asks is the fake's.
type counting struct {
	*verifytest.Fake
	Polls, Logs int
}

func (c *counting) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	c.Polls++
	return c.Fake.Poll(ctx, job)
}

func (c *counting) Log(ctx context.Context, job verify.Job) (string, error) {
	c.Logs++
	return c.Fake.Log(ctx, job)
}

// countingEngine is testState with a Verifier the test can count. It
// builds its own Deps rather than borrowing the shared helper, which
// takes the fake concretely — and only the seams a settle actually
// uses, so that what it proves is about the settle and not about the
// wiring.
func countingEngine(repo *git.Repo, prov verify.Verifier) *Engine {
	return New(Deps{
		Repo:     func(context.Context) (*git.Repo, error) { return repo, nil },
		RepoFor:  func(context.Context, string) (*git.Repo, error) { return repo, nil },
		Ledger:   ledger.Open,
		Verifier: func(context.Context) (verify.Verifier, error) { return prov, nil },
		TreeRoot: repo.Root,
		Out:      io.Discard,
		Err:      io.Discard,
	})
}

// One guest is asked once, however many members are building in it.
//
// Not a tidiness point. A poll per member is N answers taken at N
// different instants, and the guest is failing in between: one member
// would be judged against a job that had not given up yet and its
// sibling against the same job after it had, from one log fetched
// somewhere in the middle. The plural judge takes one status and one
// log by construction, and this is the caller's half of that.
func TestOneGuestIsPolledOnceForEveryMember(t *testing.T) {
	repo, sha := engineRepo(t)
	prov := &counting{Fake: &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}}
	n := cohortNote(t, repo, sha, "oniguruma", "jq", "libedit")

	require.NoError(t, countingEngine(repo, prov).settle(context.Background(), repo, &n))

	assert.Equal(t, 1, prov.Polls, "one guest, one poll")
	assert.Equal(t, 1, prov.Logs, "one guest, one log — the members share the file")
	for _, m := range []string{"oniguruma", "jq", "libedit"} {
		assert.Equal(t, record.Passed, runFor(n, m, "Testos").State, m)
	}
}

// A member that already reached a verdict is still part of the cohort
// the log is read against, and still not this pass's to write.
//
// Both halves matter and they pull opposite ways. Dropping it from the
// roster would make its name in the log read as a port outside the
// change — blocking a live sibling on a stranger and handing back the
// environment that failure was keeping. Writing it back would have this
// pass overwrite a verdict somebody else reached from a poll it never
// made.
func TestASettledMemberStaysInTheRosterAndOutOfTheWrite(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := cohortNote(t, repo, sha, "jq", "oniguruma")
	// oniguruma is already down as failed, from an earlier pass.
	settled := record.Run{State: record.Failed, Platform: "Testos", Linted: true,
		Detail: "Failed to build oniguruma: command execution failed"}
	n.Runs[record.RunKey("oniguruma", "Testos")] = settled
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building oniguruma\n" +
			"Error: Failed to build oniguruma: command execution failed\n"},
	}
	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	assert.Equal(t, settled, runFor(n, "oniguruma", "Testos"), "somebody else's verdict, untouched")
	jq := runFor(n, "jq", "Testos")
	assert.Equal(t, record.Blocked, jq.State)
	assert.Equal(t, "oniguruma", jq.Blamed, "the roster still knows oniguruma is one of us")
}
