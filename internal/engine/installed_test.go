package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check's build is compared with the port's previous result in the same
// environment: a library whose install name moved is said with the check
// it compared against; with no earlier reading nothing is said (Then's
// item 9: exiv2 0.28's soname).
func TestACheckSaysWhatMovedOfWhatItInstalled(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	reading := func(soname string) func(*model.GuestExecution, *model.TargetResult) {
		var key string
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			var err error
			key, err = tx.RecordInputs(model.TargetInputs{Environment: "tart darwin 25 arm64 " + soname, Directory: "textproc/jq", Tree: model.ObjectID(strings.Repeat("a", 40)), Resources: model.ObjectID(strings.Repeat("b", 40)), Active: []model.ActivePort{}})
			return err
		}))
		return func(_ *model.GuestExecution, jq *model.TargetResult) {
			jq.Inputs = key
			jq.Installed = []string{"/opt/local/bin/jq", "/opt/local/lib/" + soname}
			jq.Libraries = []model.Library{{File: "/opt/local/lib/" + soname, Name: "/opt/local/lib/" + soname, Compatibility: "1.0.0"}}
			jq.Configure = []string{"-- Found ZLIB", "checking for " + soname + "... yes"}
		}
	}
	latest := func() model.Run {
		var runs []model.Run
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			var err error
			runs, err = r.Runs(store.RunFilter{Branch: branch.ID})
			return err
		}))
		return runs[0]
	}
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, reading("libjq.1.dylib"))
	found, err := e.InstallChanges(t.Context(), latest())
	require.NoError(t, err)
	require.Empty(t, found, "no earlier reading says nothing")

	first := latest()
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, reading("libjq.2.dylib"))
	found, err = e.InstallChanges(t.Context(), latest())
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, model.TargetID("jq"), found[0].Target)
	require.Equal(t, first.Name(), found[0].Check)
	require.Equal(t, "libjq.1.dylib is libjq.2.dylib now", found[0].Libraries[0].Words())
	require.Empty(t, found[0].Added, "a new soname is the same file")
	require.Equal(t, []string{"checking for libjq.2.dylib... yes"}, found[0].Found, "and configure's drift (Then's item 10)")
	require.Equal(t, []string{"checking for libjq.1.dylib... yes"}, found[0].Lost)
}
