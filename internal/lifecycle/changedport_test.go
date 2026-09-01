package lifecycle

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
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
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet")
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sysutils", "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sysutils", "demo", "Portfile"), []byte(subportPortfile), 0o644))
	run("add", ".")
	run("commit", "--quiet", "-m", "initial tree")

	repo, err := git.Open(context.Background(), dir)
	require.NoError(t, err)
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	// The branch moves ONLY the subport's version — the change is
	// about demo2, whatever the portdir is called.
	edited := bytes.Replace([]byte(subportPortfile),
		[]byte("    version         2.0"), []byte("    version         2.1"), 1)
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/demo2-2.1", Base: primary, Path: "sysutils/demo/Portfile",
		Content: edited, Message: "demo2: update to 2.1",
	})
	require.NoError(t, err)
	return repo, sha
}

func TestChangedPortNamesTheSubportTheBranchMoves(t *testing.T) {
	testenv.PortTclsh(t)
	repo, sha := subportRepo(t)
	var buf bytes.Buffer
	rs := &runstate.Context{Out: &buf, Err: &buf}

	name, err := ChangedPort(context.Background(), rs, repo, sha, "sysutils/demo")
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
	var buf bytes.Buffer
	rs := &runstate.Context{Out: &buf, Err: &buf}

	name, err := ChangedPort(context.Background(), rs, repo, sha, "sysutils/demo")
	require.NoError(t, err)
	assert.Equal(t, "demo", name, "an unevaluatable distinction falls back to the portdir's name")
}
