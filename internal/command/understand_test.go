package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// jqDependents stands in for the port index: two ports depend on jq.
// jqDependents are jq's dependents, as the index has them; where under,
// jo links it under its +jq variant too, as variantDependents finds.
type jqDependents struct{ under bool }

func (d jqDependents) Dependents(context.Context, model.Source, []string) ([]engine.Dependent, error) {
	jo := engine.Dependent{Name: "jo", Directory: "textproc/jo", On: []string{"jq"}, Phases: []string{"build"}}
	if d.under {
		jo.Phases, jo.Variants = []string{"library"}, []string{"jq"}
	}
	return []engine.Dependent{jo, {Name: "yq", Directory: "textproc/yq", On: []string{"jq"}, Phases: []string{"library", "runtime"}}}, nil
}

// jqAndDevel reads jq's dependents as jqDependents does, and names the
// ports textproc/jq defines, as the index does for a directory with a
// subport.
type jqAndDevel struct{ jqDependents }

func (jqAndDevel) PortsDefined(context.Context, model.Source, []string) (map[string][]string, error) {
	return map[string][]string{"textproc/jq": {"jq", "jq-devel"}}, nil
}

func TestDiffAndImpact(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	worktree := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", worktree)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := dockhand(t, "diff")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · from master ")
	require.Contains(t, out, " · with uncommitted edits to 1 file\n  textproc/jq  changed\n\n")
	require.Contains(t, out, "--- a/textproc/jq/Portfile\n+++ b/textproc/jq/Portfile\n")
	require.Contains(t, out, "-version 1.7.1\n+version 1.8.1\n")

	t.Chdir(filepath.Join(worktree, "textproc", "jq"))
	out, _, err = dockhand(t, "diff", "--stat", "Portfile")
	require.NoError(t, err)
	require.Contains(t, out, "\n\n  textproc/jq/Portfile\n", "a path is taken from where you are")
	out, _, err = dockhand(t, "diff", "files")
	require.NoError(t, err)
	require.Contains(t, out, "Nothing changed under those paths.")

	testDependentReader = jqDependents{}
	t.Cleanup(func() { testDependentReader = nil })
	out, _, err = dockhand(t, "impact")
	require.NoError(t, err)
	require.Equal(t, `Changed ports     jq
Other dependents  jo (build), yq (library, runtime); candidates to look at
Shared files      none
Next: dockhand check --also yq,jo builds them against the branch
`, out)

	// A changed directory's other ports are named as the base's index has
	// them: devel/libuv's libuv-devel read as libuv alone (batch 32).
	testDependentReader = jqAndDevel{}
	out, _, err = dockhand(t, "impact")
	require.NoError(t, err)
	require.Contains(t, out, "Changed ports     jq and jq-devel\n")
	testDependentReader = jqDependents{}

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\nrevision 1\n"), 0o644))
	out, _, err = dockhand(t, "impact")
	require.NoError(t, err)
	require.Contains(t, out, "Changed ports     jq (revision only)\nOther dependents  none looked for; the branch changes no existing port beyond its revision\n")
}
