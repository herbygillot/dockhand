package tree

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// sampleTree opens a tree carrying the checked-in slice of a real
// PortIndex. There are no portdirs under it: what these tests ask about
// is what the index says, and where a name lives on disk is Resolve's
// question.
func sampleTree(t *testing.T) *Tree {
	t.Helper()
	tr, err := Open(testenv.PortIndexTree(t))
	require.NoError(t, err)
	return tr
}

func TestTreeDependents(t *testing.T) {
	deps, err := sampleTree(t).Dependents()
	require.NoError(t, err)

	rows := deps.ByPort["gdal"]
	require.Len(t, rows, 82)

	// The staging unit is the portdir, because a subport has no
	// directory of its own to edit.
	portdirs := map[string]bool{}
	for _, r := range rows {
		portdirs[r.Portdir] = true
	}
	assert.Len(t, portdirs, 39)

	// A dependent whose name is not its portdir's basename resolves to
	// the parent, which is the whole subport rule.
	var judy portindex.Dependent
	for _, r := range deps.ByPort["judy"] {
		if r.Name == "php85-Judy" {
			judy = r
		}
	}
	assert.Equal(t, "php/php-Judy", judy.Portdir)
}

func TestTreeMaintained(t *testing.T) {
	byKey, err := sampleTree(t).Maintained()
	require.NoError(t, err)
	assert.Contains(t, byKey["gh:nilason"], "gdal")
	assert.NotContains(t, byKey, "mail:nomaintainer@macports.org")
}

// The caches are built once and handed back, hit or miss. The cost is a
// full pass over the index — 25.6 MB on a real tree — so a second ask
// must not pay it again.
func TestTreeCachesWhatItBuilt(t *testing.T) {
	tr := sampleTree(t)
	first, err := tr.Dependents()
	require.NoError(t, err)
	second, err := tr.Dependents()
	require.NoError(t, err)
	require.NotEmpty(t, first.ByPort["gdal"])
	assert.Len(t, second.ByPort, len(first.ByPort))
	assert.Same(t, &first.ByPort["gdal"][0], &second.ByPort["gdal"][0], "the same rows, not rebuilt ones")

	firstM, err := tr.Maintained()
	require.NoError(t, err)
	secondM, err := tr.Maintained()
	require.NoError(t, err)
	require.NotEmpty(t, firstM["gh:nilason"])
	assert.Len(t, secondM, len(firstM))
	assert.Same(t, &firstM["gh:nilason"][0], &secondM["gh:nilason"][0])
}

// One Tree is memoized for a whole run and handed to workers that fan
// out over a pool, so the lazy fills have to survive being asked for at
// once. Under -race this fails on an unguarded fill.
func TestTreeCachesAreSafeToShare(t *testing.T) {
	tr := sampleTree(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deps, err := tr.Dependents()
			assert.NoError(t, err)
			assert.Len(t, deps.ByPort["gdal"], 82)
			byKey, err := tr.Maintained()
			assert.NoError(t, err)
			assert.Contains(t, byKey["gh:nilason"], "gdal")
			_, err = tr.Resolve("gdal")
			assert.Error(t, err, "the sample tree has no portdirs on disk")
		}()
	}
	wg.Wait()
}

// A tree with no index cannot answer, and says so rather than returning
// an empty reverse index. An empty one reads as "nothing depends on
// this", which is the answer that proposes no cohort while claiming to
// have looked.
func TestDependentsWithoutAnIndexRefuses(t *testing.T) {
	tr, err := Open(fakeTree(t))
	require.NoError(t, err)

	deps, err := tr.Dependents()
	require.ErrorIs(t, err, portindex.ErrNoIndex)
	assert.Nil(t, deps.ByPort)
	assert.Nil(t, deps.Unread, "nothing was walked, so nothing went unread")

	byKey, err := tr.Maintained()
	require.ErrorIs(t, err, portindex.ErrNoIndex)
	assert.Nil(t, byKey)
}
