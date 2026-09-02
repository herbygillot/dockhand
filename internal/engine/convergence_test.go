package engine

// Promoting one branch twice must reach the pull request the first
// promotion opened, not open a second one. Nothing stores that link:
// it is derived from the head ref every time (D21), so the proof is a
// GitHub that answers head-ref lookups the way the real one does —
// once a pull request exists for herbygillot:dockhand/jq-1.8, it is
// what the query returns.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
)

// convergingGh is a GitHub that remembers: a pull request opened against a
// head ref is returned by every later lookup of that head ref, which is
// the whole mechanism convergence rests on.
type convergingGh struct {
	login string
	// open is the pull request per "owner:branch" head ref.
	open  map[string]int
	next  int
	calls [][]string
}

func newConvergingGh(login string) *convergingGh {
	return &convergingGh{login: login, open: map[string]int{}, next: 100}
}

func (g *convergingGh) run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	switch {
	case args[0] == "api" && len(args) >= 2 && args[1] == "user":
		return g.login + "\n", nil
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "/pulls?head="):
		head := args[1][strings.Index(args[1], "head=")+len("head=") : strings.Index(args[1], "&")]
		num, ok := g.open[head]
		if !ok {
			return "[]", nil
		}
		return fmt.Sprintf(`[{"number":%d,"state":"open","title":"jq: update to 1.8","html_url":"https://x/%d"}]`, num, num), nil
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "search/issues"):
		return `{"items":[]}`, nil
	case args[0] == "pr" && args[1] == "create":
		head := args[headArg(args, "--head")]
		g.next++
		g.open[head] = g.next
		return fmt.Sprintf("https://x/%d\n", g.next), nil
	}
	return "", fmt.Errorf("convergingGh: unscripted call %v", args)
}

func headArg(args []string, flag string) int {
	for i, a := range args {
		if a == flag {
			return i + 1
		}
	}
	return 0
}

func (g *convergingGh) called(verb string) int {
	n := 0
	for _, c := range g.calls {
		if len(c) >= 2 && c[0] == "pr" && c[1] == verb {
			n++
		}
	}
	return n
}

// promotableRepo is an engineRepo with the two remotes a promotion
// needs and a passed run on its tip.
func promotableRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	repo, sha := engineRepo(t)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "passed", Linted: true, Lint: "clean"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	return repo, sha
}

func TestPromotingTwiceConvergesOnOnePR(t *testing.T) {
	repo, sha := promotableRepo(t)
	ctx := context.Background()
	gh := newConvergingGh("herbygillot")

	var out, errb bytes.Buffer
	eng := testEngine(t, repo, nil, &out, &errb)
	eng.Gh = gh.run

	require.NoError(t, eng.Promote(ctx, repo, "jq", PromoteOpts{}))
	require.NoError(t, eng.Promote(ctx, repo, "jq", PromoteOpts{}))

	assert.Equal(t, 1, gh.called("create"), "the second promotion finds the first one's PR")
	assert.Equal(t, "https://x/101\nhttps://x/101\n", out.String(),
		"both promotions name the same pull request")
	assert.Contains(t, errb.String(), "PR #101 already open for this branch; the push updated it")

	// And the audit read the same number both times: two publications
	// of one change, not one publication and one mystery.
	rows, err := ledger.Open(repo).Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 2, "asking for review twice is two publications")
	for _, row := range rows {
		assert.Equal(t, 101, row.PRNumber)
		assert.Equal(t, "dockhand/jq-1.8", row.Branch)
		assert.Equal(t, record.Human, row.AskedBy)
		assert.Equal(t, record.Verified, row.Evidence)
	}
}

// A push with no pull request is still a publication: the change is on
// the fork where somebody can be pointed at it, and the audit says so
// with no number.
func TestPromoteWithNoPRRecordsThePublicationAnyway(t *testing.T) {
	repo, sha := promotableRepo(t)
	ctx := context.Background()
	gh := newConvergingGh("herbygillot")

	var out, errb bytes.Buffer
	eng := testEngine(t, repo, nil, &out, &errb)
	eng.Gh = gh.run

	require.NoError(t, eng.Promote(ctx, repo, "jq", PromoteOpts{NoPR: true}))
	assert.Zero(t, gh.called("create"))

	rows, err := ledger.Open(repo).Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Zero(t, rows[0].PRNumber, "no pull request, no number to claim")
	assert.True(t, rows[0].Open(), "the opening row awaits an outcome")
}
