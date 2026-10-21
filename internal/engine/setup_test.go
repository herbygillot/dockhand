package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupPutsWorktreesWhereThePersonSaid(t *testing.T) {
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
	f := setup(t)
	options := f.options
	options.Git = filepath.Join(t.TempDir(), "no-git")
	e, report, err := Setup(t.Context(), options, "")
	require.ErrorContains(t, err, "can't be run")
	require.Nil(t, e)
	require.Empty(t, report.Checkout)
}
