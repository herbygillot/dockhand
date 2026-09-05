package engine

// The two-map split, driven from the engine side at N greater than
// one. Nothing mints a cohort yet, so the records here are built by
// hand — which is the point: the split exists so that the day one is
// minted, the guest is still handed back once and the blame still lands
// on the member that earned it.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// cohortNote is one guest on one release with two subjects running
// inside it: one job, two runs, which is the shape the split is for.
func cohortNote(t *testing.T, repo *git.Repo, sha string, ports ...string) record.Record {
	t.Helper()
	n, err := ledger.Open(repo).LoadOrStart(context.Background(), sha)
	require.NoError(t, err)
	for _, p := range ports {
		n.Subjects = append(n.Subjects, record.Subject{Port: p, Names: []string{p}, Portdir: "sysutils/" + p})
		n.Runs[record.RunKey(p, "Testos")] = record.Run{
			State: record.Running, Platform: "Testos", Linted: true}
	}
	n.Jobs["Testos"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}}
	require.NoError(t, ledger.Open(repo).Write(context.Background(), n))
	return n
}

// One environment shared by two subjects goes back exactly once. Two
// verdicts, one guest: a release per run would return the same worker
// twice, which nothing can undo.
func TestOneGuestIsReleasedOnceForTwoSubjects(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	assert.Equal(t, []string{"fake-1"}, fake.Released, "one guest, one release")
	assert.Equal(t, record.Passed, runFor(n, "jq", "Testos").State)
	assert.Equal(t, record.Passed, runFor(n, "oniguruma", "Testos").State)
	assert.True(t, n.Jobs["Testos"].Released)
}

// A guest with a subject still building in it is not this pass's to
// hand back, however finished the subject it did judge is.
func TestAGuestStaysWhileASubjectIsStillBuilding(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	// The guest is still working, so neither run settles — and the
	// release right is refused on its own terms too, which is what makes
	// this safe against a peer that judged only its own member.
	fake := &verifytest.Fake{States: map[string]verify.Status{"fake-1": {State: verify.Running}}}
	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	assert.Empty(t, fake.Released, "nothing settled, so nothing is handed back")
	took, err := ledger.Open(repo).ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.False(t, took, "the ledger refuses the release while a run is live")
}

// A log nothing framed — no marker, no record — that names a sibling
// as what broke: the sibling takes the failure and the headline is
// blocked on it rather than disproven. Naming the sibling is the
// difference between "untested" and "untested because of oniguruma".
func TestABlockedSubjectBlamesItsSibling(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building oniguruma\n" +
			"Error: Failed to build oniguruma: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	jq := runFor(n, "jq", "Testos")
	assert.Equal(t, record.Blocked, jq.State, "jq was never reached")
	assert.Equal(t, "oniguruma", jq.Blamed, "and the note says which member stopped it")

	// The log blames oniguruma for itself, which is its own failure and
	// not an inheritance, so it keeps the environment and the guest
	// stays for somebody to enter.
	assert.Equal(t, record.Failed, runFor(n, "oniguruma", "Testos").State)
	assert.Empty(t, runFor(n, "oniguruma", "Testos").Blamed)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle)
	assert.Empty(t, fake.Released, "a failure keeps its environment however many siblings passed")
}

// Accepting the proposal: `bump-revision --for <branch>` end to end.
//
// The members are planned from the BRANCH TIP's own Portfiles and land
// as ONE commit, because they move for one reason and it is the same
// reason. The php/php-Judy family is the collapse the whole reverse
// index exists for — seven indexed names, one directory, one revision
// line — and the member whose Portfile shape does not say where a
// revision line belongs is declined by name while the rest proceed.
func TestTheCohortVerbBumpsWhatItCanAndNamesWhatItCannot(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	// The working tree is deliberately made to disagree with the branch,
	// and left uncommitted. It is the user's — a half-finished edit,
	// another branch checked out — and a cohort planned from it would
	// compute its edits against bytes the commit will never carry and
	// pledge a precondition hash for the wrong file. The revision below
	// must come back 5 (the tip's 4, incremented) and never 100.
	require.NoError(t, os.WriteFile(
		filepath.Join(repo.Root, "sysutils", "netdata", "Portfile"),
		[]byte(memberPortfile("netdata", "revision            99\n")), 0o644))
	n := noteOn(t, repo, sha, "judy")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline: installed("judy", "1.0.5_0",
			dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")),
		Installed: installed("judy", "1.1.0_0",
			dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
	})
	e := indexed(t, repo, fake)
	require.NoError(t, e.settle(ctx, repo, &n))
	require.True(t, AnyProposed(n), "the settlement left a question")

	var out, errOut bytes.Buffer
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true}))

	tip, err := repo.RevParse(ctx, "dockhand/judy-1.1")
	require.NoError(t, err)
	assert.Equal(t, tip, strings.TrimSpace(out.String()), "the new tip is the verb's answer on stdout")
	assert.NotEqual(t, sha, tip, "the branch grew a commit")

	// One commit, and its message restates the criterion verbatim.
	msg := messageOf(t, repo, tip)
	assert.Contains(t, msg, "judy: revbump 1 dependent of judy 1.1")
	assert.Contains(t, msg, "install name /opt/local/lib/libJudy.1.dylib → /opt/local/lib/libJudy.2.dylib",
		"the criterion is the measurement's own sentence, restated and not reworded")
	assert.Contains(t, msg, "necessary and not sufficient",
		"the caveat travels with the claim wherever it is quoted")
	assert.Contains(t, msg, "netdata (sysutils/netdata)")
	assert.Contains(t, msg, "Not bumped here — do these by hand:")
	assert.Contains(t, msg, "php80-Judy")

	// The bytes: netdata's revision moved and nothing else did.
	after, err := repo.BlobAt(ctx, tip, "sysutils/netdata/Portfile")
	require.NoError(t, err)
	assert.Contains(t, string(after), "revision            5",
		"the tip's 4 incremented — never the working tree's 99, which this test left uncommitted on purpose")
	assert.NotContains(t, string(after), "revision            100")
	before, err := repo.BlobAt(ctx, tip, "php/php-Judy/Portfile")
	require.NoError(t, err)
	assert.NotContains(t, string(before), "revision", "a declined member is left exactly as it was")

	// The record: the proposal answered, the declined members marked as
	// what actually happened to them, and the evidence inherited.
	fresh, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.False(t, AnyProposed(fresh), "the question is answered, so an unattended publication may proceed")
	cohort, ok := findingOf(fresh, "dependent-revbump")
	require.True(t, ok)
	assert.Equal(t, record.Accepted, cohort.Disposition)
	require.NotNil(t, fresh.Evidence)
	assert.Equal(t, sha, fresh.Evidence.From, "the new tip stands on the first commit's verification")
	assert.Equal(t, "judy", fresh.Headline().Port, "the headline the branch is named for stays where it is")
	assert.Contains(t, fresh.Ports(), "netdata")
	assert.NotContains(t, fresh.Ports(), "php80-Judy", "a member that was not planned is not a subject")
	for _, c := range cohort.Candidates {
		if c.Port == "php80-Judy" {
			assert.Contains(t, c.Reason, "proposed, then declined")
			assert.Contains(t, c.Reason, "do this one by hand")
		}
	}
	assert.Contains(t, errOut.String(), "php80-Judy: not bumped —")
}

// The two refusals the verb owes by name: a branch with nothing
// proposed, and one whose only proposal is a maintainer's comment with
// no measurement behind it. The second is the two-step remedy — a
// cohort built on a comment alone would be the blanket revbump this
// tool must never make.
func TestTheCohortVerbDeclinesByName(t *testing.T) {
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	e := indexed(t, repo, measured(verify.Manifests{}))

	err := e.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no revbump proposal on this branch")
	assert.Equal(t, exitcode.PlanDeclined, exitcode.TwinOf(err).Code)

	n.Findings = []record.Finding{{
		Kind: "instruction-comment", Ports: []string{"judy"},
		Source: "sysutils/judy/Portfile", Quote: "# Please revbump netdata whenever judy updates",
		Disposition: record.Proposed,
	}}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	err = e.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ABI criterion yet")
	assert.Contains(t, err.Error(), "dockhand verify dockhand/judy-1.1")
	assert.Contains(t, err.Error(), "dockhand dismiss dockhand/judy-1.1")

	// And the third: the comment is still proposed and the measurement
	// HAS been made and found nothing. The refusal must read the note it
	// is refusing over. It used to say nothing had measured whether
	// anything moved — three lines under the measurement — and send the
	// reader back to `verify`, which re-measures to the same answer
	// forever while the machine gate keeps holding. Only `dismiss`
	// cleared it, and nothing said so.
	n.Findings = append([]record.Finding{{
		Kind: "abi-unchanged", Ports: []string{"judy"},
		Criterion:   "no install name, compatibility version or library moved, measured between judy@1.0.5_0 (binary archive) and @1.0.6_0 (source not recorded) on Testos",
		Disposition: record.Accepted,
	}}, n.Findings...)
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	err = e.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the measurement found nothing to bump on")
	assert.Contains(t, err.Error(), "no install name, compatibility version or library moved",
		"the evidence a person weighs the comment against, quoted rather than described")
	assert.Contains(t, err.Error(), "revbump by hand what you judge the comment covers")
	assert.Contains(t, err.Error(), "dockhand dismiss dockhand/judy-1.1")
	assert.NotContains(t, err.Error(), "nothing has measured")
	assert.NotContains(t, err.Error(), "dockhand verify",
		"running it again produces the same sentence; offering it would be a loop")
	assert.Equal(t, exitcode.PlanDeclined, exitcode.TwinOf(err).Code)
}

// Dismissal is an answer and not an absence: the measurement stays on
// the note and only the answer to it changes, so the next pass does not
// ask again.
func TestDismissRecordsTheAnswerAndKeepsTheMeasurement(t *testing.T) {
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	n.Findings = []record.Finding{
		{Kind: "abi-change", Ports: []string{"judy"}, Criterion: "install name moved", Disposition: record.Accepted},
		{Kind: "dependent-revbump", Ports: []string{"netdata"}, Criterion: "install name moved", Disposition: record.Proposed},
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	var out, errOut bytes.Buffer
	e := indexed(t, repo, measured(verify.Manifests{}))
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.Dismiss(ctx, repo, "dockhand/judy-1.1"))
	assert.Contains(t, out.String(), "dismissed on dockhand/judy-1.1: dependent-revbump")

	fresh, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.False(t, AnyProposed(fresh))
	abi, ok := findingOf(fresh, "abi-change")
	require.True(t, ok, "the measurement stands; only the answer to it moved")
	assert.Equal(t, "install name moved", abi.Criterion)

	// A second dismissal has nothing to answer and says so.
	err = eng.Dismiss(ctx, repo, "dockhand/judy-1.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing is proposed on this branch")
}

// The machine gate: an unattended publication will not answer a
// question, and a person publishing past their own advisory is told
// what they are publishing past.
func TestTheMachineGateHoldsAnOpenProposal(t *testing.T) {
	n := record.Record{Findings: []record.Finding{
		{Kind: "dependent-revbump", Disposition: record.Proposed},
	}}
	err := GateMachinePublish(n, "dockhand/judy-1.1", record.Machine)
	require.Error(t, err)
	assert.Equal(t, exitcode.MachineGate, exitcode.TwinOf(err).Code)
	assert.Contains(t, err.Error(), "dockhand bump-revision --for dockhand/judy-1.1")
	assert.Contains(t, err.Error(), "dockhand dismiss dockhand/judy-1.1")

	assert.NoError(t, GateMachinePublish(n, "dockhand/judy-1.1", record.Human),
		"a person is looking at the proposal; publishing anyway is their answer")

	answered := record.Record{Findings: []record.Finding{
		{Kind: "dependent-revbump", Disposition: record.Dismissed},
	}}
	assert.NoError(t, GateMachinePublish(answered, "dockhand/judy-1.1", record.Machine))
}

// The finding kinds are one vocabulary spelled in two packages: the
// plan writes them and the renderings read them, and neither may import
// the other. A constant that drifted would be a finding nobody renders
// and a section nobody fills, both of them silent — so the two are held
// equal here, in the one package that imports both.
func TestTheFindingVocabularyIsOneVocabulary(t *testing.T) {
	assert.Equal(t, render.KindInstruction, intent.FindingInstruction)
	assert.Equal(t, render.KindABIChanged, string(verdict.ABIChanged))
	assert.Equal(t, render.KindABIUnchanged, string(verdict.ABIUnchanged))
	assert.Equal(t, render.KindABIUnavailable, string(verdict.ABIUnavailable))

	// And the cohort's own kind, which verdict writes and this package
	// answers by name.
	f, ok := verdict.Cohort{Port: "libwidget",
		Members: []record.Candidate{{Port: "gdal", Proposed: true}}}.Finding()
	require.True(t, ok)
	assert.Equal(t, render.KindCohort, f.Kind)
}

// The acceptance test: the chain, and not the links.
//
// The ask this whole overhaul was for is one sentence — bundle revision
// bumps to downstream ports when a bump changes the target port's ABI —
// and what answers it is a chain: the environment describes what it
// installed, the measurement says what moved, the note records it, the
// machine gate holds an unattended publication, a person answers, one
// commit lands, and the pull request states the claim. Every link is
// proven on its own in this package and its neighbours. A chain of
// sound links still breaks at the joints, and the joints are where the
// silent failures live — a gate reading a disposition the settlement
// never writes, a verb declining a proposal that was made, a body
// stating a cohort the note does not carry. Only a walk finds those.
//
// The reverse index under all of it is the captured PortIndex slice, so
// the members are the ports that really declare judy, at the portdirs
// the index really gives them.

// judyMoved is the environment where the library moved: one logical
// library under a new install name, which is the break a dependent
// recorded the old side of.
func judyMoved() verify.Manifests {
	return verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline: installed("judy", "1.0.5_0",
			dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")),
		Installed: installed("judy", "1.1.0_0",
			dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
		// Attributed to the member that recorded it. The guest reads one
		// capture per subject, so the port that installed the file is
		// known exactly there and nowhere after it — a file path does not
		// say which port laid it down.
		Links: map[string]map[string][]string{
			"netdata": {"/opt/local/lib/libJudy.2.dylib": {"/opt/local/bin/netdata"}},
		},
	}
}

// judyStill is the same environment with nothing moved: the same
// library, the same install name, the same compatibility version, over
// a version bump that really did only change the source.
func judyStill() verify.Manifests {
	same := dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")
	return verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline:       installed("judy", "1.0.5_0", same),
		Installed:      installed("judy", "1.0.6_0", same),
	}
}

// Every answer the guest can give, walked from the measurement to what
// a person is finally told.
//
// Five environments, and only the first proposes anything. The other
// four refuse, which is the step's whole character: nothing moved, no
// baseline, no Manifester and no dependents each decline BY NAME, and
// the three that could not measure are never allowed to arrive at the
// person as the one that did and found nothing. Even the proposal is
// advisory — the gate holds it, and a human answers.
func TestEveryAnswerTheGuestCanGiveReachesAPerson(t *testing.T) {
	for _, tc := range []struct {
		name      string
		port      string
		manifests verify.Manifests
		// mute wraps the provider in Incapable: one that declared it
		// could describe an installation and implements no Manifester,
		// which is the reconfiguration the two gates are apart for.
		mute bool

		// kind is the ABI finding the settlement records, empty where the
		// question was never asked at all.
		kind string
		// proposed is whether the settlement left a question, which is
		// exactly what the machine gate holds on.
		proposed bool
		// verb is what `bump-revision --for` says, empty where it builds.
		verb string
		// body and notBody are what the pull request ends up stating —
		// the last link, and the only one a reviewer ever reads.
		body    []string
		notBody []string
	}{{
		name: "the library moved", port: "judy", manifests: judyMoved(),
		kind: "abi-change", proposed: true,
		body: []string{
			"ABI changed: install name /opt/local/lib/libJudy.1.dylib → /opt/local/lib/libJudy.2.dylib",
			"was proposed and is not in this change",
		},
	}, {
		name: "nothing moved", port: "judy", manifests: judyStill(),
		kind: "abi-unchanged",
		verb: "no revbump proposal on this branch",
		body: []string{"ABI unchanged: no install name, compatibility version or library moved"},
		// The refutation is a result and has to read as one. A body that
		// also carried a proposal would be claiming both.
		notBody: []string{"revision bump", "Revision bumped"},
	}, {
		name: "no baseline to compare against", port: "judy",
		manifests: verify.Manifests{
			BaselineSource: verify.BaselineNone,
			BaselineReason: "the port did not exist at the merge base",
			Installed: installed("judy", "1.1.0_0",
				dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
		},
		kind: "abi-unavailable",
		verb: "no revbump proposal on this branch",
		body: []string{
			"ABI check unavailable: no baseline for judy: the port did not exist at the merge base",
			"nothing banks one yet",
		},
		// The one substitution that must never happen: an absent before
		// side compares as every library removed, so silence here would
		// be the strongest false break available. And no command that
		// would not help: the remedy this replaced named `dockhand verify
		// <portdir>`, which banks nothing, so a reader who ran it met the
		// identical refusal with no way to tell they had been sent in a
		// circle.
		notBody: []string{"ABI unchanged", "ABI changed", "dockhand verify"},
	}, {
		name: "an environment that cannot describe one", port: "judy",
		manifests: judyMoved(), mute: true,
		kind: "abi-unavailable",
		verb: "no revbump proposal on this branch",
		body: []string{"ABI check unavailable: this environment cannot describe an installation"},
		// Not one word about libJudy: nothing was measured, so nothing
		// may be said about what it publishes.
		notBody: []string{"ABI unchanged", "libJudy"},
	}, {
		name: "a port nothing depends on", port: "jq", manifests: judyMoved(),
		verb: "no revbump proposal on this branch",
		// Not even an abi-unchanged. The measurement's one consumer is
		// the cohort decision, so a leaf port's body is byte-identical to
		// what it always was.
		notBody: []string{"ABI check", "ABI changed", "ABI unchanged", "revision bump"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.verb == "" {
				// Only the row that actually plans a member needs a real
				// evaluator; the four refusals decline before any Tcl runs.
				testenv.PortTclsh(t)
			}
			ctx := context.Background()
			repo, sha := indexedRepo(t, tc.port)
			branch := "dockhand/" + tc.port + "-1.1"
			n := noteOn(t, repo, sha, tc.port)
			fake := measured(tc.manifests)
			fake.Logs = map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"}
			var prov verify.Verifier = fake
			if tc.mute {
				prov = verifytest.Incapable{Fake: fake}
			}
			e := indexed(t, repo, prov)

			// 1. Settle: the guest is asked what it installed, while it
			// still holds it, and the measurement goes on the note.
			require.NoError(t, e.settle(ctx, repo, &n))
			if tc.kind == "" {
				assert.Empty(t, n.Findings, "nothing depends on it, so nothing was ever measured")
			} else {
				_, ok := findingOf(n, tc.kind)
				assert.True(t, ok, "the settlement records %s", tc.kind)
			}

			// 2. The machine gate: an unattended publication answers no
			// questions, and a person's own promote is never refused.
			assert.Equal(t, tc.proposed, AnyProposed(n))
			gate := GateMachinePublish(n, branch, record.Machine)
			if tc.proposed {
				require.Error(t, gate)
				assert.Equal(t, exitcode.MachineGate, exitcode.TwinOf(gate).Code)
			} else {
				require.NoError(t, gate, "no question, nothing to hold")
			}
			require.NoError(t, GateMachinePublish(n, branch, record.Human),
				"the gate refuses the machine and never the person")

			// 3. The body: what a reviewer is actually told.
			body := render.PRBody(n, n.Promotable(), render.PRBodyOpts{Version: "test"})
			for _, want := range tc.body {
				assert.Contains(t, body, want)
			}
			for _, never := range tc.notBody {
				assert.NotContains(t, body, never)
			}

			// 4. The verb: a person answering the proposal, or being told
			// by name why there is nothing to answer.
			var out, errOut bytes.Buffer
			eng := *e
			eng.Out, eng.Err = &out, &errOut
			err := eng.BuildCohort(ctx, repo, branch, CohortOpts{NoVerify: true})
			if tc.verb != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.verb)
				assert.Equal(t, exitcode.PlanDeclined, exitcode.TwinOf(err).Code,
					"a refusal with nothing broken and nothing written")
				return
			}
			require.NoError(t, err)

			// 5. And the claim the accepted cohort publishes, on the tip
			// that carries it: the same criterion, now stated as done.
			tip := strings.TrimSpace(out.String())
			fresh, rerr := ledger.Open(repo).Read(ctx, tip)
			require.NoError(t, rerr)
			assert.NoError(t, GateMachinePublish(fresh, branch, record.Machine),
				"the question is answered, so the unattended road may proceed")
			after := render.PRBody(fresh, fresh.Promotable(), render.PRBodyOpts{Version: "test"})
			assert.Contains(t, after, "Revision bumped in this change:")
			assert.Contains(t, after, "netdata (sysutils/netdata)")
			assert.NotContains(t, after, "was proposed and is not in this change",
				"the proposal was accepted, and a body may not say both")
			assert.NotContains(t, after, "no verification environment on the submitting machine",
				"the tip inherits the headline's verification and says so; the machine has an environment")
		})
	}
}

// An up-front cohort the measurement refutes.
//
// A person who bundled the revision bumps by hand has already committed
// them, so there is no proposal to make and nothing for the gate to
// hold. What there is, is a claim in the change that the measurement
// does not support — and the one place a reviewer will meet it is the
// pull request body. That is what makes "nothing moved" a result rather
// than an absence: it is printed whether or not a cohort was carried.
func TestAnUpFrontCohortIsRefutedInTheBody(t *testing.T) {
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{
		{Port: "judy", Names: []string{"judy"}, Portdir: "sysutils/judy", Intent: "bump", Target: "1.1"},
		{Port: "netdata", Names: []string{"netdata"}, Portdir: "sysutils/netdata",
			Intent: "bump-revision", Target: "rev5"},
	}
	startedFor(&n, "judy", "Testos", "fake-1", record.Run{State: record.Running, Linted: true})
	startedFor(&n, "netdata", "Testos", "fake-1", record.Run{State: record.Running, Linted: true})
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := measured(judyStill())
	fake.Logs = map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"}
	require.NoError(t, indexed(t, repo, fake).settle(ctx, repo, &n))

	assert.False(t, AnyProposed(n), "the bumps are already committed; there is no question left to ask")
	require.NoError(t, GateMachinePublish(n, "dockhand/judy-1.1", record.Machine),
		"and so nothing for the machine gate to hold")

	body := render.PRBody(n, n.Promotable(), render.PRBodyOpts{Version: "test"})
	assert.Contains(t, body, "ABI unchanged: no install name, compatibility version or library moved",
		"the reviewer is told the change carries a revision bump the measurement does not support")
	assert.Contains(t, body, "netdata",
		"and the member stays in the change, because a commit is not undone by a measurement")
}

// Accepting the proposal verifies the whole cohort at once.
//
// One guest, the headline first and the members after it in dependency
// order, every portdir staged ahead of the guest's own tree. That
// ordering is the evidence and not a tidiness point: each member's rev+1
// names an archive that does not exist, so it rebuilds from source
// against the library the headline just installed, and a member built
// before the headline would be built against the old one.
func TestAcceptingTheProposalVerifiesTheWholeCohortAtOnce(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(judyMoved())
	fake.Logs = map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"}
	e := indexed(t, repo, fake)
	require.NoError(t, e.settle(ctx, repo, &n))
	require.True(t, AnyProposed(n))

	var out, errOut bytes.Buffer
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{}))

	require.Len(t, fake.Submitted, 1, "a cohort that must be built together is one build")
	req := fake.Submitted[0]
	assert.Equal(t, []string{"judy", "netdata"}, req.Ports,
		"the headline first, then the member that links what it publishes")
	require.Len(t, req.Portdirs, 2, "every member staged, or the guest builds its own tree's copy")
	assert.True(t, strings.HasSuffix(req.Portdirs[0], filepath.FromSlash("sysutils/judy")), req.Portdirs[0])
	assert.True(t, strings.HasSuffix(req.Portdirs[1], filepath.FromSlash("sysutils/netdata")), req.Portdirs[1])
	assert.True(t, req.Manifest,
		"judy has dependents and the provider can describe an installation, which is the whole of the condition")
	assert.Equal(t, [][]string{nil, {"judy"}}, req.Requires,
		"the guest is told which member waits for which: netdata declares judy, and judy waits for nobody")

	tip := strings.TrimSpace(out.String())
	fresh, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.Running, runFor(fresh, "netdata", "Testos").State,
		"the member is being built, and the note says so before anything comes back")
}

// A cohort that breaks names the member that broke it.
//
// The exit status is the whole answer a machine gets, and "the branch
// failed" over a nine-member cohort is not one anybody can act on. The
// headline comes back blocked with the member blamed — a true sentence
// about the headline and the wrong answer to "what happened" — so the
// status is read off the member that actually failed.
func TestACohortFailureNamesTheMemberThatEarnedIt(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building oniguruma\n" +
			"Error: Failed to build oniguruma: command execution failed\n"},
	}
	cohortNote(t, repo, sha, "jq", "oniguruma")

	var out, errOut bytes.Buffer
	err := testEngine(t, repo, fake, &out, &errOut).Follow(ctx, repo, sha, "jq", "Testos",
		fake, verify.Job{Provider: "fake", ID: "fake-1"})

	var failed *VerifyFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, "oniguruma", failed.Port, "the member that failed, not the port the follow was watching")
	assert.Equal(t, "fake-1", failed.Handle, "and the environment it kept, for somebody to enter")
	assert.Equal(t, exitcode.VerifyFailed, exitcode.TwinOf(err).Code)
}

// The tail of the chain: the cohort's own verification, settled.
//
// Every other walk in this file stops at "netdata Running". What that
// leaves unproven is the last claim the pull request makes — that this
// member links what moved — and it is the claim whose evidence has the
// furthest to travel: the guest reads one capture per subject, the
// settlement draws each member's proof against the names the
// measurement says moved, the run records it, and the body prints it
// per port. A rendering golden built from a hand-written record cannot
// see any of that.
func TestTheCohortsOwnVerificationProvesEachMemberSLink(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(judyMoved())
	fake.Logs = map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"}
	e := indexed(t, repo, fake)
	require.NoError(t, e.settle(ctx, repo, &n))

	var out, errOut bytes.Buffer
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{}))

	tip := strings.TrimSpace(out.String())
	fresh, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	require.Equal(t, record.Running, runFor(fresh, "netdata", "Testos").State)

	// The cohort's guest comes back. Both subjects settle out of the one
	// environment, and the evidence is collected before it is released.
	require.NoError(t, eng.settle(ctx, repo, &fresh))

	assert.Equal(t, record.Passed, runFor(fresh, "judy", "Testos").State)
	assert.Equal(t, record.Passed, runFor(fresh, "netdata", "Testos").State)

	// The proof, on the member's own run and nowhere else. A file path
	// does not say which port installed it, so this attribution exists
	// in the guest and is lost the moment the bindings are merged.
	assert.Equal(t, []string{"/opt/local/bin/netdata links against /opt/local/lib/libJudy.2.dylib"},
		runFor(fresh, "netdata", "Testos").Links)
	assert.Nil(t, runFor(fresh, "judy", "Testos").Links,
		"the headline is not asked whether it links itself")

	// And the last link: what a reviewer actually reads.
	body := render.PRBody(fresh, fresh.Promotable(), render.PRBodyOpts{Version: "test"})
	assert.Contains(t, body, "— netdata (sysutils/netdata): depends_lib; /opt/local/bin/netdata links against /opt/local/lib/libJudy.2.dylib")
	assert.Contains(t, body, "necessary and not sufficient",
		"the caveat travels with the criterion into the body, not only into the commit")
	assert.NotContains(t, body, "no verification environment on the submitting machine",
		"the cohort's tip has runs of its own now, and never had none while carrying a measurement")

	// The re-measurement did not ask the question again. The proposal on
	// this record was answered by the commit that produced it.
	assert.False(t, AnyProposed(fresh))
	require.NoError(t, GateMachinePublish(fresh, "dockhand/judy-1.1", record.Machine))
}

// The extended tip before its own verification comes back, which is
// what a `--no-verify` cohort publishes and what any extend publishes
// while the follow-on submit has not started.
//
// It carries no runs and it carries an ABI measurement, and the body
// used to state both: "nothing was run" directly above "ABI changed".
// Two sentences that cannot both be true, with no sha offered for the
// reader to go and check which one was.
func TestAnExtendedTipSaysWhereItsEvidenceCameFrom(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	e := indexed(t, repo, measured(judyMoved()))
	require.NoError(t, e.settle(ctx, repo, &n))

	var out, errOut bytes.Buffer
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true}))

	fresh, err := ledger.Open(repo).Read(ctx, strings.TrimSpace(out.String()))
	require.NoError(t, err)
	require.Empty(t, fresh.Runs, "an extend inherits its evidence and earns none of its own")

	body := render.PRBody(fresh, fresh.Promotable(), render.PRBodyOpts{Version: "test"})
	assert.NotContains(t, body, "no verification environment on the submitting machine",
		"the machine has one; it verified the tip this commit stands on")
	assert.Contains(t, body, "this commit adds to a change that was verified at `"+sha[:12]+"`")
	assert.Contains(t, body, "ABI changed: install name",
		"and the measurement it inherited is stated in the same body")
}

// The proposal names every dependent now that the cap is off, and
// --exclude is how a person takes some of them. An excluded member is
// out of the change entirely: not bumped, not built, and listed among
// the ports examined and not bumped so a reviewer can disagree.
func TestExcludeTakesAMemberOutOfTheChange(t *testing.T) {
	f := record.Finding{Candidates: []record.Candidate{
		{Port: "ImageMagick", Portdir: "graphics/ImageMagick", Proposed: true, Reason: "depends_lib"},
		{Port: "gegl", Portdir: "graphics/gegl", Proposed: true, Reason: "depends_lib"},
		{Port: "gthumb", Portdir: "gnome/gthumb", Proposed: true, Reason: "depends_lib"},
	}}

	got, err := excludeMembers(f, []string{"gthumb"})
	require.NoError(t, err)

	var proposed, left []string
	for _, c := range got.Candidates {
		if c.Proposed {
			proposed = append(proposed, c.Port)
		} else {
			left = append(left, c.Port+": "+c.Reason)
		}
	}
	assert.Equal(t, []string{"ImageMagick", "gegl"}, proposed)
	assert.Equal(t, []string{"gthumb: " + excludedReason}, left,
		"named, because a port dropped in silence is one nobody can disagree about")
}

// Case matters to a reader and not to an intention. What must not
// happen is the verb bumping a port the user asked it to leave alone.
func TestExcludeMatchesRegardlessOfCase(t *testing.T) {
	f := record.Finding{Candidates: []record.Candidate{
		{Port: "ImageMagick", Proposed: true},
		{Port: "gegl", Proposed: true},
	}}
	got, err := excludeMembers(f, []string{"imagemagick"})
	require.NoError(t, err)
	for _, c := range got.Candidates {
		if c.Port == "ImageMagick" {
			assert.False(t, c.Proposed, "the user named this port; spelling its case differently is not consent")
		}
	}
}

// A name that matches nothing is an error and not a shrug: the verb
// would otherwise take an instruction, do the opposite of it, and
// report success.
func TestExcludeRefusesANameTheProposalDoesNotHold(t *testing.T) {
	f := record.Finding{Candidates: []record.Candidate{{Port: "gegl", Proposed: true}}}
	_, err := excludeMembers(f, []string{"gthumb"})
	require.Error(t, err)

	var unknown *UnknownMemberError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, []string{"gthumb"}, unknown.Names)
	assert.Contains(t, err.Error(), "gegl", "the refusal says what could have been named instead")
	assert.Equal(t, exitcode.PlanDeclined, unknown.DockhandExit())
}

// Excluding everything is not how a proposal is turned down. Saying so
// points at the verb that records the decision, rather than committing
// an empty cohort that reads as work done.
func TestExcludingEveryMemberIsRefused(t *testing.T) {
	f := record.Finding{Candidates: []record.Candidate{
		{Port: "gegl", Proposed: true}, {Port: "gthumb", Proposed: true}}}
	_, err := excludeMembers(f, []string{"gegl", "gthumb"})
	require.Error(t, err)

	var empty *EmptyCohortError
	require.ErrorAs(t, err, &empty)
	assert.Contains(t, err.Error(), "dismiss")
}

// A member's whole files ride in the cohort commit beside its
// Portfile, at the member's own portdir, the way planOnBase carries
// them for a single plan. Exercised on the list builder alone because
// no cohort planner produces a file today; the carry is what keeps the
// commit honest when one does.
func TestCohortFilesCarryEachMembersWholeFiles(t *testing.T) {
	built := []planned{
		{Portdir: "graphics/gegl", Plan: &plan.Plan{}, Content: []byte("revision 1\n")},
		{Portdir: "graphics/gthumb", Content: []byte("revision 2\n"), Plan: &plan.Plan{Files: []plan.FileEdit{
			{Path: "files/patch-foo.diff", Content: "@@ -9 +9 @@\n", Reason: "1 hunk moved"},
		}}},
	}
	assert.Equal(t, []git.File{
		{Path: "graphics/gegl/Portfile", Content: []byte("revision 1\n")},
		{Path: "graphics/gthumb/Portfile", Content: []byte("revision 2\n")},
		{Path: "graphics/gthumb/files/patch-foo.diff", Content: []byte("@@ -9 +9 @@\n")},
	}, cohortFiles(built))
}

// The request's graph is the edges among the ports being built, and
// nothing wider. It is read off the reverse index for every member and
// not the headline alone, matched as the index keys names, and spelled
// back the way the request spells them, so the provider finds each
// prerequisite in Ports by equality.
func TestTheCohortsGraphIsTheEdgesAmongItsOwnMembers(t *testing.T) {
	repo, _ := indexedRepo(t, "judy")
	e := indexed(t, repo, &verifytest.Fake{})

	assert.Equal(t, [][]string{nil, {"judy"}, nil}, e.cohortRequires([]string{"judy", "netdata", "other"}),
		"netdata declares judy; judy and other declare nothing in the request")
	assert.Equal(t, [][]string{nil, {"Judy"}}, e.cohortRequires([]string{"Judy", "Netdata"}),
		"matched as the index keys names, written as the request spells them")
	assert.Equal(t, [][]string{{"judy"}, nil}, e.cohortRequires([]string{"netdata", "judy"}),
		"an edge is an edge whichever member is the headline")
	assert.Nil(t, e.cohortRequires([]string{"netdata"}), "one port has no cohort to be ordered within")

	// No tree, no graph: every member is attempted, and one built
	// ahead of a failed prerequisite fails on its own with the
	// prerequisite's name in its log — the slow answer, never a wrong
	// one.
	bare, sha := engineRepo(t)
	assert.Nil(t, testState(t, bare, &verifytest.Fake{}).cohortRequires([]string{"jq", "oniguruma"}))
	_ = sha
}

// A provider that cannot read the guest's record leaves the log to
// speak alone, and the log alone reads a member it never announced as
// a runner that did not finish — errored, blamed on nobody — rather
// than guessing that it was skipped for the member ahead of it.
func TestAProviderWithoutARecordSettlesFromTheLogAlone(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": verify.SubjectMarker("oniguruma") + "\n" +
			"Error: Failed to build oniguruma: command execution failed\n"},
		Outcomes: map[string][]verify.MemberState{"fake-1": {
			{Port: "oniguruma", Outcome: verify.MemberFailed},
			{Port: "jq", Outcome: verify.MemberSkipped, Prerequisite: "oniguruma"},
		}},
	}
	n := cohortNote(t, repo, sha, "oniguruma", "jq")

	require.NoError(t, countingEngine(repo, verifytest.Incapable{Fake: fake}).settle(ctx, repo, &n))

	assert.Equal(t, record.Failed, runFor(n, "oniguruma", "Testos").State)
	jq := runFor(n, "jq", "Testos")
	assert.Equal(t, record.Errored, jq.State, "the record was there to read and this provider cannot read it")
	assert.Empty(t, jq.Blamed)

	// The same guest through a provider that can: the record says jq
	// was skipped for oniguruma, and the settle writes that.
	repo, sha = engineRepo(t)
	again := cohortNote(t, repo, sha, "oniguruma", "jq")
	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &again))
	assert.Equal(t, record.Blocked, runFor(again, "jq", "Testos").State)
	assert.Equal(t, "oniguruma", runFor(again, "jq", "Testos").Blamed)
}
