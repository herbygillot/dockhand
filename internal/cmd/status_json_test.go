package cmd

// Fixtures for cmd-level lifecycle-adjacent tests: a ports-tree-shaped
// repo with one minted dockhand branch, mirroring lifecycle's own test
// fixture (helpers cannot cross package test boundaries).

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func lifecycleRepo(t *testing.T) (*git.Repo, string) {
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sysutils", "jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sysutils", "jq", "Portfile"), []byte("version 1.7\n"), 0o644))
	run("add", ".")
	run("commit", "--quiet", "-m", "initial tree")

	repo, err := git.Open(context.Background(), testFinder(), dir)
	require.NoError(t, err)
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/jq-1.8", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 1.8\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	return repo, sha
}

func runningNote(t *testing.T, repo *git.Repo, sha, jobID string) lifecycle.Note {
	t.Helper()
	ctx := context.Background()
	n, err := lifecycle.LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = lifecycle.Run{State: "running",
		Job: verify.Job{Provider: "fake", ID: jobID}, Linted: true}
	require.NoError(t, lifecycle.WriteNote(ctx, repo, n))
	return n
}

func TestStatusJSONReportsTheSettledTruth(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	require.NoError(t, statusAction{json: true}.Execute(context.Background(), rs))

	var got statusJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	require.Len(t, got.Branches, 1)
	b := got.Branches[0]
	assert.Equal(t, "dockhand/jq-1.8", b.Branch)
	assert.Equal(t, sha, b.Tip)
	require.NotNil(t, b.Note)
	assert.Equal(t, "passed", b.Note.Runs["Testos"].State, "the JSON mode settles, same as the human one")
	assert.Equal(t, "clean", b.Note.Runs["Testos"].Lint)
	assert.Nil(t, b.PR, "an unpromoted branch carries no PR object")
	assert.False(t, b.Cleaned)
}

func TestStatusJSONKeepsStdoutPureUnderAutoclean(t *testing.T) {
	// A merged-PR autoclean fires mid---json; its prose must land on
	// stderr, never inside the document. Field-measured breakage.
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	_ = sha

	// Promote-shape the branch: a tracked remote is what makes judge
	// look the PR up.
	forkRoot := filepath.Join(t.TempDir(), "herbygillot")
	require.NoError(t, os.MkdirAll(forkRoot, 0o755))
	fork := filepath.Join(forkRoot, "ports")
	out0, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out0)
	out0, err = exec.Command("git", "-C", repo.Root, "remote", "add", "origin", "https://github.com/macports/macports-ports.git").CombinedOutput()
	require.NoError(t, err, "%s", out0)
	out0, err = exec.Command("git", "-C", repo.Root, "remote", "add", "herby", fork).CombinedOutput()
	require.NoError(t, err, "%s", out0)
	require.NoError(t, repo.Push(context.Background(), "herby", "dockhand/jq-1.8"))

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb, Gh: gh.run,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	require.NoError(t, statusAction{json: true}.Execute(context.Background(), rs))

	var got statusJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	require.Len(t, got.Branches, 1)
	assert.True(t, got.Branches[0].Cleaned)
	assert.Contains(t, errb.String(), "discarded", "the autoclean's prose lands on stderr")
}
