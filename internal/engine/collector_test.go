package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// An update's assessment, kept as its revision's, checks its patches as a
// revision's assessment does: it had none, and a later look reused it
// without them (the architecture review's finding 1, its probe made a
// regression test). The record is reused, collecting nothing again, and
// says of the patch what it found, as a fresh collection does.
func TestAnUpdatesAssessmentChecksItsPatches(t *testing.T) {
	t.Parallel()
	f := setup(t)
	patch := "--- src/main.c\n+++ src/main.c\n@@ -1 +1 @@\n-int main;\n+int main(void);\n"
	write(t, f.upstream, map[string]string{"textproc/jq/files/patch-main.diff": patch})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "add patch")
	f.options.Readings = t.TempDir()
	e, editor := f.withPreparer(t)
	old := map[string]string{"LICENSE": "MIT\n", "src/main.c": "int main;\n"}
	new := map[string]string{"LICENSE": "MIT\n", "src/main.c": "int main;\n"}
	editor.upstream = [2]map[string]string{old, new}
	editor.options = map[string]string{"patchfiles": "patch-main.diff", "patch.pre_args": "-p0", "extract.rename": "0", "worksrcdir": "jq-1.8.1"}
	update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "collected-update"}, Action: model.EditUpdate, Port: "jq", CompareUpstream: true})
	require.NoError(t, err)
	worktree, err := e.worktree(t.Context(), update.Branch)
	require.NoError(t, err)
	_, current, err := worktree.WorkingTree(t.Context())
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(update.Base)})
	require.NoError(t, err)
	base, tree := model.ObjectID(trees[string(update.Base)]), model.ObjectID(current)
	planner := newPlanner(t)
	e.ArchivePlanner = planner
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}}
	planner.add(base, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.7.1", Options: editor.options}, archives: map[string]map[string]string{"jq-1.7.1.tar.gz": old}})
	planner.add(tree, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.8.1", Options: editor.options}, archives: map[string]map[string]string{"jq-1.8.1.tar.gz": new}})
	// What the record says of the patch: a coverage line where it applied,
	// a finding where it didn't. The update's archive is the fake
	// preparer's, at another top than the port builds in, so its patch
	// doesn't apply there, where the planner's does; either way it was
	// checked.
	ofPatch := func(c model.UpstreamComparison) int {
		n := 0
		for _, coverage := range c.Coverage {
			if coverage.Path == "patch-main.diff" {
				n++
			}
		}
		for _, change := range c.Changes {
			if change.Path == "patch-main.diff" {
				n++
			}
		}
		return n
	}

	reused, err := e.revisionAssessments(t.Context(), update.Branch.ID, update.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, reused, 1)
	require.Zero(t, planner.fetches.Load(), "the update's record stands, collecting nothing again")
	require.Equal(t, 1, ofPatch(reused[0].Comparison), "the update's own record checked the patch")
	fresh := e.assessPort(t.Context(), planner, [2]model.Source{{Tree: base}, {Tree: tree}}, "textproc/jq", "jq", true)
	require.Empty(t, fresh.Problem)
	require.Equal(t, 1, ofPatch(fresh), "as a fresh collection does")
}
