package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// zdemo is a port of three subports, its main port's lines inside
// terraform's stub condition, and its others set apart by subport blocks.
func zdemo(mainRevision, develVersion string) string {
	return "PortSystem 1.0\nname zdemo\nversion 1.0\ncategories devel\nlicense MIT\nmaintainers nomaintainer\n" +
		"homepage https://example.invalid\ndescription demo\nlong_description demo\nmaster_sites https://example.invalid/releases\n" +
		"checksums sha256 " + strings.Repeat("a", 64) + " size 10\n" +
		"if {${subport} eq ${name}} {\n    revision " + mainRevision + "\n}\n" +
		"subport zdemo-devel {\n    version " + develVersion + "\n}\n" +
		"subport zdemo-legacy {\n    version 0.9\n}\n"
}

// A revision's change record says which of a directory's subports it
// changed, as MacPorts evaluates the base and the revision: an edit
// inside one subport's block changes that subport, and an edit inside
// `if {${subport} eq ${name}}`, terraform-1.16's stub, the main port
// alone. The directory's text says every subport changed. The record is
// kept, and what's recorded is read again; only the subports it names are
// assessed.
func TestARevisionsRecordSaysWhichSubportsChanged(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "subported"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	base := model.ObjectID(trees[string(branch.Base)])

	devel := editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.1")})
	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, devel, true)
	require.NoError(t, err)
	record := records["devel/zdemo"]
	require.Empty(t, record.Problem)
	require.Equal(t, fidelity.ChangePolicy, record.Policy)
	require.Equal(t, []string{"zdemo-devel"}, record.Changed())
	require.Equal(t, []string{"zdemo", "zdemo-devel", "zdemo-legacy"}, portNames(record.Ports), "the main port first")
	require.Contains(t, record.Ports[1].Fields, model.FieldChange{Field: "version", From: "2.0", To: "2.1"})
	require.NotEmpty(t, record.Platform.OS)

	stub := editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": zdemo("1", "2.0")})
	records, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, stub, true)
	require.NoError(t, err)
	require.Equal(t, []string{"zdemo"}, records["devel/zdemo"].Changed())
	require.Equal(t, []model.FieldChange{{Field: "revision", From: "0", To: "1"}}, records["devel/zdemo"].Ports[0].Fields)

	recorded, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, stub, false)
	require.NoError(t, err)
	require.Equal(t, []string{"zdemo"}, recorded["devel/zdemo"].Changed(), "read as recorded")

	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, devel, true)
	require.NoError(t, err)
	var assessed []string
	for _, a := range assessments {
		assessed = append(assessed, a.Port)
	}
	require.Equal(t, []string{"zdemo-devel"}, assessed, "upstream is compared for the subport the revision changed")
}

// An adopted pull request's Portfile isn't the person's, so nothing
// evaluates it for a record (the trust rule), and its directories keep
// their text scope.
func TestAnAdoptedPullRequestHasNoRecord(t *testing.T) {
	t.Parallel()
	e, branch, _, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	branch.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 4711, Head: "someone:feature", Adopted: true}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Empty(t, records)
	_, ok := changedSubports(records, "devel/libharbor")
	require.False(t, ok)
	require.True(t, recordedChange(records, "devel/libharbor", "libharbor"), "its text scope")
}

func portNames(ports []model.SubportChange) []string {
	var names []string
	for _, port := range ports {
		names = append(names, port.Port)
	}
	return names
}
