package engine

// The machine publish gate, and the argument that it dominates every
// path to ring 3.
//
// EVERY TEST HERE IS A REFUSAL TEST. There is no test that an unattended
// publication works, because on this build it does not and must not: the
// permission is a build-time constant, it is false, and what is proven
// here is that nothing gets past it. When the trust ladder's numbers
// arrive and the constant flips, these tests keep their meaning — they
// are written against the permission as a value, so the same table drives
// both sides of it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// THE RULING: a build-time constant guards every publish whose invoker
// is a machine, and it is false on this build.
//
// The gate is a pure function over two values, so the table drives both
// sides of the permission: what a granted build would allow is stated
// here, unreached by any binary, so that flipping the constant is a
// change to one line and not to an argument.
func TestTheMachineGateRefusesOnThisBuild(t *testing.T) {
	cases := []struct {
		name      string
		by        record.Driver
		permitted bool
		refused   bool
	}{
		{"a machine on this build", record.Machine, false, true},
		{"a machine on a build that granted it", record.Machine, true, false},
		{"a person on this build", record.Human, false, false},
		{"a person on a build that granted it", record.Human, true, false},
		// The zero driver is a person: every verb dockhand has is one.
		{"an unset invoker", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := GateRing3(tc.by, tc.permitted)
			if !tc.refused {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var refusal *MachineDisabledError
			require.ErrorAs(t, err, &refusal)
			assert.Equal(t, exitcode.MachineGate, refusal.DockhandExit())
			assert.Equal(t, "refused", exitcode.Family(refusal.DockhandExit()))
			assert.Equal(t, "machine-publish-disabled", refusal.Code())
			assert.Contains(t, refusal.Error(), "dockhand promote",
				"a refusal names the road that is open")
		})
	}
}

// THE RULING: the permission's zero value is the refusal.
//
// This is the whole of the domination argument's foundation. The engine
// cannot read cmd's constant — the layering forbids it — so the
// permission arrives as a value, and every engine built by every test,
// every future composition root and every caller who never heard of the
// field gets false because false is what a bool is when nobody set it.
func TestAnEngineNobodyGrantedAnythingRefusesTheMachine(t *testing.T) {
	e := New(Deps{})
	assert.False(t, e.MachinePublish, "the zero Deps grants nothing")
	require.Error(t, GateRing3(record.Machine, e.MachinePublish))

	// And the field is named for the permission GRANTED. A
	// NoMachinePublish would have made the zero value permissive, which
	// is the inversion this test exists to make impossible to introduce
	// quietly: it would have to rename the field, and the rename would
	// fail here.
	granted := New(Deps{MachinePublish: true})
	require.NoError(t, GateRing3(record.Machine, granted.MachinePublish))
}

// THE RULING: a machine's promote is refused BEFORE any side effect.
//
// Not merely refused — refused first. A machine that reached the verb
// reads no note, cancels no running verification and spends no gh call,
// so the refusal it gets names the gate that is actually operating
// rather than a complaint about its evidence.
func TestAMachinePromoteRefusesBeforeItTouchesAnything(t *testing.T) {
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	// A gh seam that fails the test if anything reaches it. The refusal
	// must come before the fork remote is resolved, which is the first
	// gh call a promotion makes.
	e.Gh = func(context.Context, ...string) (string, error) {
		t.Fatal("a refused machine promote must not reach the forge")
		return "", nil
	}
	// A running verification on the tip: the line a machine must never
	// execute is the cancellation, and this is what it would cancel.
	n := runningNote(t, repo, sha, "job-1")

	err := e.Promote(context.Background(), repo, "dockhand/jq-1.8", PromoteOpts{Invoker: record.Machine})
	require.Error(t, err)
	var refusal *MachineDisabledError
	require.ErrorAs(t, err, &refusal, "the build gate is what refuses, not the evidence")

	// The run is untouched. A machine never cancels.
	after := mustRead(t, repo, sha)
	assert.Equal(t, record.Running, after.Runs[record.RunKey("jq", "Testos")].State,
		"a machine never cancels a running verification")
	assert.Equal(t, n.Runs[record.RunKey("jq", "Testos")].State,
		after.Runs[record.RunKey("jq", "Testos")].State)
}

// THE RULING: a machine never cancels, stated over the permission rather
// than over the constant.
//
// The gate above already stops a machine on this build, so this test
// grants the permission and shows the cancellation is STILL not reached.
// Without it, "a machine never cancels" would be an accident of the
// constant being false, and flipping the constant would quietly hand the
// unattended pass a way to kill every build on the machine.
func TestAMachineNeverCancelsEvenWhereItMayPublish(t *testing.T) {
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true
	e.Gh = func(_ context.Context, args ...string) (string, error) {
		// The promotion gets as far as resolving the fork; it must not get
		// past it, and it must not have cancelled anything on the way.
		return "", errors.New("no forge in this test")
	}
	runningNote(t, repo, sha, "job-1")

	require.Error(t, e.Promote(context.Background(), repo, "dockhand/jq-1.8",
		PromoteOpts{Invoker: record.Machine, NoPRCheck: true}))

	after := mustRead(t, repo, sha)
	assert.Equal(t, record.Running, after.Runs[record.RunKey("jq", "Testos")].State,
		"the running verification survives a machine's promotion attempt")
}

// THE RULING: --no-verify, --no-pr-check and --force are a person's
// overrides.
//
// They are not refused on the machine road, they are not honoured. A
// caller that set them by accident cannot argue with a flag that was
// never read.
//
// force is here for a stronger reason than the other two: it selects a
// with-lease force-push over a branch backing an open review and a
// retitle of that review. No caller sets it on the machine side today,
// and reading it through a method is what keeps that a property of the
// TYPE rather than of every future call site remembering.
func TestTheOverridesAreUnreachableFromTheMachineRoad(t *testing.T) {
	o := PromoteOpts{NoVerify: true, NoPRCheck: true, Force: true}
	assert.True(t, o.noVerify(), "a person's override stands")
	assert.True(t, o.noPRCheck())
	assert.True(t, o.force())

	o.Invoker = record.Machine
	assert.False(t, o.noVerify(), "the machine road has nobody to mean it")
	assert.False(t, o.noPRCheck(), "and a rate-limited pass must not skip the duplicate check")
	assert.False(t, o.force(), "and nothing on that road force-pushes over an open review")

	// And the raw field is read nowhere: three call sites, all of them
	// through the narrowing method. A fourth that spelled o.Force would be
	// the machine road acquiring a force-push by inattention.
	promote := compiledLines(string(readSource(t, "promote.go")))
	assert.Equal(t, 1, countMatching(promote, regexp.MustCompile(`\bo\.Force\b`)),
		"the raw field is read once, inside force(), which is what narrows it to a person")
	assert.Equal(t, 1, countMatching(promote, regexp.MustCompile(`func \(o PromoteOpts\) force\(\) bool\s+\{ return o\.Force && `)),
		"and that one read is the narrowing itself")
	assert.Equal(t, 3, countMatching(promote, regexp.MustCompile(`o\.force\(\)`)),
		"the push, the no-PR push and the PR refresh")
}

// THE RULING: a machine never republishes over an open pull request.
//
// The slot decides this a phase earlier and calls it work already done.
// What this pins is the FUNNEL: a machine that reached the verb anyway
// is refused before the push, not after it. Read in source order the
// convergence arm below the push updates an open review's head and then
// prints a note saying so, which is right for a person and is a machine
// force-updating somebody's review on nobody's authority.
//
// The permission is granted so the property is the guard and not an
// accident of the constant being false.
func TestAMachineIsRefusedOverAnOpenPullRequestBeforeItPushes(t *testing.T) {
	ctx := context.Background()
	repo, _ := promotableRepo(t)
	forge := newConvergingGh("herbygillot")
	// A pull request already open for this branch's head ref, opened by
	// somebody or by an earlier pass. The machine must not touch it.
	forge.open["herbygillot:dockhand/jq-1.8"] = 7

	var out, errb bytes.Buffer
	e := testEngine(t, repo, nil, &out, &errb)
	e.MachinePublish = true
	e.Gh = forge.run

	err := e.Promote(ctx, repo, "dockhand/jq-1.8", PromoteOpts{Invoker: record.Machine})
	var refusal *MachineRepublishError
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, exitcode.MachineGate, refusal.DockhandExit())
	assert.Equal(t, "machine-republish", refusal.Code())
	assert.Contains(t, refusal.Error(), "dockhand promote")

	assert.Zero(t, forge.called("edit"), "the open review is not retitled")
	assert.Zero(t, forge.called("create"), "and no second one is opened")
	assert.Empty(t, out.String(), "nothing was published, so no URL is printed")
	assert.NotContains(t, errb.String(), "pushed", "the refusal is BEFORE the push")

	// And a person over the same branch converges on it, which is the
	// behaviour this refusal is carving the machine out of.
	require.NoError(t, e.Promote(ctx, repo, "dockhand/jq-1.8", PromoteOpts{}))
	assert.Contains(t, errb.String(), "PR #7 already open for this branch; the push updated it")
}

// A machine promoting twice converges on the pull request the first
// promotion opened, exactly as a person's does.
//
// The gate list names this: the convergence property is reached by two
// different routes on the two roads — the slot refuses a published phase
// as a no-op, and the verb derives the link from the head ref — so the
// only way to know a GRANTED machine converges rather than opening a
// second pull request is to drive it twice with the permission on.
func TestAMachinePromotingTwiceConvergesOnOnePR(t *testing.T) {
	ctx := context.Background()
	repo, _ := promotableRepo(t)
	forge := newConvergingGh("herbygillot")

	var out, errb bytes.Buffer
	e := testEngine(t, repo, nil, &out, &errb)
	e.MachinePublish = true
	e.Gh = forge.run

	require.NoError(t, e.Promote(ctx, repo, "jq", PromoteOpts{Invoker: record.Machine}))
	assert.Equal(t, 1, forge.called("create"))

	// The second time round the branch's own pull request is open, and the
	// machine is refused rather than updating it. One pull request, and
	// the refusal names it.
	err := e.Promote(ctx, repo, "jq", PromoteOpts{Invoker: record.Machine})
	var refusal *MachineRepublishError
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, 101, refusal.Number)
	assert.Equal(t, 1, forge.called("create"), "the second promotion opens nothing")
	assert.Equal(t, "https://x/101\n", out.String(), "one publication, one URL")
}

// THE RULING: the funnels refuse from below.
//
// A gate above the call sites dominates the tree as it stands. A gate
// INSIDE the two funnels dominates the tree as it will stand, including
// the code written by somebody who never read Promote — which is exactly
// what the publish slot is.
func TestThePushAndForgeFunnelsRefuseAMachineThemselves(t *testing.T) {
	repo, _ := engineRepo(t)
	e := testState(t, repo, nil)
	e.Gh = func(context.Context, ...string) (string, error) {
		t.Fatal("the funnel must refuse before the seam is reached")
		return "", nil
	}
	ctx := context.Background()

	err := e.push(ctx, repo, "fork", "someone", "dockhand/jq-1.8", false, record.Machine)
	var refusal *MachineDisabledError
	require.ErrorAs(t, err, &refusal, "the push funnel refuses a machine on this build")

	_, err = e.publishGh(ctx, record.Machine, "pr", "create", "--repo", "macports/macports-ports")
	require.ErrorAs(t, err, &refusal, "the forge-write funnel refuses a machine on this build")

	// And a person passes both, reaching the seam beneath. The push is
	// asked for a remote that does not exist, so it fails as a push — the
	// point is only that the GATE let it through.
	err = e.push(ctx, repo, "no-such-remote", "someone", "dockhand/jq-1.8", false, record.Human)
	require.Error(t, err)
	assert.NotErrorIs(t, err, refusal, "a person is not refused by the machine gate")
}

// THE RULING (structural): every act that spends ring 3 has exactly one
// call site, and the gate is inside it.
//
// This is what turns domination from a fact about today's call graph into
// an invariant. A push is git.Repo.Push or PushForce; a pull request is
// `gh pr create` or `gh pr edit`. Each may appear in internal/ exactly
// once outside a test, in the funnel that gates it. A second call site
// fails here rather than at review, which matters because writing one is
// the obvious way to build any new publisher.
func TestOnlyOneCallSiteSpendsRingThree(t *testing.T) {
	sites := []struct {
		name    string
		pattern *regexp.Regexp
		// want is file to occurrences. Stated as a map and not as a count
		// so that a second call site fails by NAMING the file that grew
		// one, which is the sentence a reader of the failure needs.
		want map[string]int
	}{
		{
			name:    "a push to the fork",
			pattern: regexp.MustCompile(`\.PushForce\(|\.Push\(`),
			// Two, because the funnel is the with-lease force and the
			// ordinary push, and both are inside Engine.push.
			want: map[string]int{"internal/engine/promote.go": 2},
		},
		{
			name:    "a pull request opened",
			pattern: regexp.MustCompile(`"pr",\s*"create"`),
			want: map[string]int{
				"internal/engine/promote.go": 1,
				// The guard's own table, which NAMES the verb in order to
				// refuse it. A mention in a deny list is the opposite of a
				// call site, and it is pinned here so that adding a verb to
				// that table is a deliberate act rather than a silent one.
				"internal/cmd/machinepublish.go": 1,
			},
		},
		{
			name:    "a pull request edited",
			pattern: regexp.MustCompile(`"pr",\s*"edit"`),
			want: map[string]int{
				"internal/engine/promote.go":     1,
				"internal/cmd/machinepublish.go": 1,
			},
		},
	}
	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			assert.Equal(t, site.want, scanTree(t, site.pattern),
				"%s must be spelled only where it is gated", site.name)
		})
	}
}

// readSource reads one file of this package, for the rules that are
// about what is written rather than about what runs.
func readSource(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	require.NoError(t, err, "the rule is pinned against %s", name)
	return b
}

// countMatching counts the lines a pattern occurs on. Lines and not
// occurrences: the rules here are about a statement being written, and
// one line saying a thing twice is still one place it is said.
func countMatching(lines []string, re *regexp.Regexp) int {
	n := 0
	for _, line := range lines {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// scanTree counts a pattern's occurrences per file across internal/,
// over the lines the compiler sees.
func scanTree(t *testing.T, pattern *regexp.Regexp) map[string]int {
	t.Helper()
	found := map[string]int{}
	require.NoError(t, filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel := "internal/" + strings.TrimPrefix(filepath.ToSlash(path), "../")
		for _, line := range compiledLines(string(src)) {
			if pattern.MatchString(line) {
				found[rel]++
			}
		}
		return nil
	}))
	return found
}

// compiledLines drops the lines the compiler does not see, so a
// structural rule can be NAMED in a comment without the comment failing
// the rule it describes.
func compiledLines(src string) []string {
	var out []string
	block := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case block:
			if strings.Contains(trimmed, "*/") {
				block = false
			}
		case strings.HasPrefix(trimmed, "//"):
		case strings.HasPrefix(trimmed, "/*"):
			block = !strings.Contains(trimmed, "*/")
		default:
			out = append(out, line)
		}
	}
	return out
}

// THE RULING: the reconciler's publish slot refuses on this build, once
// for the pass and before it considers a single branch.
func TestThePublishSlotPublishesNothingOnThisBuild(t *testing.T) {
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	e.Gh = func(context.Context, ...string) (string, error) {
		t.Fatal("a disabled slot must not reach the forge")
		return "", nil
	}
	publishBound(t, repo, sha)

	slot := &PublishSlot{}
	rep, err := e.Reconcile(context.Background(), ReconcileOpts{Publish: slot})
	require.NoError(t, err, "a closed publish road is not a broken pass")

	require.Len(t, slot.Results, 1, "one refusal for the pass, not one per branch")
	var refusal *MachineDisabledError
	require.ErrorAs(t, slot.Results[0].Err, &refusal)
	assert.Empty(t, slot.Results[0].Branch, "the refusal is about the build, not about a branch")
	assert.False(t, slot.Results[0].Published)

	// The pass itself succeeded and still reports every branch: a road
	// nobody may walk is not a reason to stop reporting.
	assert.NotEmpty(t, rep.Branches)
	assert.NoError(t, slot.Outcome(),
		"a closed road is not pending work; a cron entry must not exit non-zero every ten minutes over it")
}

// The slot judges each candidate and refuses it, with the permission
// granted so the judgment is actually reached. Nothing is published:
// every branch here is refused on its own merits, which is the table the
// inversions live in.
//
// The forge is a real answering fake rather than a seam that fails the
// test, because the slot's FIRST question is now a forge read: how far
// this change has already got, asked by fork owner, because the report's
// tracking-config answer cannot tell an absent pull request from one
// nobody looked for. What is asserted instead — and it is the stronger
// property — is that no candidate reaches a forge WRITE.
func TestThePublishSlotRefusesEveryCandidateOnItsOwnTerms(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	// A fork to be asked about, and no verdict on the tip: the first case
	// below is the one where there is no evidence at all.
	gittest.BareFork(t, repo, "herbygillot", "herby")
	e := testState(t, repo, nil)
	e.MachinePublish = true
	forge := newConvergingGh("herbygillot")
	e.Gh = forge.run

	cases := []struct {
		name   string
		note   func(n *record.Record)
		branch render.BranchReport
		code   int
		reason string
	}{
		{
			name:   "no positive evidence",
			code:   exitcode.MachineGate,
			reason: "no-positive-evidence",
		},
		{
			name:   "a verification still going",
			note:   func(n *record.Record) { setRun(n, record.Running) },
			code:   exitcode.PromotionPending,
			reason: "promotion-pending",
		},
		{
			name:   "a failed build",
			note:   func(n *record.Record) { setRun(n, record.Failed) },
			code:   exitcode.VerifyFailed,
			reason: "verification-failed",
		},
		{
			name: "a pass still carrying an unanswered finding",
			note: func(n *record.Record) {
				setRun(n, record.Passed)
				n.Findings = append(n.Findings, record.Finding{
					Kind: "dependent-revbump", Disposition: record.Proposed, At: time.Now()})
			},
			code:   exitcode.MachineGate,
			reason: "open-proposal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := publishBound(t, repo, sha)
			if tc.note != nil {
				tc.note(&n)
				require.NoError(t, e.Ledger(repo).Write(ctx, n))
				n = mustRead(t, repo, sha)
			}
			b := render.BranchReport{Branch: "dockhand/jq-1.8", Tip: sha, Note: &n}
			rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{b}}
			slot := &PublishSlot{}
			e.publishPass(ctx, repo, &rep, slot)

			require.Len(t, slot.Results, 1)
			res := slot.Results[0]
			assert.False(t, res.Published, "nothing is published")
			require.Error(t, res.Err)
			var coder exitcode.Coder
			require.ErrorAs(t, res.Err, &coder)
			assert.Equal(t, tc.code, coder.DockhandExit())
			var namer exitcode.Reasoner
			require.ErrorAs(t, res.Err, &namer)
			assert.Equal(t, tc.reason, namer.Code())
			// The refusal is said under the branch it is about, the way
			// every other phase's prose is.
			require.NotEmpty(t, rep.Branches[0].Prose)
			assert.Equal(t, render.ToErr, rep.Branches[0].Prose[0].Stream)
		})
	}

	assert.Zero(t, forge.called("create"), "no candidate here opens a pull request")
	assert.Zero(t, forge.called("edit"), "and none edits one")
}

// A forge lookup the pass could not answer stops it. The zero PRFact
// means "no pull request", and reading an unanswered lookup as one would
// make an unattended pass open a second pull request beside the first.
func TestAnUnansweredForgeLookupStopsTheSlot(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true
	n := publishBound(t, repo, sha)
	setRun(&n, record.Passed)
	require.NoError(t, e.Ledger(repo).Write(ctx, n))
	n = mustRead(t, repo, sha)

	b := render.BranchReport{Branch: "dockhand/jq-1.8", Tip: sha, Note: &n}
	b.Retire.Promoted = true
	b.Retire.Err = "GitHub said 403"
	rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{b}}
	slot := &PublishSlot{}
	e.publishPass(ctx, repo, &rep, slot)

	require.Len(t, slot.Results, 1)
	var lookup *ForgeLookupError
	require.ErrorAs(t, slot.Results[0].Err, &lookup)
	assert.Equal(t, exitcode.WitnessAPI, lookup.DockhandExit())
	assert.Equal(t, "forge-lookup-failed", lookup.Code())
	assert.Contains(t, lookup.Error(), "will not guess")
}

// THE RULING: absent and unknown never become the same answer.
//
// A branch with an open pull request and NO LOCAL TRACKING CONFIG is the
// case the report cannot see: retire() returns before any forge call
// when git knows no upstream for the branch, leaving the zero PRFact,
// and the zero PRFact reads as PhaseInFlight. A slot keying its
// idempotence on that would open a second pull request beside the first
// — the one cost this tool exists never to impose.
//
// The shape is not hypothetical: a branch --replace just re-minted has
// no tracking config until a push restores it, and neither does a fresh
// clone of the tree. So the slot asks the forge itself, by fork owner.
func TestTheSlotAsksTheForgeRatherThanTheTrackingConfig(t *testing.T) {
	ctx := context.Background()
	repo, sha := promotableRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true
	forge := newConvergingGh("herbygillot")
	// Open on the forge; invisible to git, because nothing was ever
	// pushed from this checkout.
	forge.open["herbygillot:dockhand/jq-1.8"] = 7
	e.Gh = forge.run

	n := publishBound(t, repo, sha)
	b := render.BranchReport{Branch: "dockhand/jq-1.8", Tip: sha, Note: &n}
	require.Equal(t, verdict.PhaseInFlight, verdict.PhaseOf(b.Retire.PR),
		"the report's own answer, which is what a lookup that never ran leaves behind")

	rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{b}}
	slot := &PublishSlot{}
	e.publishPass(ctx, repo, &rep, slot)

	require.Len(t, slot.Results, 1)
	require.NoError(t, slot.Results[0].Err)
	assert.True(t, slot.Results[0].NoOp, "already in front of reviewers is work done, not an error")
	assert.False(t, slot.Results[0].Published)
	assert.Zero(t, forge.called("create"), "and no second pull request is opened")
	assert.Empty(t, rep.Branches[0].Prose, "nothing to say about work that was already done")
}

// A forge that will not answer the phase question stops the branch,
// rather than being read as "no pull request".
func TestAPhaseLookupThatFailsStopsTheSlot(t *testing.T) {
	ctx := context.Background()
	repo, sha := promotableRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true
	e.Gh = func(context.Context, ...string) (string, error) {
		return "", errors.New("GitHub said 403")
	}

	n := publishBound(t, repo, sha)
	rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{
		{Branch: "dockhand/jq-1.8", Tip: sha, Note: &n}}}
	slot := &PublishSlot{}
	e.publishPass(ctx, repo, &rep, slot)

	require.Len(t, slot.Results, 1)
	var lookup *ForgeLookupError
	require.ErrorAs(t, slot.Results[0].Err, &lookup)
	assert.Contains(t, lookup.Error(), "this branch's own pull request")
}

// THE RULING: a hold costs no network, and the note the slot gates on is
// the one it just read.
//
// The pass's snapshot was taken during observation, before retire and
// before any other phase. A hold placed in that window — a person
// running `dockhand hold` while a pass walks the namespace — was not
// consulted until Promote, which is on the far side of the re-witness: a
// full mirrors-off distfile download for every file the change records,
// spent on a change somebody had already stopped.
func TestAHoldPlacedDuringThePassStopsItBeforeTheRewitness(t *testing.T) {
	ctx := context.Background()
	repo, sha := promotableRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true
	e.Gh = newConvergingGh("herbygillot").run
	// The seam the re-witness reaches first. Reaching it at all is the
	// failure this test is about.
	e.Session = func(context.Context, ...eval.Option) (*eval.Evaluator, error) {
		t.Fatal("a held change must not cost a fetch, an evaluation or a push")
		return nil, nil
	}

	// The report's snapshot: not held, because at observation time it was
	// not.
	observed := publishBound(t, repo, sha)
	require.Nil(t, observed.Hold)

	// And the note as it stands NOW, held by somebody between the
	// observation and this phase.
	require.NoError(t, e.Hold(ctx, repo, "dockhand/jq-1.8", "leave this one alone", time.Now()))

	rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{
		{Branch: "dockhand/jq-1.8", Tip: sha, Note: &observed}}}
	slot := &PublishSlot{}
	e.publishPass(ctx, repo, &rep, slot)

	require.Len(t, slot.Results, 1)
	var held *HeldError
	require.ErrorAs(t, slot.Results[0].Err, &held)
	assert.Equal(t, "the publication", held.Withheld)
	assert.Equal(t, exitcode.Held, held.DockhandExit())
}

// A branch the machine was never asked to publish is not a candidate,
// and neither is a held one. Candidacy is the ask on the record, because
// acting past the ask is inventing it.
func TestTheSlotActsOnlyOnWhatSomebodyAskedToBePublished(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	e.MachinePublish = true

	base := mintedNote(t, repo, sha)
	held := base
	held.Destination = record.ToPublished
	held.Hold = &record.Hold{Reason: "a person said stop", At: time.Now()}
	superseded := base
	superseded.Destination = record.ToPublished
	superseded.SupersededBy = "dockhand/jq-1.9"

	toVerdict := base
	toVerdict.Destination = record.ToVerdict
	toBranch := base
	toBranch.Destination = record.ToBranch

	for _, tc := range []struct {
		name string
		note *record.Record
	}{
		{"bound for a verdict, not a pull request", &toVerdict},
		{"bound for the branch alone", &toBranch},
		{"held by a person", &held},
		{"replaced by a newer sibling", &superseded},
		{"no record at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{
				{Branch: "dockhand/jq-1.8", Tip: sha, Note: tc.note}}}
			slot := &PublishSlot{}
			e.publishPass(ctx, repo, &rep, slot)
			assert.Empty(t, slot.Results, "not a candidate, so not considered")
			assert.Empty(t, rep.Branches[0].Prose, "and nothing said about it")
		})
	}

	// A branch whose standing could not be read is not a candidate
	// either: there is no standing to publish on.
	rep := render.Report{Now: time.Now(), Branches: []render.BranchReport{
		{Branch: "dockhand/jq-1.8", Tip: sha, ObserveErr: "the note will not parse"}}}
	slot := &PublishSlot{}
	e.publishPass(ctx, repo, &rep, slot)
	assert.Empty(t, slot.Results)
}

// THE RULING: the per-pass cap and the pacing land with the road, so
// that flipping the constant later is safe.
//
// Both are exercised as pure limits. The cap's default is one, which is
// why the pacing never fires today — and the pacing is here anyway,
// because a cap raised later beside a pacing that was never written is a
// burst.
func TestTheCapAndThePacingHoldThePassBack(t *testing.T) {
	now := time.Now()

	t.Run("the cap is one by default", func(t *testing.T) {
		s := &PublishSlot{}
		assert.Equal(t, DefaultPassPRCap, s.cap())
		assert.Equal(t, DefaultPublishPace, s.pace())
		require.NoError(t, s.admit("dockhand/jq-1.8", now))
		s.took(now)

		err := s.admit("dockhand/jq-1.9", now)
		var limit *PassLimitError
		require.ErrorAs(t, err, &limit)
		assert.Equal(t, exitcode.PromotionPending, limit.DockhandExit())
		assert.Equal(t, "pending", exitcode.Family(limit.DockhandExit()),
			"a pass pacing itself is not a refusal")
		assert.Equal(t, "pass-limit", limit.Code())
	})

	t.Run("the pacing holds a raised cap back", func(t *testing.T) {
		s := &PublishSlot{MaxPRs: 5, Pace: time.Hour}
		require.NoError(t, s.admit("dockhand/jq-1.8", now))
		s.took(now)

		var limit *PassLimitError
		require.ErrorAs(t, s.admit("dockhand/jq-1.9", now.Add(time.Minute)), &limit)
		assert.Contains(t, limit.Error(), "pacing interval")
		require.NoError(t, s.admit("dockhand/jq-1.9", now.Add(2*time.Hour)),
			"once the interval has elapsed the pass may publish again")
	})

	t.Run("a negative cap publishes nothing", func(t *testing.T) {
		s := &PublishSlot{MaxPRs: -1}
		require.Error(t, s.admit("dockhand/jq-1.8", now))
	})

	// The clock the pass asks these limits against is read PER
	// PUBLICATION and not once for the report. rep.Now exists so that two
	// branches cannot disagree about how long a run has been going;
	// handing it to the pacing as well would make every publication in
	// one pass happen at the same instant, so the elapsed gap after the
	// first would be zero forever and a raised cap would admit nothing.
	t.Run("the pacing is asked against a moving clock", func(t *testing.T) {
		one := bodyOfSource(t, "publishslot.go", "func (e *Engine) publishOne(")
		assert.Equal(t, 1, countMatching(one, regexp.MustCompile(`s\.admit\(b\.Branch, time\.Now\(\)\)`)),
			"the cap and the pacing read the clock where the publication happens")
		assert.Equal(t, 1, countMatching(one, regexp.MustCompile(`s\.took\(time\.Now\(\)\)`)))
		assert.Equal(t, 0, countMatching(one, regexp.MustCompile(`s\.(admit|took)\([^)]*\bnow\b`)),
			"and never against the pass's single clock read")
	})

	// And what the pacing is NOT, said in the constant's own doc so that
	// nobody raises the cap believing a cron interval is being policed.
	// The slot is built fresh by each caller and keeps nothing across
	// passes, so this is a within-pass stop.
	t.Run("the pacing does not claim to be a rate", func(t *testing.T) {
		src := string(readSource(t, "publishslot.go"))
		assert.Contains(t, src, "WHAT IT IS NOT IS A RATE",
			"a limit that reads as a rate limit and is not one is worse than none")
		fresh := &PublishSlot{MaxPRs: 5, Pace: time.Hour}
		require.NoError(t, fresh.admit("dockhand/jq-1.8", now),
			"a new pass starts unpaced, which is exactly what a per-pass stop means")
	})
}

// bodyOfSource returns the lines inside the block opening on the first
// line of a file in this package that contains header, over the lines
// the compiler sees.
func bodyOfSource(t *testing.T, name, header string) []string {
	t.Helper()
	lines := compiledLines(string(readSource(t, name)))
	start := -1
	for i, line := range lines {
		if strings.Contains(line, header) {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "%q is where the rule is written, and it is not there", header)
	// The brace may be a line or two below the header: gofmt wraps a long
	// signature, and a rule pinned to a function must not depend on how
	// wide its parameter list happens to be.
	depth, opened := 0, false
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth > 0 {
			opened = true
			continue
		}
		if opened {
			return lines[start+1 : i]
		}
	}
	require.True(t, opened, "%q must open a block", header)
	t.Fatalf("%q opens a block that never closes", header)
	return nil
}

// Outcome reports the waiting and never the refusing. A cron entry that
// exited non-zero because a road it was never asked to walk is closed
// would read as a broken machine in every log that watched it.
func TestTheOutcomeReportsWaitingAndNotRefusing(t *testing.T) {
	assert.NoError(t, (&PublishSlot{}).Outcome(), "a pass with nothing to do is fine")

	refused := &PublishSlot{Results: []PublishResult{
		{Err: &MachineDisabledError{}},
		{Branch: "dockhand/jq-1.8", Err: &verdict.UnprovenError{Branch: "dockhand/jq-1.8"}},
	}}
	assert.NoError(t, refused.Outcome(), "refusals are stated on their branch, not exited on")

	waiting := &PublishSlot{Results: []PublishResult{
		{Err: &MachineDisabledError{}},
		{Branch: "dockhand/jq-1.8", Err: &PassLimitError{Branch: "dockhand/jq-1.8", Why: "capped"}},
	}}
	err := waiting.Outcome()
	require.Error(t, err)
	var coder exitcode.Coder
	require.ErrorAs(t, err, &coder)
	assert.Equal(t, exitcode.PromotionPending, coder.DockhandExit())
}

// The re-witness comparison, over values: what upstream serves against
// what the change records. The fetch itself is not exercised — it is a
// network — and the judgment is, because the judgment is what decides
// whether a change is held.
func TestTheRewitnessComparesRecordedAgainstServed(t *testing.T) {
	recorded := []checksums.Recorded{
		{File: "jq-1.8.tar.gz", Type: "sha256", Value: "aaa"},
		{File: "jq-1.8.tar.gz", Type: "size", Value: "100"},
		// A legacy type nothing here can recompute. Reporting it as a
		// mismatch against an empty string would hold every ancient port
		// on the first unattended pass.
		{File: "jq-1.8.tar.gz", Type: "md5", Value: "ancient"},
	}
	t.Run("agreement is silence", func(t *testing.T) {
		assert.Empty(t, CompareRecorded(recorded, map[string]checksums.Sums{
			"jq-1.8.tar.gz": {Sha256: "aaa", Size: 100}}))
	})
	t.Run("a re-rolled artifact is named in both directions", func(t *testing.T) {
		got := CompareRecorded(recorded, map[string]checksums.Sums{
			"jq-1.8.tar.gz": {Sha256: "bbb", Size: 100}})
		require.Len(t, got, 1)
		assert.Equal(t, "jq-1.8.tar.gz sha256: recorded aaa, upstream now serves bbb", got[0].String())
	})
	t.Run("a file the fetch never produced is a gap and not a contradiction", func(t *testing.T) {
		assert.Empty(t, CompareRecorded(recorded, nil))
	})
	t.Run("the single-distfile form takes the one file fetched", func(t *testing.T) {
		got := CompareRecorded([]checksums.Recorded{{Type: "sha256", Value: "aaa"}},
			map[string]checksums.Sums{"jq-1.8.tar.gz": {Sha256: "bbb"}})
		require.Len(t, got, 1)
		assert.Equal(t, "jq-1.8.tar.gz", got[0].File)
	})

	// The finding a mismatch appends is PROPOSED: it says what was
	// measured and asks, which is what every other finding in the tree
	// does. A finding that executed would be the tool deciding an
	// upstream acted in bad faith.
	f := StealthFinding([]string{"jq"}, CompareRecorded(recorded,
		map[string]checksums.Sums{"jq-1.8.tar.gz": {Sha256: "bbb", Size: 100}}), time.Now())
	assert.Equal(t, StealthSuspected, f.Kind)
	assert.Equal(t, record.Proposed, f.Disposition)
	assert.Contains(t, f.Criterion, "mirrors disabled")
	assert.Contains(t, f.Criterion, "recorded aaa")
}

// A re-witness whose fetch could not run stops the publication. It is
// not evidence of a stealth update and it is not evidence of anything
// else; it is a gate that could not be asked.
func TestARewitnessThatCouldNotRunStopsThePublication(t *testing.T) {
	repo, sha := engineRepo(t)
	e := testState(t, repo, nil)
	n := mintedNote(t, repo, sha)
	n.Subjects = nil

	_, err := e.rewitness(context.Background(), repo, sha, n)
	require.Error(t, err, "a record naming no portdir cannot be re-witnessed")

	b := render.BranchReport{Branch: "dockhand/jq-1.8", Tip: sha, Note: &n}
	err = e.rewitnessBeforePush(context.Background(), repo, &b, n, time.Now())
	var lookup *ForgeLookupError
	require.ErrorAs(t, err, &lookup, "a check that did not run does not publish")
}

// recordedFiles fetches only what the change records AND the surface
// offers. A distfile a vendored block supplies is served by nothing this
// evaluation can see, and refusing over one would hold every port that
// carries a block.
func TestTheRewitnessFetchesOnlyWhatItCanCompare(t *testing.T) {
	offered := map[string][]string{
		"jq-1.8.tar.gz":  {"https://example.invalid/jq-1.8.tar.gz"},
		"vendor.tar.zst": {"https://example.invalid/vendor.tar.zst"},
	}
	got := recordedFiles([]checksums.Recorded{
		{File: "jq-1.8.tar.gz", Type: "sha256", Value: "aaa"},
		{File: "cargo-crates.tar.gz", Type: "sha256", Value: "bbb"},
	}, offered)
	assert.Equal(t, []string{"jq-1.8.tar.gz"}, got,
		"a recorded file the surface does not offer is skipped, not refused over")

	assert.Empty(t, recordedFiles(nil, offered), "nothing recorded, nothing to compare")
	assert.Equal(t, []string{"jq-1.8.tar.gz"},
		recordedFiles([]checksums.Recorded{{Type: "sha256", Value: "aaa"}},
			map[string][]string{"jq-1.8.tar.gz": {"https://example.invalid/jq-1.8.tar.gz"}}),
		"the single-distfile form names the one file offered")
}

// publishBound writes the note a change bound for a pull request has:
// the ask the slot reads, and nothing else.
func publishBound(t *testing.T, repo *git.Repo, sha string) record.Record {
	t.Helper()
	n := mintedNote(t, repo, sha)
	n.Destination = record.ToPublished
	require.NoError(t, ledger.Open(repo).Write(context.Background(), n))
	return mustRead(t, repo, sha)
}

// mustRead is the note as the ledger has it.
func mustRead(t *testing.T, repo *git.Repo, sha string) record.Record {
	t.Helper()
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	return n
}

// setRun puts one run on the fixture's platform.
func setRun(n *record.Record, state record.RunState) {
	if n.Runs == nil {
		n.Runs = map[string]record.Run{}
	}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: state, Platform: "Testos"}
}

// THE RULING: --to-pr's immediate form asks its ring-3 questions BEFORE
// it mints anything.
//
// Both refusals below would otherwise arrive after the branch existed,
// leaving a person a branch they asked for only as a step toward a
// publication that was never going to happen — and the merged case would
// leave it pointing at work the project has already taken.
//
// The questions are asked over the branch and title the mint WOULD
// produce, derived the way the mint derives them: a precheck asking
// about a different branch than the one about to exist would be a check
// with nothing behind it.
func TestThePrepublishChecksRefuseBeforeAnythingIsMinted(t *testing.T) {
	ctx := context.Background()

	t.Run("an own pull request that already merged is a dead end", func(t *testing.T) {
		repo, _ := promotableRepo(t)
		forge := &scriptedGh{login: "herbygillot",
			own: `[{"number":80,"state":"closed","merged_at":"2026-09-01T00:00:00Z","title":"jq: update to 1.8","html_url":"https://x/80"}]`}
		e := testState(t, repo, nil)
		e.Gh = forge.run

		err := e.PrecheckPublish(ctx, repo, bumpPlan(t, repo, "bump", "1.8"))
		var merged *verdict.PRMergedError
		require.ErrorAs(t, err, &merged)
		assert.Equal(t, exitcode.PRMerged, merged.DockhandExit())
		assert.Equal(t, 80, merged.Number)

		_, rerr := repo.RevParse(ctx, "dockhand/jq-1.8-unminted")
		require.Error(t, rerr, "nothing was minted to reach this refusal")
	})

	t.Run("an open pull request proposing the same title is a duplicate", func(t *testing.T) {
		repo, _ := promotableRepo(t)
		forge := &scriptedGh{login: "herbygillot", own: "[]",
			open: `[{"number":91,"state":"open","title":"jq: update to 1.8","html_url":"https://x/91"}]`}
		e := testState(t, repo, nil)
		e.Gh = forge.run

		err := e.PrecheckPublish(ctx, repo, bumpPlan(t, repo, "bump", "1.8"))
		var dup *verdict.DuplicatePRError
		require.ErrorAs(t, err, &dup)
		assert.Equal(t, exitcode.DuplicatePR, dup.DockhandExit())
	})

	t.Run("a clean forge lets the mint proceed", func(t *testing.T) {
		repo, _ := promotableRepo(t)
		e := testState(t, repo, nil)
		e.Gh = (&scriptedGh{login: "herbygillot", own: "[]", open: "[]"}).run
		require.NoError(t, e.PrecheckPublish(ctx, repo, bumpPlan(t, repo, "bump", "1.8")))
	})

	// A forge that will not answer leaves the box unticked and warns. This
	// form is reachable only by a person — the boundary refuses every other
	// invoker before it — so promote's own reasoning for warning rather
	// than refusing holds here unchanged.
	t.Run("a forge that will not answer warns and lets it through", func(t *testing.T) {
		repo, _ := promotableRepo(t)
		var out, errb bytes.Buffer
		e := testEngine(t, repo, nil, &out, &errb)
		e.Gh = func(_ context.Context, args ...string) (string, error) {
			if args[0] == "api" && args[1] == "user" {
				return "herbygillot\n", nil
			}
			return "", errors.New("GitHub said 403")
		}
		require.NoError(t, e.PrecheckPublish(ctx, repo, bumpPlan(t, repo, "bump", "1.8")))
		assert.Contains(t, errb.String(), "warning: could not check for this branch's own PR")
		assert.Contains(t, errb.String(), "warning: could not search for open PRs")
	})
}

// scriptedGh answers the two lookups a precheck makes and nothing else,
// so a call the precheck should not have made fails the test by being
// unscripted rather than by returning something plausible.
type scriptedGh struct {
	login string
	// own answers the head-ref query; open answers the same-port walk.
	own, open string
}

func (g *scriptedGh) run(_ context.Context, args ...string) (string, error) {
	switch {
	case args[0] == "api" && len(args) >= 2 && args[1] == "user":
		return g.login + "\n", nil
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "/pulls?head="):
		return g.own, nil
	case args[0] == "api" && len(args) >= 2 && strings.Contains(args[1], "/pulls?state=open"):
		return g.open, nil
	}
	return "", fmt.Errorf("scriptedGh: unscripted call %v", args)
}
