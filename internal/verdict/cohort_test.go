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
			// lookup: one subject blames what it has always blamed.
			in := soloCohort(exp.Port, running(true))
			in.Status, in.Log, in.LogRead = st, log, true
			dep, blamed := CohortBlame(in)
			wantDep, wantBlamed := BlamedDependency(log, exp.Port)
			assert.Equal(t, wantDep, dep, "blamed port")
			assert.Equal(t, wantBlamed, blamed)
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

			in := cohortOf(exp.Members, string(raw), exp.Outcome)
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

			// A cohort blames a port outside the change or it blames
			// nobody, and it never blames one of its own members: the
			// nomaintainer note tells a person there is nobody to nudge
			// about somebody else's port, and a sibling is not one.
			if dep, blamed := CohortBlame(in); blamed {
				assert.NotContains(t, exp.Members, dep, "a member is never the stranger")
			}
		})
	}
}

// cohortOf is the corpus's roster as the settle hands it over: every
// member running inside one guest, with the one poll and the one log.
func cohortOf(members []string, log, outcome string) CohortInput {
	in := CohortInput{
		Status: verify.Status{State: verify.Failed, Handle: "fake-1"},
		Runs:   map[string]record.Run{},
		Log:    log, LogRead: true,
	}
	if outcome == "passed" {
		in.Status = verify.Status{State: verify.Passed, Handle: "fake-1"}
	}
	for _, m := range members {
		in.Subjects = append(in.Subjects, record.Subject{Port: m, Names: []string{m}})
		in.Runs[m] = running(true)
	}
	return in
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
	_, blamed := CohortBlame(in)
	assert.False(t, blamed, "a subport of a member is not a stranger")
}

// A member that declines the platform stopped the cohort without
// failing at anything, and the members behind it are told so in those
// words. A reader told "fails to build" would go looking for a
// breakage that is not there.
func TestADecliningMemberStopsTheCohortWithoutFailing(t *testing.T) {
	log := verify.SubjectMarker("oniguruma") + "\n" +
		"--->  Skipping oniguruma: known_fail on Monterey\n"
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"}}, {Port: "jq", Names: []string{"jq"}}},
		Runs:     map[string]record.Run{"oniguruma": running(true), "jq": running(true)},
		Status:   verify.Status{State: verify.Failed, Handle: "fake-1"},
		Log:      log, LogRead: true,
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

	dep, blamed := CohortBlame(in)
	assert.True(t, blamed, "the tree lookup must still be sent after pkgconfig")
	assert.Equal(t, "pkgconfig", dep)
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
	for _, m := range []string{"jq", "oniguruma"} {
		j := JudgeCohort(in)[m]
		assert.Equal(t, record.Blocked, j.Run.State, m)
		assert.Empty(t, j.Run.Blamed, "%s: the detail names a stranger, so nothing of ours is blamed", m)
	}
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
