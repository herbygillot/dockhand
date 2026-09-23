package portindex

import (
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// A stager stages a tree once and opens it after; a second stager over the
// same cache finds the generation there.
func TestStagerStagesOnceAndOpensAfter(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile)
	_, tree := f.commit()
	source := record.Source{Tree: record.ObjectID(tree)}
	snapshot, err := f.repo.Materialize(t.Context(), tree)
	require.NoError(t, err)
	t.Cleanup(func() { snapshot.Close() })
	into, err := macports.NewTree(source, snapshot.Root, testPlatform)
	require.NoError(t, err)
	var mu sync.Mutex
	var messages []string
	ctx := progress.WithReporter(t.Context(), func(update progress.Update) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, update.Message)
	})

	stager := &Stager{Repo: f.repo, Config: f.config}
	index, err := stager.Index(ctx, into)
	require.NoError(t, err)
	requireVersion(t, index, "working", "1")
	require.Contains(t, strings.Join(messages, "\n"), "Generating full PortIndex for source "+tree[:12])
	messages = nil
	index, err = stager.Index(ctx, into)
	require.NoError(t, err)
	requireVersion(t, index, "working", "1")
	require.Empty(t, messages, "a tree already staged is only opened")

	again := &Stager{Repo: f.repo, Config: f.config}
	_, err = again.Index(ctx, into)
	require.NoError(t, err)
	require.Contains(t, strings.Join(messages, "\n"), "Using cached PortIndex for source "+tree[:12])

	unplaced, err := macports.NewTree(source, snapshot.Root, record.Platform{})
	require.NoError(t, err)
	_, err = (&Stager{Repo: f.repo, Config: f.config}).Index(ctx, unplaced)
	require.ErrorContains(t, err, "names no platform")
	_, err = (&Stager{}).Index(ctx, into)
	require.ErrorContains(t, err, "repository is required")
}
