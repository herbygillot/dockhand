package verdict

// The guest-log corpus, swept where the judgments now live.
//
// The corpus itself stays at internal/lifecycle/testdata/logs and is
// read from here by relative path. It is not copied: a corpus is only
// worth having if a real `dockhand log` capture dropped into it is
// picked up everywhere with no code change, and two copies would drift
// the first time someone replaced a reconstruction with a capture. The
// directory's README says how to add one, and the lifecycle package
// sweeps the same files through the effectful settle. What a .expect
// sidecar means is stated once too, in corpustest, so the two sweeps
// cannot come to read one differently.
//
// What runs here is the judgment half: each reader on its own, and then
// all of them composed through JudgeRun, with the poll status and the
// log supplied as values. No repository, no worker, no fake provider.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/corpustest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// corpusDir is the one copy of the corpus, in the package whose settle
// the judgments were extracted from.
const corpusDir = "../lifecycle/testdata/logs"

func TestLogCorpus(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join(corpusDir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs, "%s must hold at least the reconstructed shapes", corpusDir)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			log := string(raw)
			exp := corpustest.Read(t, strings.TrimSuffix(path, ".log")+".expect")

			// Each reader on its own, consulted as the judgment consults
			// it: lint is what the log's lint line says whatever the
			// outcome; the failure-side readers answer only for a failed
			// guest.
			assert.Equal(t, exp.Lint, LintSummary(log), "lint summary")
			if exp.Outcome == "failed" {
				assert.Equal(t, exp.State == "unsupported", PortDeclined(log), "refusal")
				summary := FailureSummary(log)
				dep, blocked := DependencyFailure(summary, exp.Port)
				assert.Equal(t, exp.Blamed, dep, "blamed port")
				assert.Equal(t, exp.State == "blocked", blocked, "blocked")
				if exp.State == "failed" {
					assert.Equal(t, exp.Detail, summary, "a failure's detail is the first substantive Error line")
				}
				// And through the guarded reader the caller uses to know
				// whether the tree lookup is even worth doing.
				dep, blamed := BlamedDependency(log, exp.Port)
				assert.Equal(t, exp.Blamed, dep, "blamed port, guarded")
				assert.Equal(t, exp.State == "blocked", blamed)
			}

			// And composed, through JudgeRun itself: the state the note
			// records, the detail it carries, and what happens to the
			// worker — kept for a failure, released for anything else,
			// because only one's own breakage is worth a slot.
			t.Run("judge", func(t *testing.T) {
				st := verify.Status{State: verify.Failed, Handle: "fake-1"}
				if exp.Outcome == "passed" {
					st = verify.Status{State: verify.Passed, Handle: "fake-1"}
				}
				// The corpus settles against a tree holding no
				// dependency's Portfile, so nothing is ever annotated
				// here; the annotation has its own test.
				require.True(t, NeedsLog(st.State, true), "every corpus run reads its log")
				j := JudgeRun(RunInput{
					Run:    running(true),
					Port:   exp.Port,
					Status: st,
					Log:    log, LogRead: true,
				})
				require.True(t, j.Settled)
				// The sidecar is plain text, so the wire word is what it
				// names: the state converts to it rather than the corpus
				// learning a Go type.
				assert.Equal(t, exp.State, string(j.Run.State), "state")
				assert.Equal(t, exp.Detail, j.Run.Detail, "detail")
				if exp.Outcome == "passed" {
					assert.Equal(t, exp.Lint, j.Run.Lint, "lint evidence is read before the release")
				} else {
					assert.Empty(t, j.Run.Lint, "lint is corroborated on a pass; a failed run's log stays reachable")
				}
				if exp.State == "failed" {
					assert.Equal(t, "fake-1", j.Run.Handle, "the failure's environment is the debug handle")
					assert.Equal(t, KeepWorker, j.Release, "a failed run's worker is kept")
				} else {
					assert.Empty(t, j.Run.Handle, "nothing of this branch's to debug")
					assert.NotEqual(t, KeepWorker, j.Release, "the worker is released")
				}
			})

			// A pass is also the one shape whose verdict a caller may
			// reach without the log at all, and the corpus says what is
			// lost when it does: the lint evidence, and nothing else.
			if exp.Outcome == "passed" {
				j := JudgeRun(RunInput{Run: running(false), Port: exp.Port,
					Status: verify.Status{State: verify.Passed, Handle: "fake-1"}})
				assert.Equal(t, record.Passed, j.Run.State)
				assert.Empty(t, j.Run.Lint, "a run that never linted reads no log")
			}
		})
	}
}
