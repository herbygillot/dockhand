package cmd

// The promote lifecycle, hermetically: a scripted GitHub behind the
// ghOut seam, a bare repo standing in for the fork (its path ends in
// herbygillot/ports, which is all ownerRepoFromURL reads), and the
// same note fixtures the verify lifecycle uses. Everything promote
// decides — duplicate refusal, re-promotion, force refresh, the merged
// dead end — was previously provable only against real GitHub.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// ghFake scripts GitHub's answers and records every call.
type ghFake struct {
	login     string
	ownPRs    string // JSON array for the head-ref lookup
	searchHit string // JSON search/issues document
	createURL string
	calls     [][]string
}

func (g *ghFake) run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	switch {
	case len(args) >= 2 && args[0] == "api" && args[1] == "user":
		return g.login + "\n", nil
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "/pulls?head="):
		if g.ownPRs == "" {
			return "[]", nil
		}
		return g.ownPRs, nil
	case args[0] == "api" && contains(args, "search/issues"):
		if g.searchHit == "" {
			return `{"items":[]}`, nil
		}
		return g.searchHit, nil
	case args[0] == "pr" && args[1] == "create":
		return g.createURL + "\n", nil
	case args[0] == "pr" && args[1] == "edit":
		return "", nil
	}
	return "", fmt.Errorf("ghFake: unscripted call %v", args)
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func (g *ghFake) called(verb string) [][]string {
	var out [][]string
	for _, c := range g.calls {
		if len(c) >= 2 && c[0] == "pr" && c[1] == verb {
			out = append(out, c)
		}
	}
	return out
}

func (g *ghFake) install(t *testing.T) {
	t.Helper()
	real_ := ghOut
	ghOut = g.run
	t.Cleanup(func() { ghOut = real_ })
}

// promoteRepo is a lifecycleRepo with an upstream remote (URL only,
// never contacted) and a pushable bare fork whose path names the
// login as its owner.
func promoteRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	repo, sha := lifecycleRepo(t)
	forkRoot := filepath.Join(t.TempDir(), "herbygillot")
	require.NoError(t, os.MkdirAll(forkRoot, 0o755))
	fork := filepath.Join(forkRoot, "ports")
	out, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, args := range [][]string{
		{"remote", "add", "origin", "https://github.com/macports/macports-ports.git"},
		{"remote", "add", "herby", fork},
	} {
		out, err := exec.Command("git", append([]string{"-C", repo.Root}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	// A passed, linted run makes the branch promotable.
	ctx := context.Background()
	n, err := lifecycle.LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = lifecycle.Run{State: "passed", Linted: true, Lint: "clean"}
	require.NoError(t, lifecycle.WriteNote(ctx, repo, n))
	return repo, sha
}

func promoteState(t *testing.T, repo *git.Repo) (*runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	return &runstate.Context{TreeRoot: repo.Root, Out: &out, Err: &errb}, &out, &errb
}

func TestPromoteOpensThePR(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot", createURL: "https://github.com/macports/macports-ports/pull/999"}
	gh.install(t)
	rs, out, _ := promoteState(t, repo)

	require.NoError(t, promoteAction{target: "jq"}.Execute(context.Background(), rs))
	assert.Contains(t, out.String(), "/pull/999")
	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Testos: linted clean, built in a pristine VM")
	assert.Contains(t, body, "- [x] checked that there aren't other open [pull requests]",
		"a clean search checks the box")
	assert.Contains(t, creates[0], "jq: update to 1.8", "the title is the lifecycle.Minted commit's subject")
	assert.Equal(t, "herby", repo.TrackedRemote(context.Background(), "dockhand/jq-1.8"))
}

func TestPromoteRefusesADuplicate(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		searchHit: `{"items":[{"number":123,"title":"jq: update to 1.8","state":"open","html_url":"https://x/123"}]}`}
	gh.install(t)
	rs, _, _ := promoteState(t, repo)

	err := promoteAction{target: "jq"}.Execute(context.Background(), rs)
	var dup *DuplicatePRError
	require.ErrorAs(t, err, &dup)
	assert.Empty(t, repo.TrackedRemote(context.Background(), "dockhand/jq-1.8"),
		"a refused promotion pushes nothing")
	assert.Empty(t, gh.called("create"))
}

func TestPromoteRePromotionUpdatesInPlace(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":77,"state":"open","html_url":"https://x/77","title":"jq: update to 1.8"}]`}
	gh.install(t)
	rs, out, errb := promoteState(t, repo)

	require.NoError(t, promoteAction{target: "jq"}.Execute(context.Background(), rs))
	assert.Contains(t, errb.String(), "PR #77 already open for this branch; the push updated it")
	assert.Contains(t, out.String(), "https://x/77")
	assert.Empty(t, gh.called("create"), "re-promotion never opens a second PR")
	assert.Empty(t, gh.called("edit"))
}

func TestPromoteForceRefreshesTitleAndBody(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":77,"state":"open","html_url":"https://x/77","title":"jq: update to 1.7"}]`}
	gh.install(t)
	rs, _, errb := promoteState(t, repo)

	require.NoError(t, promoteAction{target: "jq", force: true}.Execute(context.Background(), rs))
	assert.Contains(t, errb.String(), "force-pushed")
	assert.Contains(t, errb.String(), "PR #77 replaced")
	edits := gh.called("edit")
	require.Len(t, edits, 1)
	assert.Contains(t, edits[0], "77")
	assert.Contains(t, edits[0], "jq: update to 1.8", "the stale title refreshes")
}

func TestPromoteMergedPRIsADeadEnd(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":50,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/50"}]`}
	gh.install(t)
	rs, _, _ := promoteState(t, repo)

	err := promoteAction{target: "jq"}.Execute(context.Background(), rs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already merged")
	assert.Contains(t, err.Error(), "dockhand clean")
}
