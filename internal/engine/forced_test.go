package engine

// The D24 override at the verb and on the roads after it. A withheld
// member a person forces is seated last with its sibling deactivated;
// the note records the seat on the candidate and on the run; and every
// road that resubmits the cohort — the drain, and a hand verify after a
// --no-verify — reads the record rather than the tree, so a withheld
// member stays out and a forced one goes back last.
//
// The judy fixture's index holds no conflicting pair, so the withheld
// candidate is written onto the measured proposal by hand, exactly as
// verdict would have written it: Solo, with Over naming the seated
// sibling.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

const withheldOther = "depends_lib; conflicts with netdata, which this cohort builds — bumped here, and not built"

// judyWithAWithheldMember is the judy cohort settled to a real proposal,
// with `other` written onto it as a member withheld behind netdata.
func judyWithAWithheldMember(t *testing.T) (*git.Repo, *verifytest.Fake, *Engine) {
	t.Helper()
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(judyMoved())
	fake.Logs = map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"}
	e := indexed(t, repo, fake)
	require.NoError(t, e.settle(ctx, repo, &n))
	require.True(t, AnyProposed(n))
	require.NoError(t, ledger.Open(repo).Update(ctx, sha, func(r *record.Record) error {
		for i := range r.Findings {
			if r.Findings[i].Kind == render.KindCohort {
				r.Findings[i].Candidates = append(r.Findings[i].Candidates, record.Candidate{
					Port: "other", Portdir: "sysutils/other", Proposed: true,
					Solo: true, Over: "netdata", Reason: withheldOther})
			}
		}
		return nil
	}))
	return repo, fake, e
}

func candidateOn(t *testing.T, repo *git.Repo, tip, port string) record.Candidate {
	t.Helper()
	n, err := ledger.Open(repo).Read(context.Background(), tip)
	require.NoError(t, err)
	for _, f := range n.Findings {
		if f.Kind != render.KindCohort {
			continue
		}
		for _, c := range f.Candidates {
			if c.Port == port {
				return c
			}
		}
	}
	require.Failf(t, "no candidate", "%s is not on the note's cohort finding", port)
	return record.Candidate{}
}

func portsOf(members []Member) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Port)
	}
	return out
}

// --force-withheld at the verb: the forced member is seated last with
// its sibling in Deactivate, its run carries the sibling, no withheld
// run is written for it, and the note's candidate says it was forced —
// in the reason a reviewer reads and on the key the roads read.
func TestForcingAWithheldMemberSeatsItLastWithItsSiblingDeactivated(t *testing.T) {
	ctx := context.Background()
	repo, fake, e := judyWithAWithheldMember(t)
	var out, errOut bytes.Buffer
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{Force: []string{"OTHER"}}))

	require.Len(t, fake.Submitted, 1)
	req := fake.Submitted[0]
	assert.Equal(t, []string{"judy", "netdata", "other"}, req.Ports, "the forced member is last")
	assert.Equal(t, []string{"", "", "netdata"}, req.Deactivate, "and only it deactivates anything")
	assert.Contains(t, errOut.String(), "other: forced into the build, last; netdata will be deactivated first")

	tip := strings.TrimSpace(out.String())
	fresh, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	run := runFor(fresh, "other", "Testos")
	assert.Equal(t, record.Running, run.State, "built, not withheld")
	assert.Equal(t, "netdata", run.Forced)
	for key, r := range fresh.Runs {
		assert.NotEqual(t, record.Withheld, r.State, "no withheld run is written for a forced member: %s", key)
	}
	c := candidateOn(t, repo, tip, "other")
	assert.True(t, c.Forced, "the override is a fact on the candidate, not only prose")
	assert.True(t, c.Solo && c.Proposed)
	assert.Equal(t, "netdata", c.Over)
	assert.Contains(t, c.Reason, "forced into the build at the maintainer's request, with netdata deactivated first")
	assert.Contains(t, render.CohortBody(fresh), "forced into the build at the maintainer's request",
		"the pull request body reads the accepted proposal's candidates")
}

// Without the override the same member is withheld: kept out of the
// guest, recorded withheld with the sibling read from Over.
func TestAWithheldMemberStaysOutWithoutTheOverride(t *testing.T) {
	ctx := context.Background()
	repo, fake, e := judyWithAWithheldMember(t)
	var out bytes.Buffer
	eng := *e
	eng.Out = &out
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{}))
	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"judy", "netdata"}, fake.Submitted[0].Ports)
	assert.Nil(t, fake.Submitted[0].Deactivate, "a request that forces nobody is the request there always was")
	fresh, err := ledger.Open(repo).Read(ctx, strings.TrimSpace(out.String()))
	require.NoError(t, err)
	run := runFor(fresh, "other", "Testos")
	assert.Equal(t, record.Withheld, run.State)
	assert.Equal(t, "it conflicts with netdata, which this cohort builds", run.Detail)
	assert.False(t, candidateOn(t, repo, strings.TrimSpace(out.String()), "other").Forced)
}

// --exclude is written onto the note the same way: the excluded member
// is no longer a bumped member in the pull request body.
func TestExcludeIsWrittenOntoTheNote(t *testing.T) {
	ctx := context.Background()
	repo, _, e := judyWithAWithheldMember(t)
	var out bytes.Buffer
	eng := *e
	eng.Out = &out
	require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true, Exclude: []string{"other"}}))
	tip := strings.TrimSpace(out.String())
	c := candidateOn(t, repo, tip, "other")
	assert.False(t, c.Proposed)
	assert.Equal(t, excludedReason, c.Reason)
	fresh, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	body := render.CohortBody(fresh)
	bumped, _, _ := strings.Cut(body, "Examined and not bumped")
	assert.NotContains(t, bumped, "other (sysutils/other)", "an excluded member is not listed as bumped")
	assert.Contains(t, body, excludedReason)
}

// --exclude of the sibling and --force-withheld of the member it lost
// to is declined: with the sibling out of the change there is nothing
// to deactivate, and a forced build that must fail is no override.
func TestForceRefusesAMemberWhoseSiblingIsExcluded(t *testing.T) {
	ctx := context.Background()
	repo, fake, e := judyWithAWithheldMember(t)
	err := e.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{Exclude: []string{"netdata"}, Force: []string{"other"}})
	var cannot *CannotForceError
	require.ErrorAs(t, err, &cannot)
	assert.Equal(t, "other", cannot.Port)
	assert.Contains(t, err.Error(), "netdata, which --exclude leaves out of this change")
	assert.Equal(t, exitcode.PlanDeclined, cannot.DockhandExit())
	assert.Equal(t, "cannot-force", cannot.Code())
	assert.Empty(t, fake.Submitted, "declined: nothing built")
}

// A withheld candidate whose record does not say which sibling it lost
// to cannot be forced: seating it would deactivate nothing and record a
// force that never happened.
func TestForceRefusesAWithheldMemberWithNoRecordedSibling(t *testing.T) {
	f := record.Finding{Candidates: []record.Candidate{
		{Port: "gegl", Proposed: true, Reason: "depends_lib"},
		{Port: "gegl-devel", Proposed: true, Solo: true, Reason: "depends_lib; conflicts with gegl — bumped here, and not built"},
	}}
	_, forced, err := forceMembers(f, []string{"gegl-devel"})
	var cannot *CannotForceError
	require.ErrorAs(t, err, &cannot)
	assert.Nil(t, forced)
	assert.Equal(t, "gegl-devel", cannot.Port)
	assert.Contains(t, err.Error(), "does not say which member it conflicts with")
}

// A cohort accepted with --no-verify writes no run, so a hand
// `dockhand verify <branch>` afterwards has only the note's candidates
// to go by — and they are enough: the forced member is seated last with
// its sibling, and without the override the withheld member stays out
// and is recorded withheld under the release this attempt resolves.
func TestAHandVerifyAfterNoVerifyReadsTheOverrideOffTheNote(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force []string
		ports []string
		deact []string
	}{
		{"forced", []string{"other"}, []string{"judy", "netdata", "other"}, []string{"", "", "netdata"}},
		{"withheld", nil, []string{"judy", "netdata"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, fake, e := judyWithAWithheldMember(t)
			var out bytes.Buffer
			eng := *e
			eng.Out = &out
			require.NoError(t, eng.BuildCohort(ctx, repo, "dockhand/judy-1.1", CohortOpts{NoVerify: true, Force: tc.force}))
			require.Empty(t, fake.Submitted, "--no-verify: nothing submitted, nothing recorded")
			tip := strings.TrimSpace(out.String())

			rels, err := eng.ChangedPortdirs(ctx, repo, "dockhand/judy-1.1", tip)
			require.NoError(t, err)
			members, err := eng.SubjectsOf(ctx, repo, "dockhand/judy-1.1", "dockhand/judy-1.1", tip, rels)
			require.NoError(t, err)
			assert.Contains(t, portsOf(members), "other", "the walk turns the withheld member up: its Portfile was bumped")

			started, err := eng.SubmitRelease(ctx, repo, "dockhand/judy-1.1", tip, members, fake.Capabilities().Platforms[0], SubmitOpts{})
			require.NoError(t, err)
			require.True(t, started)
			require.Len(t, fake.Submitted, 1)
			assert.Equal(t, tc.ports, fake.Submitted[0].Ports)
			assert.Equal(t, tc.deact, fake.Submitted[0].Deactivate)

			fresh, err := ledger.Open(repo).Read(ctx, tip)
			require.NoError(t, err)
			run := runFor(fresh, "other", "Testos")
			if tc.force != nil {
				assert.Equal(t, record.Running, run.State)
				assert.Equal(t, "netdata", run.Forced)
			} else {
				assert.Equal(t, record.Withheld, run.State, "recorded withheld by the hand verify, so its line is on the body")
				assert.Equal(t, "it conflicts with netdata, which this cohort builds", run.Detail)
			}
		})
	}
}

// A machine with no environment queues the roster before any release
// resolves; the withheld member is recorded withheld at that moment too,
// so the drain that later starts the cohort finds the run that keeps it
// out rather than a changed portdir it must build.
func TestANoEnvironmentDeferralKeepsTheWithheldMemberOut(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Destination = record.ToVerdict
	n.Subjects = []record.Subject{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	withProvider := eng.Verifier
	eng.Verifier = func(context.Context) (verify.Verifier, error) { return nil, verify.ErrNoEnvironment }
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}
	err = eng.submit(ctx, m, submission{
		Port: "jq", Release: fake.Capabilities().Platforms[0],
		Members:  []Member{{Port: "jq", Portdir: "sysutils/jq"}},
		Withheld: []WithheldMember{{Port: "oniguruma", Why: "it conflicts with jq, which this cohort builds"}},
	})
	var vde *VerifyDeferredError
	require.ErrorAs(t, err, &vde)
	queued, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, runFor(queued, "jq", "Testos").State)
	assert.Equal(t, record.Withheld, runFor(queued, "oniguruma", "Testos").State,
		"withheld is decided before any environment is asked for, and recorded with the deferral")

	eng.Verifier = withProvider
	eng.Tools = pumpTools(t)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})
	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports, "the withheld member is not in the guest")
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Withheld, runFor(again, "oniguruma", "Testos").State, "and its withheld run stands")
	assert.Equal(t, record.Running, runFor(again, "jq", "Testos").State)
}

// The drain leaves a withheld member out of the retry even though its
// Portfile is a changed portdir the walk turns up.
func TestTheDrainLeavesAWithheldMemberOut(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Destination = record.ToVerdict
	n.Subjects = []record.Subject{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	n.Runs[record.RunKey("oniguruma", "Testos")] = record.Run{State: record.Withheld, Platform: "Testos",
		Detail: "it conflicts with jq, which this cohort builds"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports)
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Withheld, runFor(again, "oniguruma", "Testos").State)
	assert.Equal(t, record.Running, runFor(again, "jq", "Testos").State)
}

// A second deferral re-records the run the walk claimed with its Forced
// intact, the way FromSource and KeepEnv are kept: the attempt after it
// must still seat the member last with its sibling deactivated.
func TestASecondDeferralKeepsTheForcedSibling(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Destination = record.ToVerdict
	// The forced member is the subject the walk reaches first, so the
	// run re-recorded on the deferral is its own.
	n.Subjects = []record.Subject{
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
		{Port: "jq", Portdir: "sysutils/jq"},
	}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	n.Runs[record.RunKey("oniguruma", "Testos")] = record.Run{
		State: record.Queued, Platform: "Testos", Detail: "all slots busy", Forced: "oniguruma-devel"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	run := runFor(again, "oniguruma", "Testos")
	assert.Equal(t, record.Queued, run.State)
	assert.Equal(t, "oniguruma-devel", run.Forced, "the re-queue carried the sibling")

	fake.SubmitErr = nil
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})
	require.NotEmpty(t, fake.Submitted)
	last := fake.Submitted[len(fake.Submitted)-1]
	assert.Equal(t, []string{"jq", "oniguruma"}, last.Ports, "last, whatever order the walk found it in")
	assert.Equal(t, []string{"", "oniguruma-devel"}, last.Deactivate)
}
