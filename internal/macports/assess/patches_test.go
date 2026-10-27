package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// A patch of the candidate's that doesn't apply is said, as is each the
// base applied that the candidate drops, with whether it still would:
// libuv's #34620 dropped patch-libuv-legacy.diff, which no longer applied
// (the libuv run's finding 2). One that doesn't apply holds (the docs
// dig's I9); one the check couldn't model is
// coverage, and so is one that applies, said so a check that found nothing
// isn't taken for none (the fluent-bit run, batch 23).
func TestPatchesAreSaidAgainstTheCandidatesSource(t *testing.T) {
	comparison := Assess(Input{Versions: sourcecompare.Versions{Old: "1.44.2", New: "1.52.1"}, Patches: []Patch{
		{Result: patchcheck.Result{Name: "patch-kept.diff", Checked: true, Applies: true}},
		{Result: patchcheck.Result{Name: "patch-stale.diff", Checked: true, Detail: "1 out of 1 hunk FAILED"}},
		{Result: patchcheck.Result{Name: "patch-libuv-legacy.diff", Checked: true, Detail: "5 out of 5 hunks FAILED"}, Dropped: true},
		{Result: patchcheck.Result{Name: "patch-still.diff", Checked: true, Applies: true}, Dropped: true},
		{Result: patchcheck.Result{Name: "patch-elsewhere.diff", Detail: "patch.dir leaves the source directory"}},
	}})
	require.Equal(t, []string{
		"! patch-stale.diff doesn't apply to 1.52.1's source, as dockhand read it, and a check shows whether the build's patch phase fails: 1 out of 1 hunk FAILED",
		"· patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED",
		"· patch-still.diff, which the base applied, is dropped, though it still applies to 1.52.1's source: what it fixed may need it still",
	}, messages(comparison.Changes))
	require.Equal(t, []string{PatchRejected, PatchDropped, PatchDropped}, []string{comparison.Changes[0].Rule, comparison.Changes[1].Rule, comparison.Changes[2].Rule})
	require.Equal(t, []model.Coverage{
		{Path: "patch-kept.diff", Relevance: "used", Treatment: "inspected", Policy: "patch-applies", Reason: "applies to 1.52.1's source"},
		{Path: "patch-elsewhere.diff", Relevance: "used", Treatment: "set-apart", Policy: "patch-unchecked", Reason: "patch.dir leaves the source directory"},
	}, comparison.Coverage)

	// One that applies only with fuzz holds, which fuzz never does
	// unattended (v1's D12, the docs dig's I4); one at an offset alone is
	// coverage, as one that applies.
	fuzzed := Assess(Input{Versions: sourcecompare.Versions{Old: "1.44.2", New: "1.52.1"}, Patches: []Patch{
		{Result: patchcheck.Result{Name: "patch-fuzzy.diff", Checked: true, Applies: true, Detail: "applies only with fuzz: hunk 2 with fuzz 1", Hunks: []patchcheck.Hunk{{Number: 2, Line: 40, Fuzz: 1}}}},
		{Result: patchcheck.Result{Name: "patch-moved.diff", Checked: true, Applies: true, Detail: "applies, hunk 1 at an offset of 3 lines", Hunks: []patchcheck.Hunk{{Number: 1, Line: 4, Offset: 3}}}},
	}})
	require.Len(t, fuzzed.Changes, 1)
	require.Equal(t, PatchFuzz, fuzzed.Changes[0].Rule)
	require.True(t, fuzzed.Changes[0].Hold)
	require.Equal(t, "patch-fuzzy.diff applies to 1.52.1's source only with fuzz, hunk 2 with fuzz 1, and a fuzzed hunk can land in the wrong place and still build: look at where it lands", fuzzed.Changes[0].Message)
}
