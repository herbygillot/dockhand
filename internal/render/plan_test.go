package render

import (
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
	RenderPlan(&b, &plan.Plan{
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
	RenderPlan(&b, &plan.Plan{
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
