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

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
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
	searchHit string // JSON array for the open-pulls walk
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
	// The duplicate check walks `pulls?state=open`, paged. One page
	// shorter than the page size is the last page, so a single answer
	// ends the walk and the fake needs no page bookkeeping — the paging
	// itself is exercised in internal/gh, against the seam that does it.
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "/pulls?state=open"):
		if g.searchHit == "" {
			return "[]", nil
		}
		return g.searchHit, nil
	case args[0] == "pr" && args[1] == "create":
		return g.createURL + "\n", nil
	case args[0] == "pr" && args[1] == "edit":
		return "", nil
	}
	return "", fmt.Errorf("ghFake: unscripted call %v", args)
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
	// A version is set because the body signs off with one, and the
	// only thing proving promote hands its own over is a body carrying
	// it out the far end.
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: tools, Out: &out, Err: &errb, Gh: gh.run,
		Version: "1.2.3", Verifier: realVMProvider(tools)}
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
	assert.Contains(t, body, "Automated by [dockhand](https://github.com/herbygillot/dockhand) 1.2.3",
		"the body names the build that wrote it")
	assert.Contains(t, creates[0], "jq: update to 1.8", "the title is the minted commit's subject")
	assert.Equal(t, "herby", repo.TrackedRemote(context.Background(), "dockhand/jq-1.8"))
}

func TestPromoteRefusesADuplicate(t *testing.T) {
	repo, _ := promoteRepo(t)
	gh := &ghFake{login: "herbygillot",
		searchHit: `[{"number":123,"title":"jq: update to 1.8","state":"open","html_url":"https://x/123"}]`}
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
	assert.Contains(t, err.Error(), "dockhand cycle", "the remedy names the verb that retires it (D27)")
}

func TestPromoteMidVerificationCancelsAndProceeds(t *testing.T) {
	// The user's ruling on the assessment's "closed evidence states":
	// a promote issued mid-verification IS the answer about the
	// running build. Cancel with a warning, promote, and the PR reads
	// as unverified — no --no-verify demanded on top.
	//
	// Amended 2026-09-04: the evidence is read BEFORE the cancel. A gate
	// that cancelled first would be judging canceled runs its own
	// promotion had just written — and with the dependents best effort,
	// a canceled dependent could then have counted as settled, turning
	// "promote without waiting" into "publish as verified over builds
	// this verb just killed". So the body names what was true when the
	// decision was taken: still running. The cancellation still
	// happens, and stderr still says so.
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
	// The body names the cause as it stood when the evidence was read:
	// the build was still running. It used to say "no verification
	// environment on the submitting machine" here, which was a statement
	// about a machine that had one and had just been told not to wait
	// for it; and until 2026-09-04 it said "canceled", which was a
	// statement about something the promotion itself had just done.
	assert.Contains(t, body, "Testos: verification was still running when this was promoted")
	assert.NotContains(t, body, "fake-9", "the worker's name is local business")

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

// The body names the commit the push actually publishes.
//
// EvidenceFor answers a tip carrying no note of its own with a record
// found over the IDENTICAL TREE at another sha — a message-only amend,
// a rebase — and that sha is reachable from the notes ref and from no
// branch. Printed as "Branch head", it sent a reviewer looking up a
// commit that is not in the pull request, which is the class of
// falsehood this whole step exists to retire.
func TestPromoteNamesThePushedHeadAndNotTheRecordsSha(t *testing.T) {
	repo, noted := promoteRepo(t)
	ctx := context.Background()
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	// The same tree under a different message: the amend that moves the
	// sha and changes nothing a build could notice.
	amended := gittest.Commit(t, repo, "dockhand/jq-amended", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8 (reworded)")
	require.NotEqual(t, noted, amended)
	gittest.MoveBranch(t, repo, "dockhand/jq-1.8", amended)

	gh := &ghFake{login: "herbygillot", createURL: "https://x/pr/3"}
	rs, _, _ := promoteState(t, repo, gh)
	require.NoError(t, promoteAction{target: "dockhand/jq-1.8"}.Execute(ctx, rs))

	creates := gh.called("create")
	require.Len(t, creates, 1)
	body := creates[0][len(creates[0])-1]
	assert.Contains(t, body, "Branch head `"+amended[:12]+"`, verified at `"+noted[:12]+"` (identical tree).",
		"the head is the commit that was pushed; the record's sha says where the verdict was earned")
	assert.Contains(t, body, "Verified with [dockhand]",
		"the same-tree record is what makes this promotion verified at all")
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
	assert.Contains(t, body[len(body)-1],
		"Not verified: no verification environment on the submitting machine, so nothing was run.")
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
	// The body names the cause — this change was never reached — and
	// stops there. The dependency's own name lives in the run's detail
	// prose, which is the local log's words; Blamed is the structured
	// field a body may print, and the ledger writes it only for a
	// blamed port that is a member of the change.
	assert.Contains(t, body, "Not verified:\n  — Testos: blocked before this change was reached.")
	assert.NotContains(t, body, "olm", "the detail's prose is the local user's business")
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

// mergedPRState is a promoted branch whose pull request GitHub says
// merged: the one fact the two verbs answer differently (D27).
func mergedPRState(t *testing.T) (*git.Repo, *runstate.Context, *bytes.Buffer) {
	t.Helper()
	repo, _ := promoteRepo(t)
	require.NoError(t, repo.Push(context.Background(), "herby", "dockhand/jq-1.8"))
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	fake := &verifytest.Fake{}
	rs, out, _ := promoteState(t, repo, gh)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }
	return repo, rs, out
}

// status reports the merged pull request, names the verb that acts on
// it, and leaves the branch where it is.
func TestStatusReportsAMergedPullRequestAndLeavesTheBranch(t *testing.T) {
	repo, rs, out := mergedPRState(t)
	ctx := context.Background()

	require.NoError(t, statusAction{}.Execute(ctx, rs))
	assert.Contains(t, out.String(), "PR #9 merged — `dockhand cycle` retires the branch")
	assert.NotContains(t, out.String(), "discarded")
	assert.True(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "status deletes nothing")
}

// cycle --keep-merged reaches the same verdict, withholds the deletion,
// and says the branch was kept and why; the plain cycle retires it.
func TestCycleKeepMergedWithholdsTheDeletion(t *testing.T) {
	repo, rs, out := mergedPRState(t)
	ctx := context.Background()

	require.NoError(t, cycleAction{keepMerged: true}.Execute(ctx, rs))
	assert.Contains(t, out.String(), "PR #9 merged — kept: --keep-merged")
	assert.True(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "--keep-merged withholds the deletion")

	out.Reset()
	require.NoError(t, cycleAction{}.Execute(ctx, rs))
	assert.Contains(t, out.String(), "discarded dockhand/jq-1.8")
	assert.Contains(t, out.String(), "PR #9 merged — branch cleaned")
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "and without it the branch goes")
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

// The body can be read without publishing it (confirmed at F9,
// 2026-09-04). --body renders what a reviewer would read to stdout and
// does nothing else: no cancellation, no push, no pull request, no audit
// row — and no question to GitHub at all, which the fake proves by
// recording every call it is asked.
func TestPromoteBodyRendersAndPublishesNothing(t *testing.T) {
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	// A run still going on a second platform: promoting proper cancels
	// it, and reading the body must not.
	writeRuns(t, repo, sha, map[string]platRun{"Oldos": runningOn("fake-8")})
	fake := &verifytest.Fake{}
	forge := &ghFake{login: "herbygillot", createURL: "https://x/pr/1"}
	rs, out, errb := promoteState(t, repo, forge)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, promoteAction{target: "jq", body: true}.Execute(ctx, rs))
	body := out.String()
	assert.True(t, strings.HasPrefix(body, "#### Description\n"), "stdout is the body and nothing else:\n%s", body)
	assert.Contains(t, body, "Verified with [dockhand]")
	assert.Contains(t, body, "Testos: linted clean, built in a pristine VM")
	assert.Contains(t, body, "- [ ] checked that there aren't other open [pull requests]",
		"the search did not run, so the box it would have checked is honest about it")
	assert.Contains(t, errb.String(), "note: --body renders the body and publishes nothing")
	assert.Contains(t, errb.String(), "open PRs were not searched")
	assert.NotContains(t, body, "--body", "the note is beside the body, never in it")

	assert.Empty(t, forge.calls, "GitHub is asked nothing — not even whose the fork is")
	assert.Empty(t, repo.TrackedRemote(ctx, "dockhand/jq-1.8"), "nothing was pushed")
	rows, err := ledger.Open(repo).Outcomes(ctx, sha)
	require.NoError(t, err)
	assert.Empty(t, rows, "no audit row: nothing was published")
	assert.Empty(t, fake.Released, "the running verification is not cancelled")
	after, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, after.Runs[record.RunKey("jq", "Oldos")].State,
		"and the record is untouched")
}

// The preview and the publication are one rendering. Promote the branch
// after reading its body: the create call's body differs from what
// --body printed in exactly the box the skipped search would have
// checked, and nowhere else.
func TestPromoteBodyIsTheBodyPromotePublishes(t *testing.T) {
	repo, _ := promoteRepo(t)
	ctx := context.Background()
	forge := &ghFake{login: "herbygillot", createURL: "https://x/pr/1"}
	rs, out, _ := promoteState(t, repo, forge)

	require.NoError(t, promoteAction{target: "jq", body: true}.Execute(ctx, rs))
	preview := out.String()
	out.Reset()
	require.NoError(t, promoteAction{target: "jq"}.Execute(ctx, rs))
	creates := forge.called("create")
	require.Len(t, creates, 1)
	published := creates[0][len(creates[0])-1]

	unchecked := "- [ ] checked that there aren't other open"
	checked := "- [x] checked that there aren't other open"
	assert.Contains(t, preview, unchecked)
	assert.Contains(t, published, checked)
	assert.Equal(t, strings.Replace(preview, unchecked, checked, 1), published)
}

// Nothing bounded the pull request body (the reassessment, 2026-09-04).
// With the cohort cap off a body grows a line per member per platform,
// and GitHub refuses one past 65536 characters — from `gh pr create`,
// after the push. A synthetic cohort large enough to cross the line is
// refused BEFORE the push, in the declined band, naming the size and
// the limit; nothing is trimmed to fit, and nothing leaves the machine.
func TestPromoteRefusesABodyGitHubWouldNotTake(t *testing.T) {
	repo, sha := promoteRepo(t)
	ctx := context.Background()
	require.NoError(t, ledger.Open(repo).Update(ctx, sha, func(r *record.Record) error {
		// Each member passed, so each contributes a verified line and
		// none of them is a warning on stderr: the size is the only thing
		// wrong with this change.
		for i := range 1000 {
			port := fmt.Sprintf("py313-dependent-of-jq-with-a-long-name-%04d", i)
			r.Subjects = append(r.Subjects, record.Subject{Port: port, Names: []string{port}})
			r.Runs[record.RunKey(port, "Testos")] = record.Run{
				State: record.Passed, Platform: "Testos", Linted: true, Lint: "clean"}
		}
		return nil
	}))
	forge := &ghFake{login: "herbygillot", createURL: "https://x/pr/1"}
	rs, out, errb := promoteState(t, repo, forge)

	err := promoteAction{target: "jq"}.Execute(ctx, rs)
	var long *engine.PRBodyTooLongError
	require.ErrorAs(t, err, &long)
	assert.Equal(t, 65536, long.Limit, "GitHub's own number, as gh relays it")
	assert.Greater(t, long.Size, long.Limit)
	assert.Equal(t, exitcode.PlanDeclined, ExitCode(err), "declined: the next move is the author's")
	assert.Equal(t, "pr-body-too-long", long.Code())
	assert.Contains(t, err.Error(), fmt.Sprintf("%d characters", long.Size), "the size is named")
	assert.Contains(t, err.Error(), "at most 65536", "and so is the limit")
	assert.Contains(t, err.Error(), "nothing was pushed")

	assert.Empty(t, repo.TrackedRemote(ctx, "dockhand/jq-1.8"), "refused before the push")
	assert.NotContains(t, errb.String(), "pushed")
	assert.Empty(t, forge.called("create"), "no pull request")
	assert.Empty(t, forge.called("edit"))
	assert.Empty(t, out.String(), "nothing was published, so no URL is printed")
	rows, err := ledger.Open(repo).Outcomes(ctx, sha)
	require.NoError(t, err)
	assert.Empty(t, rows, "no audit row over a publication that did not happen")
}
