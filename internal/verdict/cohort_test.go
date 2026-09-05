package verdict

// The cohort judge, over values. Two sweeps run here: the cohort
// corpus, which is the new behaviour, and the SINGLE-subject corpus,
// which is the old one and must come back out of JudgeCohort
// byte-identical to what JudgeRun says about it.
//
// The second sweep is the important one. A cohort judge that produced
// slightly different words for one subject would move every settle
// golden, every status line and the liveproof, and it would do it
// silently — the shapes are the same states with the same releases, and
// only the sentences would drift. Holding the two functions equal over
// every log in the corpus is the only assertion that catches that.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/corpustest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// cohortDir is the one copy of the cohort corpus, beside the
// single-subject one it does not replace.
const cohortDir = "../engine/testdata/cohorts"

// soloCohort is one subject stated as a cohort of one — the shape
// every record in the field has today.
func soloCohort(port string, run record.Run) CohortInput {
	return CohortInput{
		Subjects: []record.Subject{{Port: port, Names: []string{port}}},
		Runs:     map[string]record.Run{port: run},
	}
}

// A single subject settles through JudgeCohort exactly as it settles
// through JudgeRun, over every log in the corpus and every status a
// poll can answer with. Not "equivalently": the same Judgment value,
// which is the state, the whole run and the release advice.
func TestJudgeCohortIsJudgeRunAtOneSubject(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join(corpusDir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			log := string(raw)
			exp := corpustest.Read(t, strings.TrimSuffix(path, ".log")+".expect")

			st := verify.Status{State: verify.Failed, Handle: "fake-1"}
			if exp.Outcome == "passed" {
				st = verify.Status{State: verify.Passed, Handle: "fake-1"}
			}
			for _, tc := range []struct {
				name string
				run  record.Run
				in   CohortInput
			}{
				{name: "as polled", run: running(true)},
				{name: "a run that never linted", run: running(false)},
				{name: "a log that could not be read", run: running(true)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					readable := tc.name != "a log that could not be read"
					in := soloCohort(exp.Port, tc.run)
					in.Status, in.LogRead = st, readable
					if readable {
						in.Log = log
					}
					want := JudgeRun(RunInput{Run: tc.run, Port: exp.Port, Status: st,
						Log: in.Log, LogRead: readable})
					got := JudgeCohort(in)
					require.Len(t, got, 1, "one subject, one verdict")
					assert.Equal(t, want, got[exp.Port])
				})
			}

			// And the guarded reader the caller runs before the tree
			// lookup: one subject blames what it has always blamed, and
			// nothing more.
			in := soloCohort(exp.Port, running(true))
			in.Status, in.Log, in.LogRead = st, log, true
			if wantDep, wantBlamed := BlamedDependency(log, exp.Port); wantBlamed {
				assert.Equal(t, []string{wantDep}, CohortBlame(in), "blamed port")
			} else {
				assert.Empty(t, CohortBlame(in))
			}
		})
	}
}

// The statuses no log reaches, held equal the same way. A vanished
// job, a guest still building and an environment that never came up
// are facts about the guest, and one subject must hear about them in
// exactly the words it always has.
func TestJudgeCohortMatchesJudgeRunOnEveryStatus(t *testing.T) {
	cases := []struct {
		name     string
		vanished bool
		st       verify.Status
	}{
		{name: "vanished", vanished: true},
		{name: "still building", st: verify.Status{State: verify.Running}},
		{name: "errored", st: verify.Status{State: verify.Errored, Detail: "guest agent never came up"}},
		{name: "passed", st: verify.Status{State: verify.Passed, Handle: "fake-1"}},
	}
	log := "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := soloCohort("jq", running(true))
			in.Vanished, in.Status, in.Log, in.LogRead = tc.vanished, tc.st, log, true
			want := JudgeRun(RunInput{Run: running(true), Port: "jq", Vanished: tc.vanished,
				Status: tc.st, Log: log, LogRead: true})
			assert.Equal(t, want, JudgeCohort(in)["jq"])
		})
	}
}

// A record with no subjects is a verdict about nobody, and the judge
// says nothing about nobody rather than reaching for a headline that
// is not there.
func TestJudgeCohortOfNobodySaysNothing(t *testing.T) {
	assert.Empty(t, JudgeCohort(CohortInput{
		Status: verify.Status{State: verify.Failed}, Log: "Error: Failed to build jq: x\n", LogRead: true}))
}

func TestCohortCorpus(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join(cohortDir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs, "%s must hold at least the synthesized shapes", cohortDir)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			exp := corpustest.ReadCohort(t, strings.TrimSuffix(path, ".log")+".expect")

			in := cohortOf(exp, string(raw))
			js := JudgeCohort(in)
			require.Len(t, js, len(exp.Members), "every member gets a verdict")
			for _, m := range exp.Members {
				want := exp.Verdict[m]
				j, ok := js[m]
				require.True(t, ok, "%s has no verdict", m)
				require.True(t, j.Settled, "%s: a finished guest settles every member", m)
				// The sidecar is plain text, so the wire word is what it
				// names: the state converts to it rather than the corpus
				// learning a Go type.
				assert.Equal(t, want.State, string(j.Run.State), "%s: state", m)
				assert.Equal(t, want.Detail, j.Run.Detail, "%s: detail", m)
				assert.Equal(t, want.Blamed, j.Run.Blamed, "%s: blamed", m)
				assert.Equal(t, want.Lint, j.Run.Lint, "%s: lint evidence", m)
				if want.State == "failed" {
					assert.Equal(t, KeepWorker, j.Release,
						"%s: this change's own breakage keeps the environment", m)
				} else {
					assert.NotEqual(t, KeepWorker, j.Release,
						"%s: nothing of this member's to debug", m)
				}
			}

			// A cohort blames ports outside the change or it blames
			// nobody, and it never blames one of its own members: the
			// nomaintainer note tells a person there is nobody to nudge
			// about somebody else's port, and a sibling is not one.
			for _, dep := range CohortBlame(in) {
				assert.NotContains(t, exp.Members, dep, "a member is never the stranger")
			}
		})
	}
}

// cohortOf is the corpus's roster as the settle hands it over: every
// member running inside one guest, with the one poll, the one log, and
// the runner's own record of each member.
func cohortOf(exp corpustest.CohortExpect, log string) CohortInput {
	in := CohortInput{
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
		Runs:   map[string]record.Run{},
		Log:    log, LogRead: true,
		Reported: exp.MemberStates(),
	}
	if exp.Outcome == "passed" {
		in.Status = verify.Status{State: verify.Passed, Handle: "fake-1"}
	}
	for _, m := range exp.Members {
		in.Subjects = append(in.Subjects, record.Subject{Port: m, Names: []string{m}})
		in.Runs[m] = running(true)
	}
	return in
}

// reported is one entry of the runner's record, spelled the way the
// sidecar spells it: a word, and for a skip the member it names.
func reported(port string, outcome verify.MemberOutcome, prerequisite ...string) verify.MemberState {
	ms := verify.MemberState{Port: port, Outcome: outcome}
	if len(prerequisite) > 0 {
		ms.Prerequisite = prerequisite[0]
	}
	return ms
}

// A subport's failure belongs to the member that ships it. This is
// the whole reason Subject.Names exists, and getting it wrong is not a
// wording bug: a member matched on its port alone would find no owner
// for py312-foo, read its own breakage as a stranger's, block instead
// of fail, and hand back the environment the failure was keeping.
func TestACohortBlamesTheMemberThatOwnsTheSubport(t *testing.T) {
	log := verify.SubjectMarker("foo") + "\n" +
		"--->  Building py312-foo\n" +
		"Error: Failed to build py312-foo: command execution failed\n" +
		"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"
	in := CohortInput{
		Subjects: []record.Subject{
			{Port: "foo", Names: []string{"foo", "py312-foo", "py313-foo"}},
			{Port: "bar", Names: []string{"bar"}},
		},
		Runs:   map[string]record.Run{"foo": running(true), "bar": running(true)},
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:    log, LogRead: true,
		Reported: []verify.MemberState{reported("foo", verify.MemberFailed), reported("bar", verify.MemberSkipped, "foo")},
	}

	foo := JudgeCohort(in)["foo"]
	assert.Equal(t, record.Failed, foo.Run.State, "the member that ships the subport owns its failure")
	assert.Equal(t, "Failed to build py312-foo: command execution failed", foo.Run.Detail)
	assert.Equal(t, KeepWorker, foo.Release, "and the environment it broke in is kept")
	assert.Empty(t, foo.Run.Blamed, "a failure inherits nothing")

	bar := JudgeCohort(in)["bar"]
	assert.Equal(t, record.Blocked, bar.Run.State)
	assert.Equal(t, "foo", bar.Run.Blamed, "blamed names the member, not the subport that broke")

	// And the tree lookup is never sent after a name of our own.
	assert.Empty(t, CohortBlame(in), "a subport of a member is not a stranger")
}

// A member that declines the platform failed at nothing, and the
// member skipped for it is told so in those words. A reader told
// "fails to build" would go looking for a breakage that is not there.
func TestADecliningMemberBlocksItsDependentWithoutFailing(t *testing.T) {
	log := verify.SubjectMarker("oniguruma") + "\n" +
		"--->  Skipping oniguruma: known_fail on Monterey\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"}}, {Port: "jq", Names: []string{"jq"}}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{reported("oniguruma", verify.MemberFailed), reported("jq", verify.MemberSkipped, "oniguruma")},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Unsupported, js["oniguruma"].Run.State)
	assert.Equal(t, "the port declines to build on this platform", js["oniguruma"].Run.Detail)
	assert.Equal(t, ReleaseQuietly, js["oniguruma"].Release, "a correct refusal leaves nothing to debug")

	assert.Equal(t, record.Blocked, js["jq"].Run.State)
	assert.Equal(t, "oniguruma", js["jq"].Run.Blamed)
	assert.Equal(t, "oniguruma declines this platform; this member is untested", js["jq"].Run.Detail)
}

// A failed guest whose log cannot be read at all. Nothing can be
// attributed, so the headline takes the failure and keeps the
// environment — the direction this package is conservative in
// everywhere, since a failure is one log read away from the truth and
// the environment is where that read would be made.
func TestAnUnreadableCohortLogFallsToTheHeadline(t *testing.T) {
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["oniguruma"].Run.State, "the headline is the one a refusal names")
	assert.Empty(t, js["oniguruma"].Run.Detail, "no log, no diagnosis")
	assert.Equal(t, KeepWorker, js["oniguruma"].Release)
	assert.Equal(t, record.Blocked, js["jq"].Run.State)
	assert.Equal(t, "oniguruma", js["jq"].Run.Blamed)
}

// A provider that can name the member its verdict is about is
// believed, when the log's own markers say nothing. None does today,
// which is why this is stated here rather than left to the corpus.
func TestAProviderMayNameTheFailingMember(t *testing.T) {
	log := "--->  Building something\nError: Processing of port jq failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1", Subject: "jq"},
		Log:      log, LogRead: true,
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["jq"].Run.State, "the provider named jq and nothing contradicted it")
	assert.Equal(t, record.Blocked, js["oniguruma"].Run.State)
	assert.Equal(t, "jq", js["oniguruma"].Run.Blamed)
}

// Promotable sums over every SUBJECT and not merely over the run map.
// A cohort can reach a shape one subject never could — a pass, a block
// and no failure at all — and the run-map arithmetic reads that as
// promotable, which would publish a member nothing ever built.
func TestPromotableSumsOverEveryMember(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs: map[string]record.Run{
			record.RunKey("oniguruma", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("jq", "Sequoia"):        {State: record.Failed, Platform: "Sequoia"},
		}}
	assert.True(t, n.Promotable(),
		"a dependent that failed is best effort: published over, and named to the author and the reviewer")

	n.Runs[record.RunKey("jq", "Sequoia")] = record.Run{State: record.Blocked, Platform: "Sequoia"}
	assert.True(t, n.Promotable(), "and a dependent nothing built reached an outcome too")

	// What the gate still sums over every member is whether each has an
	// answer at all. A member mid-build is the case the run arithmetic
	// alone would misread, and it is why this asks after the subjects
	// rather than the map.
	n.Runs[record.RunKey("jq", "Sequoia")] = record.Run{State: record.Running, Platform: "Sequoia"}
	assert.False(t, n.Promotable(), "jq is still building; its guest has not finished disagreeing")

	delete(n.Runs, record.RunKey("jq", "Sequoia"))
	assert.False(t, n.Promotable(),
		"and with no run at all, a promotion that summed only the runs would publish jq on oniguruma's pass")

	n.Runs[record.RunKey("jq", "Sequoia")] = record.Run{State: record.Passed, Platform: "Sequoia"}
	assert.True(t, n.Promotable(), "both members proven on the platform they were asked about")

	// The headline is not best effort. It is the change itself, and a
	// dependent's pass does not answer for it.
	n.Runs[record.RunKey("oniguruma", "Sequoia")] = record.Run{State: record.Failed, Platform: "Sequoia"}
	assert.False(t, n.Promotable(), "the headline failed")
}

// A member the runner skipped is not blocked on a member built after
// it. The sentence blame writes points backwards — "X fails to build;
// this member is untested" — and a member ahead of the stopper that
// nothing announced cannot have been stopped by a successor. What is
// true of it is that the guest said nothing about it.
func TestAnUnannouncedMemberAheadOfTheStopIsNotBlamedOnItsSuccessor(t *testing.T) {
	log := verify.SubjectMarker("jq") + "\n" +
		"--->  Building jq\n" +
		"Error: Failed to build jq: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["jq"].Run.State)
	assert.Equal(t, record.Errored, js["oniguruma"].Run.State,
		"the runner builds in this order and announced nothing for oniguruma")
	assert.Equal(t, "the guest reported no output for this subject", js["oniguruma"].Run.Detail)
	assert.Empty(t, js["oniguruma"].Run.Blamed, "jq is built after oniguruma and cannot have stopped it")
	assert.Equal(t, ReleaseQuietly, js["oniguruma"].Release)
}

// A marker naming nobody in the change announces nothing about the
// change. Cutting on it would hand every member an empty section, and
// an empty section has no diagnosis in it and no stranger to find —
// which is how a dependency's breakage becomes this change's own.
func TestAMarkerNamingNoMemberLeavesTheLogWhole(t *testing.T) {
	log := "===> dockhand subject: oniguruma extra\n" +
		"--->  Building oniguruma\n" +
		"Error: Failed to build pkgconfig: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Blocked, js["oniguruma"].Run.State, "a stranger broke, not the change")
	assert.Equal(t, "dependency pkgconfig fails to build; the change itself is untested",
		js["oniguruma"].Run.Detail)
	assert.Equal(t, ReleaseQuietly, js["oniguruma"].Release)

	assert.Equal(t, []string{"pkgconfig"}, CohortBlame(in), "the tree lookup must still be sent after pkgconfig")
}

// One subject concludes what the request's shape says it concludes, and
// never what a line in the log says. A build that prints the marker
// prefix itself would otherwise cut a log that has no sections in it,
// and a change that passed would settle errored on the strength of one
// line of somebody's build output.
func TestOneSubjectIsUnmovedByAStrayMarker(t *testing.T) {
	log := "--->  Verifying Portfile for jq\n" +
		"--->  0 errors and 0 warnings found.\n" +
		verify.SubjectMarker("whatever") + "\n" +
		"--->  Installing jq @1.8.0_1\n"
	in := soloCohort("jq", running(true))
	in.Status, in.Log, in.LogRead = verify.Status{State: verify.Passed, Handle: "fake-1"}, log, true

	j := JudgeCohort(in)["jq"]
	assert.Equal(t, record.Passed, j.Run.State, "the guest passed and one member is what passed")
	assert.Equal(t, "clean", j.Run.Lint, "and its lint line is read out of the whole log")
}

// A blame an earlier settlement wrote does not outlive the reading that
// put it there. Blamed names a member of the change; a stranger is not
// one, and a note carrying both sentences at once contradicts itself.
// The member the stranger broke under is blamed on nobody, and the
// member behind it is blamed on THAT member — so a stale name is
// cleared on the one and overwritten on the other, and neither reading
// lets "leftover" stand.
func TestAStrangerBlockClearsAStaleBlame(t *testing.T) {
	stale := running(true)
	stale.Blamed = "leftover"
	log := "--->  Building jq\nError: Failed to build pkgconfig: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "jq"}, {Port: "oniguruma"}},
		Runs:     map[string]record.Run{"jq": stale, "oniguruma": stale},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Blocked, js["jq"].Run.State)
	assert.Empty(t, js["jq"].Run.Blamed, "jq's detail names a stranger, so nothing of ours is blamed")

	assert.Equal(t, record.Blocked, js["oniguruma"].Run.State)
	assert.Equal(t, "jq", js["oniguruma"].Run.Blamed,
		"oniguruma is behind jq, and the sibling it stopped behind is what it is blamed on")
}

// A member skipped for a prerequisite that was itself BLOCKED — a
// stranger broke under it — is blocked by the sibling, not by the
// stranger. Nothing in the log says what a skipped member depends on,
// so a sentence naming the stranger asserts a dependency edge the tree
// may not carry. Measured live, under the runner that stopped:
// py311-rawpy, which depends on py311-scikit-image, was told that
// py310-scikit-image fails to build. Under the runner that goes on,
// py311-rawpy is not behind anything: it does not depend on
// py310-rawpy, so it is built and judged on its own.
func TestAMemberSkippedForAStrangerBlockedPrerequisiteIsBlockedByTheSibling(t *testing.T) {
	log := verify.SubjectMarker("libraw") + "\n" +
		"--->  Verifying Portfile for libraw\n" +
		"--->  0 errors and 0 warnings found.\n" +
		"--->  Building libraw\n" +
		"--->  Installing libraw @0.21.4_0\n" +
		verify.SubjectMarker("py310-rawpy") + "\n" +
		"--->  Computing dependencies for py310-rawpy\n" +
		"--->  Dependencies to be installed: py310-scikit-image\n" +
		"--->  Building py310-scikit-image\n" +
		"Error: Failed to build py310-scikit-image: command execution failed\n" +
		"Error: Processing of port py310-rawpy failed\n" +
		verify.SubjectMarker("py311-rawpy") + "\n" +
		"--->  Verifying Portfile for py311-rawpy\n" +
		"--->  0 errors and 1 warnings found.\n" +
		"--->  Building py311-rawpy\n" +
		"--->  Installing py311-rawpy @0.19.0_1\n"
	in := CohortInput{
		Subjects: []record.Subject{
			{Port: "libraw", Names: []string{"libraw"}},
			{Port: "py310-rawpy", Names: []string{"py310-rawpy"}},
			{Port: "py310-rawpy-extra", Names: []string{"py310-rawpy-extra"}},
			{Port: "py311-rawpy", Names: []string{"py311-rawpy"}},
		},
		Runs: map[string]record.Run{
			"libraw": running(true), "py310-rawpy": running(true),
			"py310-rawpy-extra": running(true), "py311-rawpy": running(true),
		},
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:    log, LogRead: true,
		Reported: []verify.MemberState{
			reported("libraw", verify.MemberPassed),
			reported("py310-rawpy", verify.MemberFailed),
			reported("py310-rawpy-extra", verify.MemberSkipped, "py310-rawpy"),
			reported("py311-rawpy", verify.MemberPassed),
		},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Passed, js["libraw"].Run.State, "announced, finished, and its section is clean")

	// The member the stranger broke under: blocked on the stranger
	// exactly as one subject would be, blaming no sibling.
	under := js["py310-rawpy"]
	assert.Equal(t, record.Blocked, under.Run.State)
	assert.Equal(t, "dependency py310-scikit-image fails to build; the change itself is untested",
		under.Run.Detail)
	assert.Empty(t, under.Run.Blamed, "a stranger is not a member, so nothing of ours is blamed")

	// The member skipped for it: blocked by the SIBLING, with the
	// stranger nowhere in the sentence.
	skipped := js["py310-rawpy-extra"]
	assert.Equal(t, record.Blocked, skipped.Run.State)
	assert.Equal(t, "py310-rawpy", skipped.Run.Blamed, "the prerequisite the record names")
	assert.Equal(t, "py310-rawpy could not be built, so this member was not built; it is untested",
		skipped.Run.Detail)
	assert.NotContains(t, skipped.Run.Detail, "py310-scikit-image",
		"the record says what py310-rawpy-extra depends on, and it is not the stranger")
	assert.Equal(t, ReleaseQuietly, skipped.Release, "nothing of this member's to debug")

	// The member that depends on neither: built after the failure,
	// passed on its own section, its own lint line read from it.
	assert.Equal(t, record.Passed, js["py311-rawpy"].Run.State, "nothing it depends on broke")
	assert.Equal(t, "1 warning", js["py311-rawpy"].Run.Lint)

	// And the tree lookup is still sent after the stranger, once, for
	// the sentence of the member it broke under.
	assert.Equal(t, []string{"py310-scikit-image"}, CohortBlame(in))
}

// A withheld member is one the guest was never asked about, so the
// log's silence about it is expected. Settling must leave it alone:
// every rule that reads silence would otherwise blame it on the runner
// or on a sibling, and neither is what happened.
func TestSettlingLeavesAWithheldMemberAlone(t *testing.T) {
	held := record.Run{State: record.Withheld, Platform: "Testos",
		Detail: "it conflicts with gegl, which this cohort builds"}
	in := CohortInput{
		Subjects: []record.Subject{{Port: "gegl"}, {Port: "gegl-devel"}, {Port: "gthumb"}},
		Runs: map[string]record.Run{
			"gegl":       {State: record.Running, Platform: "Testos"},
			"gegl-devel": held,
			"gthumb":     {State: record.Running, Platform: "Testos"},
		},
		Status:  verify.Status{State: verify.Passed, Handle: "fake-1"},
		Log:     "===> dockhand subject: gegl\n===> dockhand subject: gthumb\n",
		LogRead: true,
	}
	out := JudgeCohort(in)

	_, judged := out["gegl-devel"]
	assert.False(t, judged, "a member never submitted gets no verdict from a log that never mentions it")
	assert.Equal(t, record.Passed, out["gthumb"].Run.State,
		"and the members that did build are unaffected by its absence")
}

// The runner goes on past a failure. A member behind it that does not
// depend on it is built, announced, and judged on its own section
// exactly as if nothing around it had gone wrong — a pass, with its
// own lint line — while the failure keeps the environment for the
// member that earned it.
func TestAnIndependentMemberBehindAFailureIsJudgedOnItsOwn(t *testing.T) {
	log := verify.SubjectMarker("jq") + "\n" +
		"--->  Building jq\n" +
		"Error: Failed to build jq: command execution failed\n" +
		verify.SubjectMarker("mise") + "\n" +
		"--->  Verifying Portfile for mise\n" +
		"--->  0 errors and 0 warnings found.\n" +
		"--->  Installing mise @2025.9.1_1\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "jq"}, {Port: "mise"}},
		Runs:     map[string]record.Run{"jq": running(true), "mise": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{reported("jq", verify.MemberFailed), reported("mise", verify.MemberPassed)},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["jq"].Run.State)
	assert.Equal(t, KeepWorker, js["jq"].Release)

	assert.Equal(t, record.Passed, js["mise"].Run.State, "built after the failure, and nothing it needs broke")
	assert.Equal(t, "clean", js["mise"].Run.Lint, "read from its own section")
	assert.Empty(t, js["mise"].Run.Blamed)
	assert.Equal(t, ReleaseAndReport, js["mise"].Release)
	assert.Empty(t, CohortBlame(in), "nothing outside the change broke")
}

// A skip propagates down a chain, and each member is blamed on the
// prerequisite its own record names: the one skipped for the member
// that failed is told it fails to build, and the one skipped for THAT
// member is told it could not be built — which is what is true of a
// member that failed at nothing and was skipped in its turn.
func TestASkipIsBlamedDownTheChain(t *testing.T) {
	log := verify.SubjectMarker("liba") + "\n" +
		"Error: Failed to build liba: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "liba"}, {Port: "libb"}, {Port: "libc"}},
		Runs:     map[string]record.Run{"liba": running(true), "libb": running(true), "libc": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{
			reported("liba", verify.MemberFailed),
			reported("libb", verify.MemberSkipped, "liba"),
			reported("libc", verify.MemberSkipped, "libb"),
		},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["liba"].Run.State)

	assert.Equal(t, record.Blocked, js["libb"].Run.State)
	assert.Equal(t, "liba", js["libb"].Run.Blamed)
	assert.Equal(t, "liba fails to build; this member is untested", js["libb"].Run.Detail)

	assert.Equal(t, record.Blocked, js["libc"].Run.State)
	assert.Equal(t, "libb", js["libc"].Run.Blamed, "blamed on its own prerequisite, not on the root of the chain")
	assert.Equal(t, "libb could not be built, so this member was not built; it is untested", js["libc"].Run.Detail)
}

// A failure in the middle of the cohort takes down exactly what depends
// on it. Four members, four answers: the headline passed before it, the
// member that broke failed, the sibling that needs only the headline
// passed after it, and the member that needs the one that broke is
// blocked on it.
func TestAMiddleFailureBlocksOnlyItsDependents(t *testing.T) {
	log := verify.SubjectMarker("libfoo") + "\n" +
		"--->  0 errors and 0 warnings found.\n" +
		"--->  Installing libfoo @2.0_0\n" +
		verify.SubjectMarker("bar") + "\n" +
		"Error: Failed to build bar: command execution failed\n" +
		verify.SubjectMarker("baz") + "\n" +
		"--->  0 errors and 2 warnings found.\n" +
		"--->  Installing baz @1.0_1\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "libfoo"}, {Port: "bar"}, {Port: "baz"}, {Port: "qux"}},
		Runs: map[string]record.Run{
			"libfoo": running(true), "bar": running(true), "baz": running(true), "qux": running(true),
		},
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:    log, LogRead: true,
		Reported: []verify.MemberState{
			reported("libfoo", verify.MemberPassed),
			reported("bar", verify.MemberFailed),
			reported("baz", verify.MemberPassed),
			reported("qux", verify.MemberSkipped, "bar"),
		},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Passed, js["libfoo"].Run.State)
	assert.Equal(t, "clean", js["libfoo"].Run.Lint)
	assert.Equal(t, record.Failed, js["bar"].Run.State)
	assert.Equal(t, record.Passed, js["baz"].Run.State)
	assert.Equal(t, "2 warnings", js["baz"].Run.Lint)
	assert.Equal(t, record.Blocked, js["qux"].Run.State)
	assert.Equal(t, "bar", js["qux"].Run.Blamed, "skipped for bar, and libfoo — which passed — is not blamed")
}

// Several strangers, each named. The runner goes on past a member whose
// dependency broke, so the next member's dependency can break too, and
// each member is blocked on its own stranger with its own nomaintainer
// answer — the caller looks each one up, and CohortBlame names them in
// build order, once each.
func TestEachStrangerIsNamedAndLookedUpOnItsOwn(t *testing.T) {
	log := verify.SubjectMarker("jq") + "\n" +
		"Error: Failed to build pkgconfig: command execution failed\n" +
		verify.SubjectMarker("mise") + "\n" +
		"Error: Failed to build rust: command execution failed\n" +
		verify.SubjectMarker("fzf") + "\n" +
		"Error: Failed to build pkgconfig: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "jq"}, {Port: "mise"}, {Port: "fzf"}},
		Runs:     map[string]record.Run{"jq": running(true), "mise": running(true), "fzf": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{
			reported("jq", verify.MemberFailed), reported("mise", verify.MemberFailed), reported("fzf", verify.MemberFailed),
		},
		Nomaintainer: map[string]bool{"rust": true},
	}

	assert.Equal(t, []string{"pkgconfig", "rust"}, CohortBlame(in))

	js := JudgeCohort(in)
	assert.Equal(t, "dependency pkgconfig fails to build; the change itself is untested", js["jq"].Run.Detail)
	assert.Equal(t, "dependency rust (nomaintainer) fails to build; the change itself is untested", js["mise"].Run.Detail)
	assert.Equal(t, "dependency pkgconfig fails to build; the change itself is untested", js["fzf"].Run.Detail)
	for _, m := range []string{"jq", "mise", "fzf"} {
		assert.Equal(t, record.Blocked, js[m].Run.State, m)
		assert.Empty(t, js[m].Run.Blamed, "%s: a stranger is not a member", m)
	}
}

// A log that could not be read, with the record intact. The record
// says who passed, who failed and who was skipped; the log would have
// said why, and without it a failure carries no diagnosis and a pass no
// lint line — but neither becomes a guess about the other.
func TestAnUnreadableLogStillReadsTheRecord(t *testing.T) {
	in := CohortInput{
		Subjects: []record.Subject{{Port: "libfoo"}, {Port: "bar"}, {Port: "qux"}},
		Runs:     map[string]record.Run{"libfoo": running(true), "bar": running(true), "qux": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Reported: []verify.MemberState{
			reported("libfoo", verify.MemberPassed),
			reported("bar", verify.MemberFailed),
			reported("qux", verify.MemberSkipped, "bar"),
		},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Passed, js["libfoo"].Run.State)
	assert.Empty(t, js["libfoo"].Run.Lint, "no log, no lint line to corroborate")
	assert.Equal(t, record.Failed, js["bar"].Run.State)
	assert.Empty(t, js["bar"].Run.Detail, "no log, no diagnosis")
	assert.Equal(t, KeepWorker, js["bar"].Release)
	assert.Equal(t, record.Blocked, js["qux"].Run.State)
	assert.Equal(t, "bar", js["qux"].Run.Blamed)
	assert.Empty(t, CohortBlame(in), "nothing to read a stranger out of")
}

// A record the judge cannot act on is a runner fault and read as one:
// a skip naming nobody in the change, and a member the guest recorded
// nothing about while recording the others, are both errored — the
// guest reported no outcome for them, and nothing is invented.
func TestARecordTheJudgeCannotActOnIsARunnerFault(t *testing.T) {
	log := verify.SubjectMarker("liba") + "\n" +
		"Error: Failed to build liba: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "liba"}, {Port: "libb"}, {Port: "libc"}},
		Runs:     map[string]record.Run{"liba": running(true), "libb": running(true), "libc": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{
			reported("liba", verify.MemberFailed),
			reported("libb", verify.MemberSkipped),
		},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["liba"].Run.State)
	for _, m := range []string{"libb", "libc"} {
		assert.Equal(t, record.Errored, js[m].Run.State, m)
		assert.Equal(t, "the guest reported no output for this subject", js[m].Run.Detail, m)
		assert.Empty(t, js[m].Run.Blamed, m)
		assert.Equal(t, ReleaseQuietly, js[m].Release, m)
	}
}

// Without the record, the log alone decides how far the runner got,
// and it decides it in the FILE's order. A runner that returned to a
// member it had already built would announce it again, and the member
// the guest was inside when it gave up is the one the last marker
// names — not the one that sorts last in the roster, which here is the
// member that finished cleanly and went on to be announced after.
func TestTheLogAloneTakesTheLastMarkerInFileOrder(t *testing.T) {
	log := verify.SubjectMarker("oniguruma") + "\n" +
		"--->  0 errors and 0 warnings found.\n" +
		"--->  Installing oniguruma @6.9.10_0\n" +
		verify.SubjectMarker("jq") + "\n" +
		"--->  0 errors and 1 warnings found.\n" +
		"--->  Installing jq @1.8.0_1\n" +
		verify.SubjectMarker("oniguruma") + "\n" +
		"--->  Testing oniguruma\n" +
		"Error: Failed to test oniguruma: command execution failed\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma"}, {Port: "jq"}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Failed, js["oniguruma"].Run.State, "the member the file announced last")
	assert.Equal(t, "Failed to test oniguruma: command execution failed", js["oniguruma"].Run.Detail)
	assert.Equal(t, record.Passed, js["jq"].Run.State, "announced, clean, and the file went on past it")
	assert.Equal(t, "1 warning", js["jq"].Run.Lint)
}

// A sibling named under another member's marker, with no reading of
// its own, takes the failure printed there: the headline's install
// pulled it out of the overlay and it broke, and the roster is what
// tells it from a stranger. The environment stays, because the
// breakage is this change's own — whichever of the two the roster
// lists first.
func TestASiblingNamedUnderAnotherMemberTakesTheFailure(t *testing.T) {
	log := verify.SubjectMarker("jq") + "\n" +
		"--->  Dependencies to be installed: oniguruma\n" +
		"Error: Failed to build oniguruma: command execution failed\n" +
		"Error: Processing of port jq failed\n"
	for _, order := range [][]string{{"jq", "oniguruma"}, {"oniguruma", "jq"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			in := CohortInput{
				Runs:   map[string]record.Run{"jq": running(true), "oniguruma": running(true)},
				Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
				Log:    log, LogRead: true,
				Reported: []verify.MemberState{
					reported("jq", verify.MemberFailed),
				},
			}
			for _, p := range order {
				in.Subjects = append(in.Subjects, record.Subject{Port: p})
			}
			js := JudgeCohort(in)

			assert.Equal(t, record.Failed, js["oniguruma"].Run.State, "the failure printed under jq is oniguruma's")
			assert.Equal(t, "Failed to build oniguruma: command execution failed", js["oniguruma"].Run.Detail)
			assert.Equal(t, KeepWorker, js["oniguruma"].Release)

			assert.Equal(t, record.Blocked, js["jq"].Run.State)
			assert.Equal(t, "oniguruma", js["jq"].Run.Blamed)
			assert.Equal(t, "oniguruma fails to build; this member is untested", js["jq"].Run.Detail)
			assert.Empty(t, CohortBlame(in), "a sibling is not a stranger")
		})
	}
}

// A sibling that the log names under one member and that then built on
// its own leaves the failure where it was printed. The record says the
// sibling passed; what is true of the member whose install broke on it
// is that its own build failed, and the detail says on what.
func TestASiblingThatPassedOnItsOwnLeavesTheFailureWhereItWasPrinted(t *testing.T) {
	log := verify.SubjectMarker("jq") + "\n" +
		"Error: Failed to build oniguruma: command execution failed\n" +
		verify.SubjectMarker("oniguruma") + "\n" +
		"--->  0 errors and 0 warnings found.\n" +
		"--->  Installing oniguruma @6.9.10_0\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "jq"}, {Port: "oniguruma"}},
		Runs:     map[string]record.Run{"jq": running(true), "oniguruma": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
		Reported: []verify.MemberState{reported("jq", verify.MemberFailed), reported("oniguruma", verify.MemberPassed)},
	}
	js := JudgeCohort(in)

	assert.Equal(t, record.Passed, js["oniguruma"].Run.State, "its own section and its own record say so")
	assert.Equal(t, record.Failed, js["jq"].Run.State, "the failure is jq's own to look into")
	assert.Equal(t, "Failed to build oniguruma: command execution failed", js["jq"].Run.Detail)
	assert.Empty(t, js["jq"].Run.Blamed, "a member that built is not blamed for one that did not")
	assert.Equal(t, KeepWorker, js["jq"].Release)
}
