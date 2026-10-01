package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// A patch of the candidate's that doesn't apply is said, as is each the
// base applied that the candidate drops, with whether it still would:
// libuv's #34620 dropped patch-libuv-legacy.diff, which no longer applied
// (the libuv run's finding 2). None holds; one the check couldn't model is
// coverage, and one that applies is nothing to say.
func TestPatchesAreSaidAgainstTheCandidatesSource(t *testing.T) {
	comparison := Assess(Input{Versions: sourcecompare.Versions{Old: "1.44.2", New: "1.52.1"}, Patches: []Patch{
		{Result: patchcheck.Result{Name: "patch-kept.diff", Checked: true, Applies: true}},
		{Result: patchcheck.Result{Name: "patch-stale.diff", Checked: true, Detail: "1 out of 1 hunk FAILED"}},
		{Result: patchcheck.Result{Name: "patch-libuv-legacy.diff", Checked: true, Detail: "5 out of 5 hunks FAILED"}, Dropped: true},
		{Result: patchcheck.Result{Name: "patch-still.diff", Checked: true, Applies: true}, Dropped: true},
		{Result: patchcheck.Result{Name: "patch-elsewhere.diff", Detail: "patch.dir leaves the source directory"}},
	}})
	require.Equal(t, []string{
		"· patch-stale.diff doesn't apply to 1.52.1's source, so the build fails at its patch phase: 1 out of 1 hunk FAILED",
		"· patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED",
		"· patch-still.diff, which the base applied, is dropped, though it still applies to 1.52.1's source: what it fixed may need it still",
	}, messages(comparison.Changes))
	require.Equal(t, []string{PatchRejected, PatchDropped, PatchDropped}, []string{comparison.Changes[0].Rule, comparison.Changes[1].Rule, comparison.Changes[2].Rule})
	require.Len(t, comparison.Coverage, 1)
	require.Equal(t, "patch-unchecked", comparison.Coverage[0].Policy)
	require.Equal(t, "patch.dir leaves the source directory", comparison.Coverage[0].Reason)
}
