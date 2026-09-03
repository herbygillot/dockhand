package cmd

// The promote lifecycle, hermetically: a scripted GitHub behind the
// gh.RealGhOut seam, a bare repo standing in for the fork (its path ends in
// herbygillot/ports, which is all gh.OwnerRepoFromURL reads), and the
// same note fixtures the verify lifecycle uses. Everything promote
// decides — duplicate refusal, re-promotion, force refresh, the merged
// dead end — was previously provable only against real GitHub.

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
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
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

// promoteRepo is a lifecycleRepo with an upstream remote (URL only,
// never contacted) and a pushable bare fork whose path names the
// login as its owner.
func promoteRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	repo, sha := lifecycleRepo(t)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	// A passed, linted run makes the branch promotable.
	writeRuns(t, repo, sha, map[string]platRun{"Testos": passedOn("fake-passed")})
	return repo, sha
}

func promoteState(t *testing.T, repo *git.Repo, gh *ghFake) (*runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	tools := testFinder()
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: tools, Out: &out, Err: &errb, Gh: gh.run,
		Verifier: realVMProvider(tools)}
	return rs, &out, &errb
}

func TestPromoteOpensThePR(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot", createURL: "https://github.com/macports/macports-ports/pull/999"}
	rs, out, _ := promoteState(t, repo, gh)

	require.NoError(t, promoteAction{target: "jq"}.Execute(context.Background(), rs))
	assert.Contains(t, out.String(), "/pull/999")
	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Testos: linted clean, built in a pristine VM")
	assert.Contains(t, body, "- [x] checked that there aren't other open [pull requests]",
		"a clean search checks the box")
	assert.Contains(t, creates[0], "jq: update to 1.8", "the title is the minted commit's subject")
	assert.Equal(t, "herby", repo.TrackedRemote(context.Background(), "dockhand/jq-1.8"))
}

func TestPromoteRefusesADuplicate(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		searchHit: `{"items":[{"number":123,"title":"jq: update to 1.8","state":"open","html_url":"https://x/123"}]}`}
	rs, _, _ := promoteState(t, repo, gh)

	err := promoteAction{target: "jq"}.Execute(context.Background(), rs)
	var dup *verdict.DuplicatePRError
	require.ErrorAs(t, err, &dup)
	assert.Empty(t, repo.TrackedRemote(context.Background(), "dockhand/jq-1.8"),
		"a refused promotion pushes nothing")
	assert.Empty(t, gh.called("create"))
}

func TestPromoteRePromotionUpdatesInPlace(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":77,"state":"open","html_url":"https://x/77","title":"jq: update to 1.8"}]`}
	rs, out, errb := promoteState(t, repo, gh)

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
	rs, _, errb := promoteState(t, repo, gh)

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
	rs, _, _ := promoteState(t, repo, gh)

	err := promoteAction{target: "jq"}.Execute(context.Background(), rs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already merged")
	assert.Contains(t, err.Error(), "dockhand clean")
}

func TestPromoteMidVerificationCancelsAndProceeds(t *testing.T) {
	// The user's ruling on the assessment's "closed evidence states":
	// a promote issued mid-verification IS the answer about the
	// running build. Cancel with a warning, promote, and the PR reads
	// as unverified — no --no-verify demanded on top.
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	// The passed run is replaced outright: this branch is mid-build and
	// nothing about it has settled.
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	n.Jobs, n.Runs = nil, nil
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("fake-9")})

	fake := &verifytest.Fake{}
	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/1"}
	rs, _, errb := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	assert.Equal(t, []string{"fake-9"}, fake.Released, "the running worker is released, not abandoned")
	assert.Contains(t, errb.String(), "canceled 1 running verification(s)")
	assert.Contains(t, errb.String(), "promoting unverified; the PR will say so")

	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Not locally verified", "the PR only says verified or not")
	assert.NotContains(t, body, "canceled", "local state is the local user's business")

	after, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Canceled, after.Runs[record.RunKey("jq", "Testos")].State,
		"the note stays honest locally")
}

func TestPromoteMidVerificationKeepsThePassedEvidence(t *testing.T) {
	repo, sha := promoteRepo(t) // fixture already records a passed, linted run
	ctx := context.Background()
	writeRuns(t, repo, sha, map[string]platRun{"Oldos": runningOn("fake-8")})

	fake := &verifytest.Fake{}
	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/2"}
	rs, _, errb := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	assert.Equal(t, []string{"fake-8"}, fake.Released)
	assert.Contains(t, errb.String(), "canceled 1 running verification(s)")

	body := gh.called("create")[0]
	joined := body[len(body)-1]
	assert.Contains(t, joined, "Testos: linted clean, built in a pristine VM",
		"the surviving evidence still speaks")
	assert.NotContains(t, joined, "Oldos", "the canceled run never reaches the PR")
}

func TestPromoteUnverifiedComplainsAndProceeds(t *testing.T) {
	// The friction ruling, complete: an unverified branch promotes with
	// a complaint — invoking promote IS the publication choice — and
	// only a completed FAILED build still refuses without --no-verify.
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	n.Jobs, n.Runs = nil, nil
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/3"}
	rs, _, errb := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return &verifytest.Fake{}, nil }

	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	assert.Contains(t, errb.String(), "promoting unverified; the PR will say so")
	body := gh.called("create")[0]
	assert.Contains(t, body[len(body)-1], "Not locally verified")
}

// A blocked run sits on the unverified side of the gate: the change
// is untested, not disproven, so it promotes with the neighbor's name
// in front of the maintainer — no --no-verify demanded.
func TestPromoteBlockedPromotesWithTheDependencyNamed(t *testing.T) {
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	n.Jobs, n.Runs = nil, nil
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	writeRuns(t, repo, sha, map[string]platRun{"Testos": {Job: spentGuest("fake-blocked"),
		Run: record.Run{State: record.Blocked,
			Detail: "dependency olm (nomaintainer) fails to build; the change itself is untested"}}})

	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/1"}
	rs, _, errb := promoteState(t, repo, gh)

	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	assert.Contains(t, errb.String(), "verification blocked on Testos: dependency olm (nomaintainer) fails to build")
	assert.Contains(t, errb.String(), "promoting unverified; the PR will say so")

	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Not locally verified")
	assert.NotContains(t, body, "olm", "local state is the local user's business")
}

func TestPromoteStillRefusesAFailedBuild(t *testing.T) {
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	// The passed run is replaced outright by a failure whose guest is
	// still standing as the debug handle.
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	n.Jobs, n.Runs = nil, nil
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	writeRuns(t, repo, sha, map[string]platRun{"Testos": keptOn("kept-1", "")})

	gh := &ghFake{login: "herbygillot"}
	rs, _, _ := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return &verifytest.Fake{}, nil }

	err = promoteAction{target: "jq"}.Execute(ctx, rs)
	require.Error(t, err, "a failed build is negative evidence, not absence")
	assert.Contains(t, err.Error(), "failed verification")
	assert.Empty(t, gh.called("create"))
}

func TestStatusNoCleanReportsWithoutDeleting(t *testing.T) {
	repo, _ := promoteRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	fake := &verifytest.Fake{}
	rs, out, _ := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, statusAction{noClean: true}.Execute(ctx, rs))
	assert.Contains(t, out.String(), "PR #9 merged — `dockhand clean` removes the branch")
	assert.True(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "--no-clean withholds the deletion")
}

// Ruling 5's other half. A ticket named at promote time reaches the
// pull request body and nothing else: the commit was written at mint
// and is not rewritten, so the checklist box that asks for the ticket
// in the COMMIT message stays unchecked, and the user is told why once,
// at the point they typed the flag.
func TestPromoteClosesReachesTheBodyAndSaysSo(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot", createURL: "https://github.com/macports/macports-ports/pull/999"}
	rs, _, errb := promoteState(t, repo, gh)

	require.NoError(t, promoteAction{target: "jq", closes: "71234"}.Execute(context.Background(), rs))
	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Closes: https://trac.macports.org/ticket/71234")
	assert.Contains(t, body, "- [ ] referenced existing tickets on [Trac]",
		"the box asks about the commit message, which this ticket never reached")
	assert.True(t, strings.HasPrefix(errb.String(), promoteClosesNote),
		"said once, first, at the point the flag was typed:\n%s", errb.String())
}

// A change planned with --closes carries the number in its record, so
// the body cites it without the promoter retyping it — and without the
// note, because nothing here is late.
func TestPromoteCitesTheTicketTheMintRecorded(t *testing.T) {
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	l := ledger.Open(repo)
	require.NoError(t, l.Update(ctx, sha, func(r *record.Record) error {
		r.ClosesTicket = "71234"
		return nil
	}))
	gh := &ghFake{login: "herbygillot", createURL: "https://github.com/macports/macports-ports/pull/999"}
	rs, _, errb := promoteState(t, repo, gh)

	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Closes: https://trac.macports.org/ticket/71234")
	assert.Contains(t, body, "- [x] referenced existing tickets on [Trac]",
		"the trailer is in the commit this record was born from, so the box says so")
	assert.NotContains(t, errb.String(), "--closes", "nothing was late, so nothing is said")
}
