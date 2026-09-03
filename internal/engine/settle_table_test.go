package engine

// The settle over the log corpus, composed. What each reader says
// about a guest log is verdict's own table test now; what runs here is
// the settle that carries those judgments out — the state written to
// the note, the detail it carries, and what happens to the worker,
// with a real repository and a fake provider behind it. The corpus
// itself stays at testdata/logs and verdict sweeps the same files from
// the other side, so a real `dockhand log` capture dropped there is
// picked up by both with no code change; testdata/logs/README.md says
// how, and corpustest holds the one reader of a .expect sidecar.

import (
	"context"
	"errors"
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

// corpusNote seeds the tip with one running, linted job for the
// corpus port, the way RecordRun leaves it after a submit.
func corpusNote(t *testing.T, repo *git.Repo, sha, port string) record.Record {
	t.Helper()
	return seededNote(t, repo, sha, port, true)
}

// seededNote is corpusNote with the lint record chosen: a run
// submitted without lint never reads the log on a pass.
func seededNote(t *testing.T, repo *git.Repo, sha, port string, linted bool) record.Record {
	t.Helper()
	ctx := context.Background()
	n := mintedNoteFor(t, repo, sha, port)
	startedFor(&n, port, "Testos", "fake-1", record.Run{State: record.Running, Linted: linted})
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	return n
}

// The composition branches no log can reach: SettleRuns's own handling
// of a provider that fails once the verdict is known, and of a run
// that never linted. A pass whose worker cannot be released still
// settles passed and says so in its detail; a failure whose log cannot
// be read still settles failed, handle kept, with no diagnosis to
// quote; a pass whose log cannot be read settles passed with no lint
// evidence; and a pass that never linted reads no log at all, so a
// lint line in it is not evidence.
func TestSettleRunsProviderFailuresTable(t *testing.T) {
	passed := map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}}
	failed := map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}}
	lintLog := map[string]string{"fake-1": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"}
	unreadable := map[string]error{"fake-1": errors.New("ssh: connection reset by peer")}
	cases := []struct {
		name     string
		fake     *verifytest.Fake
		linted   bool
		state    string
		detail   string
		lint     string
		handle   string
		released []string
	}{
		{name: "a pass whose release fails",
			fake: &verifytest.Fake{States: passed, Logs: lintLog,
				ReleaseErr: map[string]error{"fake-1": errors.New("tart delete: vm is busy")}},
			linted: true, state: "passed", detail: "worker not released: tart delete: vm is busy", lint: "2 warnings"},
		{name: "a failure whose log cannot be read",
			fake:   &verifytest.Fake{States: failed, LogErr: unreadable},
			linted: true, state: "failed", handle: "fake-1"},
		{name: "a pass whose log cannot be read",
			fake:   &verifytest.Fake{States: passed, LogErr: unreadable},
			linted: true, state: "passed", released: []string{"fake-1"}},
		{name: "a pass that never linted reads no log",
			fake:   &verifytest.Fake{States: passed, Logs: lintLog},
			linted: false, state: "passed", released: []string{"fake-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, sha := engineRepo(t)
			n := seededNote(t, repo, sha, "jq", tc.linted)
			require.NoError(t, testState(t, repo, tc.fake).settle(context.Background(), repo, &n))
			r := runOf(n, "Testos")
			assert.Equal(t, tc.state, string(r.State), "state")
			assert.Equal(t, tc.detail, r.Detail, "detail")
			assert.Equal(t, tc.lint, r.Lint, "lint evidence")
			assert.Equal(t, tc.handle, n.Jobs["Testos"].Handle, "handle")
			assert.Equal(t, tc.released, tc.fake.Released, "released")

			// The settle was written back whatever the provider did: a
			// fresh read agrees with the one in hand.
			again, err := ledger.Open(repo).Read(context.Background(), sha)
			require.NoError(t, err)
			assert.Equal(t, r, runOf(again, "Testos"))
		})
	}
}

func TestLogCorpus(t *testing.T) {
	dir := filepath.Join("testdata", "logs")
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs, "%s must hold at least the reconstructed shapes", dir)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			log := string(raw)
			exp := corpustest.Read(t, strings.TrimSuffix(path, ".log")+".expect")

			// Composed, through settle itself: the state the note
			// records, the detail it carries, and what happens to the
			// worker — kept for a failure, released for anything else,
			// because only one's own breakage is worth a slot.
			repo, sha := engineRepo(t)
			st := verify.Status{State: verify.Failed, Handle: "fake-1"}
			if exp.Outcome == "passed" {
				st = verify.Status{State: verify.Passed, Handle: "fake-1"}
			}
			fake := &verifytest.Fake{
				States: map[string]verify.Status{"fake-1": st},
				Logs:   map[string]string{"fake-1": log},
			}
			n := corpusNote(t, repo, sha, exp.Port)

			require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
			r := runFor(n, exp.Port, "Testos")
			// The sidecar is plain text, so the wire word is what it
			// names: the state converts to it rather than the corpus
			// learning a Go type.
			assert.Equal(t, exp.State, string(r.State), "state")
			assert.Equal(t, exp.Detail, r.Detail, "detail")
			if exp.Outcome == "passed" {
				assert.Equal(t, exp.Lint, r.Lint, "lint evidence is read before the release")
			} else {
				assert.Empty(t, r.Lint, "lint is corroborated on a pass; a failed run's log stays reachable")
			}
			if exp.State == "failed" {
				assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle, "the failure's environment is the debug handle")
				assert.False(t, n.Jobs["Testos"].Released)
				assert.Empty(t, fake.Released, "a failed run's worker is kept")
			} else {
				assert.Empty(t, n.Jobs["Testos"].Handle, "nothing of this branch's to debug")
				assert.True(t, n.Jobs["Testos"].Released)
				assert.Equal(t, []string{"fake-1"}, fake.Released, "the worker is released")
			}
		})
	}
}
