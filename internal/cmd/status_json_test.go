package cmd

// Fixtures for cmd-level lifecycle-adjacent tests: the ports-tree-shaped
// repo with one minted dockhand branch that lifecycle's own tests start
// from, built over gittest so the two packages share one fixture.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// lifecycleRepo is a ports-tree-shaped git repo with one dockhand
// branch minted, its tip returned alongside.
func lifecycleRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, testFinder())
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	return repo, sha
}

func TestStatusJSONReportsTheSettledTruth(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	writeRuns(t, repo, sha, map[string]lifecycle.Run{"Testos": {State: "running",
		Job: verify.Job{Provider: "fake", ID: "fake-1"}, Linted: true}})

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
	gittest.BareFork(t, repo, "herbygillot", "herby")
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
