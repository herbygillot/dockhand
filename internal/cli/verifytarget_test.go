package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
)

// `dockhand verify <port>` IS THE ROAD ITS OWN SHORT DESCRIBES — "or a
// port as it sits" — and it did not work. cli passed args[0] to app
// untouched, app's adopt stage handed it to git.RelPath as if it were a
// path, and filepath.Rel refuses a bare name and a relative one alike:
// `dockhand verify jq` answered "jq is outside the repository at ...".
//
// Only two things ever worked: an absolute portdir, and a port dockhand
// ALREADY HELD A RECORD FOR — because change.Resolve takes a port name
// and the adopt road is only reached when it does not. So the broken
// case was exactly the one the phrase names: a port with no change yet,
// verified as it sits.
func TestVerifyResolvesEveryTargetFormItsUsageAdvertises(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), repo: repo}
	want := filepath.Join(repo.Root, "sysutils", "jq")
	// An explicitly relative path is relative to WHERE THE PERSON IS
	// STANDING, not to the tree — a path is a path — so the test stands
	// in the tree the way a person typing it would be.
	t.Chdir(repo.Root)

	for _, form := range []string{
		"sysutils/jq",
		"./sysutils/jq",
		want,
	} {
		dir, _, err := verifyTarget(t.Context(), s, form)
		require.NoError(t, err, form)
		assert.Equal(t, want, dir, "%s resolves to the portdir the adopt road snapshots", form)
	}
}

// A BRANCH IS NOT LOOKED UP IN THE PORTS INDEX. A dockhand/ name is a
// branch by construction, and running it through the grammar would ask
// the tree for a port called "dockhand/jq-1.8".
func TestVerifyLeavesABranchAlone(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), repo: repo}

	dir, sub, err := verifyTarget(t.Context(), s, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Empty(t, dir, "a branch names no portdir")
	assert.Empty(t, sub)
}

// A TARGET THAT RESOLVES TO NOTHING IS HANDED ON AS TYPED, because
// change.Resolve has two forms this layer cannot see: a tip and a pin.
// Refusing here would be cli inventing a "no such port" for a road it
// does not own.
func TestVerifyPassesAnUnresolvableTargetThrough(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), repo: repo}

	dir, sub, err := verifyTarget(t.Context(), s, "0f1e2d3c4b5a")
	require.NoError(t, err, "a sha is not this function's to refuse")
	assert.Empty(t, dir)
	assert.Empty(t, sub)
}
