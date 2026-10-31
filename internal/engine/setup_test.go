package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestSetupPutsWorktreesWhereThePersonSaid(t *testing.T) {
	t.Parallel()
	f := setup(t)
	unconfigured := f.options
	unconfigured.Worktrees = ""
	e, report, err := Setup(t.Context(), unconfigured, "")
	require.NoError(t, err)
	require.Equal(t, e.DefaultWorktrees(), report.Worktrees)
	require.True(t, report.Unchosen, "nobody said where, so setup may ask")
	require.Empty(t, report.Remember)
	require.Equal(t, e.Clone(), report.Checkout)
	require.Equal(t, UpstreamRepository, report.Upstream.Repository)
	require.NotEmpty(t, report.Git.Version)
	require.Equal(t, f.options.Database, report.Records)
	chosen := filepath.Join(t.TempDir(), "branches")
	report.Choose(chosen)
	require.Equal(t, chosen, report.Worktrees)
	require.Equal(t, chosen, report.Remember, "an answer is kept")
	e.Close()

	configured := unconfigured
	configured.Worktrees = chosen
	e, report, err = Setup(t.Context(), configured, "")
	require.NoError(t, err)
	require.Equal(t, chosen, report.Worktrees)
	require.False(t, report.Unchosen)
	require.Empty(t, report.Remember, "the configuration keeps it already")
	e.Close()

	named := filepath.Join(t.TempDir(), "elsewhere")
	e, report, err = Setup(t.Context(), configured, named)
	require.NoError(t, err)
	defer e.Close()
	require.Equal(t, named, report.Worktrees, "a named directory wins over the configured one")
	require.Equal(t, named, report.Remember)
}

func TestSetupRefusesAGitThatCantRunBeforeOpening(t *testing.T) {
	t.Parallel()
	f := setup(t)
	options := f.options
	options.Git = filepath.Join(t.TempDir(), "no-git")
	e, report, err := Setup(t.Context(), options, "")
	require.ErrorContains(t, err, "can't be run")
	require.Nil(t, e)
	require.Empty(t, report.Checkout)
}

// setup says whether go2port and cargo2port are there, as the edits that
// run them find them, so a missing one is said before an update needs it
// (the docs dig's I38).
func TestSetupSaysWhetherTheVendoringToolsAreThere(t *testing.T) {
	t.Parallel()
	helper := filepath.Join(t.TempDir(), "go2port")
	testsupport.WriteExecutable(t, helper, "#!/bin/sh\n")
	found := vendoringTools(depblock.Tools{Go2Port: helper, Cargo2Port: filepath.Join(t.TempDir(), "cargo2port")})
	require.Equal(t, []SetupTool{{Name: "go2port", Path: helper, For: "Go ports' go.vendors"}, {Name: "cargo2port", For: "Rust ports' cargo.crates"}}, found)
}
