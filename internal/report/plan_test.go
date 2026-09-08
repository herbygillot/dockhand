package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/plan"
)

// A cargo port's distfile delta runs to hundreds of entries; inlined,
// one field run measured it at 87KB on a single line, burying the
// branch and verify lines underneath. Big changes summarize; small
// ones still print in full.
func TestRenderChangeSummarizesTheBigOnes(t *testing.T) {
	small := plan.Change{Field: "version", Old: []string{"1.0"}, New: []string{"1.1"}}
	assert.Equal(t, "version 1.0 -> 1.1", renderChange(small))

	old := make([]string, 214)
	new_ := make([]string, 215)
	for i := range old {
		old[i] = "crate-" + strings.Repeat("x", 20)
	}
	copy(new_, old)
	for i := 0; i < 37; i++ {
		new_[i] = "changed-" + strings.Repeat("y", 20)
	}
	new_[214] = "added-crate"
	got := renderChange(plan.Change{Field: "distfiles", Old: old, New: new_})
	assert.Equal(t, "distfiles 214 -> 215 entries (38 new or changed)", got)
	assert.Less(t, len(got), 120)
}

// The summary's reason column is its own width, pinned by the intent
// goldens: two spaces, the reason and its colon padded to sixteen,
// then a space before the values.
func TestRenderPlanPadsTheReasonColumn(t *testing.T) {
	var b strings.Builder
	Plan(&b, &plan.Plan{
		Intent:  "bump",
		Portdir: "/tree/devel/jq",
		Subport: "jq-devel",
		Edits: []edit.Edit{
			{Reason: "version", Old: "1.0", New: "2.0"},
			{Reason: "checksums", Old: "old", New: "new"},
		},
		Predicted: []plan.ContextDelta{{
			Subport: "jq",
			Changes: []plan.Change{{Field: "version", Old: []string{"1.0"}, New: []string{"2.0"}}},
		}},
	})
	assert.Equal(t, "plan: bump /tree/devel/jq (subport jq-devel), 2 edits\n"+
		"  version:         1.0 -> 2.0\n"+
		"  checksums:       old -> new\n"+
		"predicted delta:\n"+
		"  jq: version 1.0 -> 2.0\n", b.String())
}

// A whole file the plan rewrites is a line in the same table, its path
// where the reason goes and what happened to it where the values go.
// The bytes stay in the JSON.
func TestRenderPlanListsTheFilesItRewrites(t *testing.T) {
	var b strings.Builder
	Plan(&b, &plan.Plan{
		Intent:  "bump",
		Portdir: "/tree/devel/jq",
		Edits:   []edit.Edit{{Reason: "version", Old: "1.0", New: "2.0"}},
		Files:   []plan.FileEdit{{Path: "files/patch-foo.diff", Content: "@@ -9,1 +9,1 @@\n", Reason: "1 hunk moved"}},
	})
	assert.Equal(t, "plan: bump /tree/devel/jq, 1 edits\n"+
		"  version:         1.0 -> 2.0\n"+
		"  files/patch-foo.diff: 1 hunk moved\n"+
		"predicted delta:\n", b.String())
	assert.NotContains(t, b.String(), "@@", "a patch is a page; the summary names it")
}

// A plan that could not check the port's patches says so, in the
// finding's own words and after everything it did: the sentence opens
// with its verdict, so it stands as its own line rather than under a
// label. Any other finding a plan carries — the instruction comment,
// whose criterion is its quote — is status's to ask about, and not this
// summary's to print.
func TestRenderPlanSaysWhatItCouldNotCheck(t *testing.T) {
	var b strings.Builder
	Plan(&b, &plan.Plan{
		Intent:  "bump",
		Portdir: "/tree/devel/jq",
		Edits:   []edit.Edit{{Reason: "version", Old: "1.0", New: "2.0"}},
		Riders:  []string{"modeline"},
		Findings: []plan.Finding{
			// The kind is a literal rather than a constant this package
			// declares, because this package does not READ it: intent owns
			// the spelling (intent.FindingInstruction) and the point of the
			// fixture is that a finding report knows nothing about is not
			// printed.
			{Kind: "instruction-comment", Quote: "# revbump foo when updating"},
			{Kind: KindPatchesUnchecked, Ports: []string{"jq"},
				Criterion: "patch check unavailable: jq's 1 patchfile was not checked against the new source because no distfile was fetched"},
		},
	})
	assert.Equal(t, "plan: bump /tree/devel/jq, 1 edits\n"+
		"  version:         1.0 -> 2.0\n"+
		"also: modeline\n"+
		"patch check unavailable: jq's 1 patchfile was not checked against the new source because no distfile was fetched\n"+
		"predicted delta:\n", b.String())
	assert.NotContains(t, b.String(), "revbump", "a proposal is status's line, not the plan summary's")
}

// AN EDIT THAT REPLACES A PAGE IS SUMMARIZED, NOT PRINTED. A Rust
// port's cargo.crates block is one edit whose Old and New are each some
// four hundred lines, and the narration printed both verbatim — eight
// hundred lines of vendored crate names between the person and the
// branch name they were waiting for.
func TestABlockSizedEditIsReportedByItsShape(t *testing.T) {
	block := strings.Repeat("    serde-1.0.203 abcdef0123456789\n", 400)
	var b bytes.Buffer
	Plan(&b, &plan.Plan{
		Intent: "bump", Portdir: "lang/skim",
		Edits: []edit.Edit{{Reason: "crates", Old: block, New: block + "    x-1.0 f\n"}},
	})
	out := b.String()
	assert.NotContains(t, out, "serde-1.0.203", "the bytes belong to --plan's JSON, not to a terminal")
	assert.Contains(t, out, "401 lines")
	assert.LessOrEqual(t, strings.Count(out, "\n"), 4, "one edit is one line")
}

// AND A SMALL VALUE IS STILL THE NEWS. A version bump's whole point is
// the two strings, and summarizing them would report the shape of a
// fact the reader came for.
func TestAnOrdinaryEditStillPrintsItsValues(t *testing.T) {
	var b bytes.Buffer
	Plan(&b, &plan.Plan{
		Intent: "bump", Portdir: "lang/skim",
		Edits: []edit.Edit{{Reason: "version", Old: "0.16.2", New: "0.19.0"}},
	})
	assert.Contains(t, b.String(), "0.16.2 -> 0.19.0")
}

// THE PLAN SAYS WHICH PATCH DID NOT COME OVER, before any branch
// exists. A person reading a plan is deciding whether to mint it, and
// this is the one finding that tells them they have work to do on the
// branch afterwards — where it used to be a refusal they met instead of
// a plan at all.
func TestThePlanNarrationStatesAPatchThatDidNotCarryOver(t *testing.T) {
	var b bytes.Buffer
	Plan(&b, &plan.Plan{
		Intent: "bump", Portdir: "lang/skim",
		Findings: []plan.Finding{{
			Kind:      KindPatchUnrelocated,
			Source:    "files/patch-foo.diff",
			Criterion: "patch not carried over: files/patch-foo.diff does not relocate onto the new source — Makefile hunk #1: its before-block occurs nowhere in the file. Refresh it by hand on the branch, then verify",
		}},
	})
	assert.Contains(t, b.String(), "does not relocate onto the new source")
	assert.Contains(t, b.String(), "Refresh it by hand on the branch")
}
