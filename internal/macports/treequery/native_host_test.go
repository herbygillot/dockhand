package treequery

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestArchivePlanUsesNativeHostRead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := workspace.Yours(t.Context(), "treequery native host test")
	portfile := `PortSystem 1.0
name fixture
version 1.0
categories devel
license MIT
homepage https://example.invalid
description fixture
long_description fixture
master_sites https://example.invalid/releases/
distfiles fixture.tar.gz
checksums rmd160 0123456789abcdef0123456789abcdef01234567 sha256 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
set host_read [file exists /opt/dockhand-absent/sdk]
`
	testsupport.WriteFiles(t, root, map[string]string{"devel/fixture/Portfile": portfile})
	testsupport.Git(t, root, "init", "-q")
	testsupport.Git(t, root, "add", "devel/fixture/Portfile")
	testsupport.Git(t, root, "commit", "-q", "-m", "fixture")
	tree := testsupport.Git(t, root, "rev-parse", "HEAD^{tree}")

	repo, err := git.Open(ctx, root, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)
	ports := &selection.Reader{Evaluator: &eval.Evaluator{
		Executable: testsupport.MacPortsTclsh(t),
		Adapter:    testsupport.BaseAdapter(),
	}}
	reader := New(repo, ports, &workspace.Registry{})
	_, plan, err := reader.ArchivePlan(ctx, model.Source{Tree: model.ObjectID(tree)}, "devel/fixture", "")
	require.NoError(t, err)
	require.Len(t, plan, 1)
	require.Equal(t, "fixture.tar.gz", plan[0].Name)
}
