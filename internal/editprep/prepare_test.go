package editprep_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func preparationFixture(t *testing.T, body string) (*editprep.Service, editprep.Request) {
	t.Helper()
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	testsupport.Git(t, root, "init", "--quiet", "-b", "candidate")
	for name, data := range map[string]string{
		"devel/fixture/Portfile":                            "PortSystem 1.0\nPortGroup dockhand-fixture 1.0\nname fixture\nversion [format \"%s.%s\" $fixtureMajor 2]\ncategories devel\n" + body,
		"_resources/port1.0/group/dockhand-fixture-1.0.tcl": "set fixtureMajor 7\n",
	} {
		filename := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0700))
		require.NoError(t, os.WriteFile(filename, []byte(data), 0600))
	}
	testsupport.Git(t, root, "add", "--", ".")
	testsupport.Git(t, root, "commit", "--quiet", "-m", "fixture")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	commit, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	return &editprep.Service{Repo: repo, Ports: &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}}, editprep.Request{
		Action: model.EditRevbump, Source: model.Source{Commit: model.ObjectID(commit), Tree: model.ObjectID(tree)}, Selection: macports.Selection{Selector: "fixture"}, Subject: "rebuild against updated dependency",
	}
}

func TestPrepareRevisionUsesWholeSnapshotAndPreservesCheckout(t *testing.T) {
	t.Parallel()
	service, request := preparationFixture(t, "revision 4\nsubport fixture-child {\n revision 9\n}\n")
	filename := filepath.Join(service.Repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(filename, []byte("a staged user edit\n"), 0600))
	testsupport.Git(t, service.Repo.Root, "add", "--", "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(filename, []byte("a subsequent unstaged edit\n"), 0600))
	index, err := os.ReadFile(filepath.Join(service.Repo.CommonDir, "index"))
	require.NoError(t, err)
	status := testsupport.Git(t, service.Repo.Root, "status", "--porcelain=v1")
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.PreparedTree)
	require.Equal(t, request.Source, result.Base)
	require.Equal(t, "7.2", result.Fidelity[0].Before.Ports["fixture"].Version)
	require.Equal(t, "7.2", result.Fidelity[0].After.Ports["fixture"].Version)
	require.Equal(t, 5, result.Fidelity[0].After.Ports["fixture"].Revision)
	require.Equal(t, 9, result.Fidelity[0].After.Ports["fixture-child"].Revision)
	require.Empty(t, result.Fidelity[0].After.Source.Commit)
	require.Equal(t, result.PreparedTree, result.Fidelity[0].After.Source.Tree)
	require.Equal(t, "fixture: "+request.Subject, result.Commits[0].Subject)
	require.Empty(t, result.Fidelity[0].UnexpectedChanges)
	require.Equal(t, status, testsupport.Git(t, service.Repo.Root, "status", "--porcelain=v1"))
	afterIndex, err := os.ReadFile(filepath.Join(service.Repo.CommonDir, "index"))
	require.NoError(t, err)
	require.Equal(t, index, afterIndex)
	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Equal(t, "a subsequent unstaged edit\n", string(content))
	commit, _, err := service.Repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, string(request.Source.Commit), commit)
}

func TestPrepareRevisionScopesSelectedSubportAndDefaultRevision(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		body, subport string
		revision      int
	}{
		{body: "description fixture\n", revision: 1},
		{body: "revision 4\nsubport fixture-child {\n revision 9\n}\n", subport: "fixture-child", revision: 10},
		{body: "revision 4\nsubport fixture-child {\n description {inherited revision}\n}\n", subport: "fixture-child", revision: 5},
		{body: "revision 4\nsubport fixture-child {}\n", revision: 5},
	} {
		t.Run(fixture.subport+fixture.body, func(t *testing.T) {
			service, request := preparationFixture(t, fixture.body)
			request.Source.Commit = ""
			request.Selection.Subport = fixture.subport
			result, err := service.Prepare(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, fixture.revision, result.Fidelity[0].After.Ports[result.Target.Name].Revision)
			if fixture.subport != "" {
				require.Equal(t, 4, result.Fidelity[0].After.Ports["fixture"].Revision)
			}
		})
	}
}

func TestPreparationDeclinesUnintendedEvaluationAndUnsupportedExpressions(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		body     string
		expected error
		detail   string
	}{
		// A subport sharing the revision line moves with it, as revbump's
		// help says (batch 41); one whose revision follows it otherwise
		// is still refused.
		{"revision 4\nsubport fixture-child {\n revision [expr {${revision} * 2}]\n}\n", editprep.ErrFidelity, "fixture-child.revision"},
		{"revision 4\ndescription revision=${revision}\n", editprep.ErrFidelity, "description changed"},
		{"revision [expr {2+2}]\n", editprep.ErrUnsupported, "matching literal"},
		{"revision 4\nif {${revision} == 5} { error {candidate evaluation fails} }\n", nil, "candidate evaluation fails"},
	} {
		t.Run(fixture.detail, func(t *testing.T) {
			service, request := preparationFixture(t, fixture.body)
			result, err := service.Prepare(t.Context(), request)
			require.ErrorContains(t, err, fixture.detail)
			if fixture.expected != nil {
				require.ErrorIs(t, err, fixture.expected)
			}
			require.Empty(t, result.PreparedTree)
			require.Empty(t, result.Commits)
		})
	}
}

func TestPreparationRejectsInconsistentSourceAndCancellation(t *testing.T) {
	t.Parallel()
	service, request := preparationFixture(t, "revision 0\n")
	request.Source.Tree = model.ObjectID(strings.Repeat("a", 40))
	_, err := service.Prepare(t.Context(), request)
	require.ErrorContains(t, err, "source commit and tree disagree")
	request.Action = model.EditUpdate
	_, err = service.Prepare(t.Context(), request)
	require.ErrorContains(t, err, "resolved release is required")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Prepare(ctx, request)
	require.ErrorIs(t, err, context.Canceled)
}
