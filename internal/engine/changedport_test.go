package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// The devel/pcre shape in miniature: a portdir named for its main
// port, carrying a subport, with the branch changing only the subport.
const subportPortfile = `PortSystem          1.0
name                demo
version             1.0
categories          sysutils
platforms           darwin
maintainers         nomaintainer
description         d
long_description    d
homepage            https://example.org
distfiles

subport demo2 {
    version         2.0
}
`

func subportRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{"sysutils/demo/Portfile": subportPortfile})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	// The branch moves ONLY the subport's version — the change is
	// about demo2, whatever the portdir is called.
	edited := strings.Replace(subportPortfile, "    version         2.0", "    version         2.1", 1)
	sha := gittest.Commit(t, repo, "dockhand/demo2-2.1", primary, "sysutils/demo/Portfile",
		edited, "demo2: update to 2.1")
	return repo, sha
}

func TestChangedPortNamesTheSubportTheBranchMoves(t *testing.T) {
	testenv.PortTclsh(t)
	repo, sha := subportRepo(t)
	name, err := testState(t, repo, nil).changedPort(context.Background(), repo, sha, "sysutils/demo")
	require.NoError(t, err)
	assert.Equal(t, "demo2", name, "the changed context, never the portdir's base name")
}

func TestChangedPortFallsBackWhenNothingEvaluatedMoves(t *testing.T) {
	testenv.PortTclsh(t)
	repo, _ := subportRepo(t)
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	// A change evaluation cannot see — a comment stands in for the
	// files-only patch, which moves no evaluated context either.
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/demo-comment", Base: primary, Path: "sysutils/demo/Portfile",
		Content: append([]byte(subportPortfile), []byte("# touched\n")...), Message: "demo: comment only",
	})
	require.NoError(t, err)
	name, err := testState(t, repo, nil).changedPort(context.Background(), repo, sha, "sysutils/demo")
	require.NoError(t, err)
	assert.Equal(t, "demo", name, "an unevaluatable distinction falls back to the portdir's name")
}
