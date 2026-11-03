package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// loadCounter counts the check records read through it, by the run.
type loadCounter struct {
	store.Store
	mu    sync.Mutex
	loads map[model.RunID]int
}

func (s *loadCounter) View(ctx context.Context, repository model.RepositoryID, fn func(store.Reader) error) error {
	return s.Store.View(ctx, repository, func(r store.Reader) error { return fn(countedReader{Reader: r, counter: s}) })
}

// countedReader counts each check record it reads (checks.LoadCheck).
type countedReader struct {
	store.Reader
	counter *loadCounter
}

func (r countedReader) Check(id model.RunID) (store.Check, error) {
	r.counter.mu.Lock()
	r.counter.loads[id]++
	r.counter.mu.Unlock()
	return r.Reader.Check(id)
}

// A branch's status in detail reads its latest check's own evidence once,
// for the links its builds made, its lint, and what they installed, where
// each read it again (Codex's review of 03b380f1, F4): the record is
// loaded once for that, beside the branch's combined evidence, which
// status reads as before.
func TestStatusReadsALatestChecksEvidenceOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, readsLinks: true}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	branch, err := e.Branch(t.Context(), run.Branch)
	require.NoError(t, err)

	// Status alone, which reads the branch's combined evidence.
	counter := &loadCounter{Store: e.Store, loads: map[model.RunID]int{}}
	e.Store = counter
	_, err = e.StatusReport(t.Context(), session(t, e), StatusRequest{})
	require.NoError(t, err)
	overview := counter.loads[run.ID]

	counter.loads = map[model.RunID]int{}
	report, err := e.StatusReport(t.Context(), session(t, e), StatusRequest{Branches: []model.Branch{branch}})
	require.NoError(t, err)
	require.Len(t, report.Statuses, 1)
	require.NotNil(t, report.Statuses[0].Latest)
	require.Equal(t, run.ID, report.Statuses[0].Latest.ID)
	require.Contains(t, report.Lint, run.ID)
	require.Contains(t, report.Installed, run.ID)
	require.Contains(t, report.Links, run.ID)
	require.Equal(t, overview+1, counter.loads[run.ID], "the latest check's own evidence, read once for its links, lint, and installs")
}

// Status quotes a Portfile's update instructions only where the branch
// changes the port's version, as update does: a branch changing curl's
// variants alone was told to increase p5-www-curl's revision "whenever
// the version of curl gets updated" (field testing's curl +sectrust
// notes, note 7). A new port's version, or one computed, counts as
// changed, so they're quoted as before.
func TestStatusQuotesUpdateInstructionsForAVersionChange(t *testing.T) {
	t.Parallel()
	curl := "# Increase the revision of p5-www-curl whenever the version of curl gets updated.\nversion 8.17.0\n"
	require.False(t, versionChanged([]byte(curl), []byte(curl+"variant sectrust {}\n")), "variants alone")
	require.True(t, versionChanged([]byte(curl), []byte(strings.Replace(curl, "8.17.0", "8.18.0", 1))), "a version update")
	require.True(t, versionChanged(nil, []byte(curl)), "a new port")
	require.True(t, versionChanged([]byte("version [exec date]\n"), []byte("version [exec date]\n")), "a version that can't be told")
}
