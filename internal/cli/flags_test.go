package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/verify"
)

// THE ARITY CHECK IS THE ONE THAT CANNOT BE ASKED AT PARSE TIME, and
// every flag app.SurveyRequest names as "refused by arity" is refused
// here rather than accepted and dropped. The two that mattered most are
// the two that did the OPPOSITE of what was typed: --in-place, whose
// own help says "no branch, no commit", reached app.Survey and minted a
// committed branch per port; and --to, which copied one version literal
// into every plan of a category.
func TestTheFlagsRefusedByArity(t *testing.T) {
	for _, c := range []struct {
		what   string
		params intent.Params
		flags  intentFlags
		says   string
	}{
		{"--to-pr", intent.Params{}, intentFlags{toPR: true}, "--to-pr"},
		{"--in-place", intent.Params{}, intentFlags{inPlace: true}, "--in-place"},
		{"--diff", intent.Params{}, intentFlags{diff: true}, "--diff"},
		{"--replace", intent.Params{}, intentFlags{replace: true}, "--replace"},
		{"--timeout", intent.Params{}, intentFlags{timeoutSet: true, timeout: time.Minute}, "--timeout"},
		{"--to", intent.Params{Version: "1.8.2"}, intentFlags{}, "--to"},
		{"--closes", intent.Params{ClosesTicket: "4127"}, intentFlags{}, "--closes"},
	} {
		f := c.flags
		err := refuseByArity(3, c.params, &f)
		require.Error(t, err, "%s is accepted under a selector naming three ports", c.what)
		assert.Equal(t, exitcode.Usage, ExitCode(err), "%s must be the invocation's problem", c.what)
		assert.Contains(t, err.Error(), c.says)
		assert.Contains(t, err.Error(), "one port",
			"%s: a refusal names the road that does support the flag", c.what)

		// And the same flags are fine on the single-port road, which is
		// the whole point of the check being about ARITY.
		assert.NoError(t, refuseByArity(1, c.params, &f), "%s under one port", c.what)
	}
}

// THE FLAGS A SWEEP CARRIES ARE NOT REFUSED. --plan is the one
// write-nothing mode a selector can honour, and --no-verify, --test,
// --keep-env, --riders and --on ride on app.SurveyRequest.
func TestTheFlagsASweepCarriesAreNotRefusedByArity(t *testing.T) {
	for _, f := range []intentFlags{
		{planOnly: true}, {noVerify: true}, {test: true}, {keepEnv: true},
		{riders: true}, {noRiders: true}, {on: "sequoia"},
	} {
		assert.NoError(t, refuseByArity(400, intent.Params{}, &f), "%+v", f)
	}
}

// --plan UNDER A SELECTOR EMITS EVERY PLAN. The sweep road planned each
// target, threw the plan away and printed a census of zeroes: a caller
// who asked for "the plan on stdout as JSON and change nothing" got one
// line of prose and no document at all, for work that had been fully
// computed. One JSON object per target, in selector order, so a
// consumer reads the two arities with one decoder.
func TestPlanUnderASelectorEmitsEveryPlan(t *testing.T) {
	var out, errOut bytes.Buffer
	s := &Services{Out: &out, Err: &errOut, Tools: testFinder(), Now: time.Now}
	f := &intentFlags{planOnly: true}
	targets := []tree.Target{{Portdir: "/tree/devel/ivy"}, {Portdir: "/tree/devel/jq"}}

	next := sweepPool(s, intentVerb{}, intent.Params{}, f, targets, upstream.Manners{},
		func(_ context.Context, t tree.Target, _ intent.Params) (*plan.Plan, error) {
			return &plan.Plan{Portdir: t.Portdir, Slug: "slug-" + t.Portdir}, nil
		},
		func(context.Context, *plan.Plan) (change.Prepared, error) {
			t.Error("--plan prepared a change against a base commit it will never commit over")
			return change.Prepared{}, nil
		})
	for {
		if _, ok := next(context.Background()); !ok {
			break
		}
	}

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var seen []string
	for dec.More() {
		var got plan.Plan
		require.NoError(t, dec.Decode(&got))
		seen = append(seen, got.Portdir)
	}
	assert.Equal(t, []string{"/tree/devel/ivy", "/tree/devel/jq"}, seen,
		"one document per target, in selector order")

	// Without --plan the pool writes nothing to either stream: the sweep
	// road's own output is the census and the progress sink.
	out.Reset()
	quiet := &intentFlags{}
	next = sweepPool(s, intentVerb{}, intent.Params{}, quiet, targets, upstream.Manners{},
		func(_ context.Context, t tree.Target, _ intent.Params) (*plan.Plan, error) {
			return &plan.Plan{Portdir: t.Portdir}, nil
		},
		func(context.Context, *plan.Plan) (change.Prepared, error) { return change.Prepared{}, nil })
	for {
		if _, ok := next(context.Background()); !ok {
			break
		}
	}
	assert.Empty(t, out.String(), "a sweep with no --plan emits no documents")
}

// STDOUT UNDER --plan IS THE DOCUMENT STREAM AND NOTHING ELSE, which is
// what makes the emission above usable: the census is a person's line
// and would sit in the middle of the JSON a caller is piping into jq.
func TestTheSweepCensusLeavesStdoutToThePlanDocuments(t *testing.T) {
	var out, errOut bytes.Buffer
	s := &Services{Out: &out, Err: &errOut}
	assert.Same(t, &errOut, sweepCensus(s, &intentFlags{planOnly: true}))
	assert.Same(t, &out, sweepCensus(s, &intentFlags{}))
}

// AN UNKNOWN SUBCOMMAND UNDER `provision` IS THE INVOCATION'S PROBLEM.
// It exited 0 with provision's help on stdout, because cobra answers an
// unrunnable command with flag.ErrHelp BEFORE it validates arguments —
// so a Makefile carrying the pre-overhaul `provision xcode` reported
// success while provisioning nothing.
func TestAnUnknownSubcommandUnderProvisionIsAUsageError(t *testing.T) {
	requireNoTree(t)
	assert.Equal(t, exitcode.Usage, code(t, "provision", "xcode"),
		"the retired flat spelling must not report success")
	assert.Equal(t, exitcode.Usage, code(t, "provision", "bogus", "subcmd"))
	assert.Equal(t, exitcode.OK, code(t, "provision"),
		"a bare `provision` still prints its help and exits 0")

	// And the retired spelling is answered by name, because cobra's own
	// suggestions search children and the new spelling is a grandchild.
	err := runCLI(t, "provision", "xcode")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provision tart xcode")
}

// --no-verify DOES NOT TELL A MACHINE WITH tart TO INSTALL tart. The
// advisory is a fact about the machine and --no-verify is a fact about
// the invocation, and they reached app through the same nil.
func TestNoVerifyIsNotADiagnosisOfTheMachine(t *testing.T) {
	minted := app.Result{
		Did:      app.Minted,
		Deferred: &app.Deferral{Reason: app.NoProvider, Detail: "unverified; install tart and `dockhand verify`"},
	}
	assert.Nil(t, quietWhereNoBuildWasAsked(minted, true).Deferred,
		"a caller who asked for no build is told nothing about a provider")

	// A change that ASKED to be built and found no provider is owed the
	// sentence. It is read off the ASK and no longer off the delivery,
	// which is what makes --no-verify --to-pr quiet too: that road asks
	// for no build either, and it is the very road the advisory would
	// have instructed to install software it does not need.
	assert.NotNil(t, quietWhereNoBuildWasAsked(minted, false).Deferred)

	// And a deferral of another kind is never touched.
	other := app.Result{Did: app.Minted, Deferred: &app.Deferral{Reason: app.NoEnvironment, Detail: "no base image"}}
	assert.NotNil(t, quietWhereNoBuildWasAsked(other, true).Deferred)
}

// `status --json` CARRIES THE VERIFICATION STANDING. record.ChangeState
// has no verdict values, so the five-field document answered a passed
// change, a failed one, three queued and one nobody ever asked to build
// with the same `"state":"minted"`: a dashboard over twelve changes
// could not find the failure and reported the fleet healthy.
func TestStatusDocumentCarriesTheVerificationStanding(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	res := app.StatusResult{
		State: statestore.State{
			Changes: map[string]record.Change{
				"c1": {ID: "c1", Branch: "dockhand/jq-1.8.2", State: record.ChangeMinted, Tip: "aaa",
					Findings: []record.Finding{{Disposition: record.Proposed, Criterion: "libjq.1 -> libjq.2"}}},
				"c2": {ID: "c2", Branch: "dockhand/ivy-2.6", State: record.ChangeMinted, Tip: "bbb",
					Hold: &record.Hold{Reason: "waiting on upstream"}},
			},
			Attempts: map[string]record.Attempt{
				"a1": {ID: "a1", Change: "c1", Sha: "aaa", Platform: "sequoia", Phase: record.Finished,
					Started: now.Add(-2 * time.Hour),
					Runs:    map[string]record.Run{"jq": {State: record.Failed, Detail: "build failed"}}},
				"a2": {ID: "a2", Change: "c2", Sha: "bbb", Platform: "sequoia", Phase: record.Requested},
			},
		},
		Facts: map[record.ChangeID]publish.Facts{
			"c1": {Forge: publish.ForgeFacts{
				OwnFound: true, Fresh: false, AsOf: now.Add(-2 * time.Hour),
				Own: gh.PullRequest{Number: 4127, State: "open", HTMLURL: "https://example.invalid/4127"},
			}},
		},
		Disagreeing: []*change.TipDisagreement{{ID: "c3", Ref: "refs/heads/dockhand/zlib", Recorded: "ccc", Absent: true}},
		Vacancy:     verify.Vacancy{Known: true, Free: 1, Limit: 2, AsOf: now},
	}

	doc := statusDoc(res, nil)
	require.Len(t, doc.Changes, 2)
	byID := map[record.ChangeID]changeDoc{}
	for _, c := range doc.Changes {
		byID[c.ID] = c
	}

	failed := byID["c1"]
	require.Len(t, failed.Attempts, 1)
	require.Len(t, failed.Attempts[0].Runs, 1)
	assert.Equal(t, record.Failed, failed.Attempts[0].Runs[0].State,
		"the verdict is the one thing a verification exists to produce")
	assert.True(t, failed.Attempts[0].Current, "its sha is the change's tip")
	assert.True(t, failed.Attempts[0].Settled)
	assert.False(t, failed.Attempts[0].Queued)
	require.NotNil(t, failed.PR)
	assert.Equal(t, 4127, failed.PR.Number)
	assert.False(t, failed.PR.Fresh, `"open" and "open when I last looked" are different claims`)
	assert.Equal(t, now.Add(-2*time.Hour), failed.PR.AsOf)
	assert.Equal(t, []string{"libjq.1 -> libjq.2"}, failed.Proposes)

	queued := byID["c2"]
	require.Len(t, queued.Attempts, 1)
	assert.True(t, queued.Attempts[0].Queued, "a queued attempt is not a failed one")
	assert.Empty(t, queued.Attempts[0].Runs)
	assert.True(t, queued.Held)
	assert.Equal(t, "waiting on upstream", queued.HoldReason)

	require.Len(t, doc.Disagreeing, 1)
	assert.Equal(t, "refs/heads/dockhand/zlib", doc.Disagreeing[0].Ref)
	assert.True(t, doc.Disagreeing[0].Absent)
}

// EVERY KEY IS TAGGED, INCLUDING THE ONES THAT CAME FROM ANOTHER
// PACKAGE'S STRUCT. verify.Vacancy declares no json tags, so embedding
// it spelled the vacancy object in Go's PascalCase and a script reading
// `.vacancy.free` read null while the human report printed "1 of 2
// environments free" in the same run.
func TestStatusDocumentTagsEveryKey(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	doc := statusDoc(app.StatusResult{Vacancy: verify.Vacancy{Known: true, Free: 1, Limit: 2, AsOf: now}}, nil)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	// Read back as raw members so the assertions are about the WIRE —
	// the key a consumer types and the bytes it gets — rather than about
	// whatever Go type a decoder chose.
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "[]", string(got["changes"]), "an empty listing is [] and never null")

	var vacancy map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got["vacancy"], &vacancy), "the document has a vacancy object: %s", raw)
	assert.Equal(t, "1", string(vacancy["free"]))
	assert.Equal(t, "2", string(vacancy["limit"]))
	assert.Equal(t, "true", string(vacancy["known"]), `"full" and "I could not find out" are two facts`)
	assert.NotContains(t, vacancy, "Free", "a Go field name in a machine-readable contract")
	assert.NotContains(t, vacancy, "Limit")
	assert.NotContains(t, vacancy, "AsOf")
}

// AN OBLIGATION IS AN OBJECT AND NOT A LEASE ID. An Untracked
// obligation is built from provider inventory and never has a lease, so
// two leaking guests serialized as ["", ""] — two entries that could
// not be told apart, correlated with nothing, and passable to no
// follow-up command, while the human report named both.
func TestStatusDocumentNamesEveryObligation(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	res := app.StatusResult{Obligations: []lease.Obligation{
		{Kind: lease.Untracked, Standing: lease.Mine, Platform: "sequoia",
			Worker: "dockhand-worker-deadbeef", Job: verify.Job{Provider: "tart", ID: "deadbeef"},
			Since: now.Add(-time.Hour), Why: "an environment no lease accounts for"},
		{Kind: lease.Untracked, Standing: lease.ForeignRoot, Platform: "tahoe",
			Worker: "dockhand-worker-c0ffee", Root: "/Users/other/ports",
			Job: verify.Job{Provider: "tart", ID: "c0ffee"}},
	}}

	doc := statusDoc(res, nil)
	require.Len(t, doc.Obligations, 2)

	mine := doc.Obligations[0]
	assert.Equal(t, "untracked", mine.Kind, "a word, not this package's iota order")
	assert.Equal(t, "mine", mine.Standing)
	assert.True(t, mine.Seizable)
	assert.Equal(t, "dockhand-worker-deadbeef", mine.Worker, "the name a follow-up command takes")
	assert.Equal(t, "deadbeef", mine.Job)
	assert.Equal(t, "sequoia", mine.Platform)
	assert.Empty(t, mine.Lease, "an untracked worker has no lease, and says so by omission")

	foreign := doc.Obligations[1]
	assert.Equal(t, "foreign-root", foreign.Standing)
	assert.False(t, foreign.Seizable, "a repository copied elsewhere must not stop a VM it does not own")
	assert.Equal(t, "/Users/other/ports", foreign.Root, "a foreign obligation is NAMED")
	assert.NotEqual(t, mine.Worker, foreign.Worker, "two obligations are two identities")

	// And they survive the wire as objects.
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back struct {
		Obligations []struct {
			Worker string `json:"worker"`
			Kind   string `json:"kind"`
		} `json:"obligations"`
	}
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Len(t, back.Obligations, 2)
	assert.Equal(t, "dockhand-worker-deadbeef", back.Obligations[0].Worker)
	assert.Equal(t, "untracked", back.Obligations[0].Kind)
}
