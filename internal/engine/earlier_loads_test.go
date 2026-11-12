package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// loadCounting counts the store's loads of earlier builds: each Reusable
// query and each inputs read.
type loadCounting struct {
	store.Store
	reusable, inputs *atomic.Int32
}

func (s loadCounting) View(ctx context.Context, repository model.RepositoryID, read func(store.Reader) error) error {
	return s.Store.View(ctx, repository, func(r store.Reader) error { return read(countingReader{Reader: r, s: s}) })
}

type countingReader struct {
	store.Reader
	s loadCounting
}

func (r countingReader) Reusable(target model.TargetID, environment model.Environment, limit int) ([]store.Build, error) {
	r.s.reusable.Add(1)
	return r.Reader.Reusable(target, environment, limit)
}

func (r countingReader) Inputs(key string) (model.TargetInputs, error) {
	r.s.inputs.Add(1)
	return r.Reader.Inputs(key)
}

// A check's driver loads earlier builds only where reuse can be chosen,
// before the first attempt of a check that isn't fresh: a fresh check loads
// none, and a second and third attempt load nothing more than the first
// did. A first attempt that reuses still loads them, and reuses (#189).
func TestEarlierBuildsAreLoadedOnlyWhereReuseCanBeChosen(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	url, first, _ := harborRepository(t)
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, fetches: map[model.TargetID]string{"libharbor": first}}, identity: "origin a"}
	c := newHarborChecks(t, e, gitHarbor(url, "v4"), provider)
	c.check(false)
	counting := loadCounting{Store: e.Store, reusable: &atomic.Int32{}, inputs: &atomic.Int32{}}
	kept := e.Store
	e.Store = counting

	c.check(true)
	require.Zero(t, counting.reusable.Load(), "a fresh check reuses nothing, so it loads no earlier build")
	require.Zero(t, counting.inputs.Load())

	c.check(false)
	firstAttempt := counting.reusable.Load()
	require.Positive(t, firstAttempt, "a first attempt that can reuse loads its targets' earlier builds")

	// Two attempts that fail as the environment's, then a third: only the
	// first loads.
	e.Store = kept
	usePorts(e, gitHarbor(url, "v4", true))
	e.Store = counting
	counting.reusable.Store(0)
	provider.scriptedProvider.failures = 2
	c.check(false)
	require.LessOrEqual(t, counting.reusable.Load(), firstAttempt, "the second and third attempts load nothing more")
}
