package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// THE FIXTURES BELOW ARE WHAT MAKES THIS PACKAGE TESTABLE AT ALL, and
// they are the design's own claim cashed: nine operations over INJECTED
// VALUES, no file opened, no lock taken, no environment read. A pass, a
// verification, an extension, a status and a cancellation all run here
// against a real temporary git repository, a real state ref, a scripted
// provider and a scripted stager — no VM, no ports tree, no network.
//
// Before these existed, Cycle, Verify, Accept, Status and Cancel had no
// test that EXECUTED them: app_test.go constructed Change, Discard and
// Survey only, and every regression in the five sequencers those five
// operations are went uncaught by `go test ./...`.

// stager is the consumer-owned seam Start turns an attempt's identity
// into a staged directory through. It is run's own test double, stood up
// again here because what these tests drive is the operation above it.
type stager struct {
	err  error
	seen []string
}

func (s *stager) Stage(_ context.Context, sha string, subjects []record.Subject, _ platform.Release) ([]run.Member, map[string]run.Preflight, error) {
	s.seen = append(s.seen, sha)
	if s.err != nil {
		return nil, nil, s.err
	}
	members := make([]run.Member, 0, len(subjects))
	pre := map[string]run.Preflight{}
	for _, sub := range subjects {
		members = append(members, run.Member{Port: sub.Port, Portdir: "/stage/" + sub.Port, Names: sub.Names})
		pre[sub.Port] = run.Preflight{Read: true}
	}
	return members, pre, nil
}

// Baseline is the merge base's copies of the same subjects. The fake
// stages them under a second root, which is the real stager's shape:
// the two trees hold the same paths with different contents.
func (s *stager) Baseline(_ context.Context, sha string, subjects []record.Subject) ([]string, error) {
	if sha == "" {
		return nil, nil
	}
	out := make([]string, 0, len(subjects))
	for _, sub := range subjects {
		out = append(out, "/baseline/"+sub.Port)
	}
	return out, nil
}

// quiet is run.Local with nothing to say: no dependents, no cues. A
// settle over it proposes no cohort, which keeps these tests about the
// operation's SEQUENCE rather than about dependents.Propose.
type quiet struct{}

func (quiet) Dependents(context.Context, string) ([]portindex.Dependent, []portindex.Unread, error) {
	return nil, nil, nil
}

func (quiet) Instructions(context.Context, string, string) ([]dependents.Instruction, error) {
	return nil, nil
}

// has and hasNone are the Verifier seam: a function, because a machine
// with no tart is not an error.
func has(p verify.Verifier) func(context.Context) (verify.Verifier, error) {
	return func(context.Context) (verify.Verifier, error) { return p, nil }
}

func hasNone(context.Context) (verify.Verifier, error) { return nil, verify.ErrNoProvider }

// roomy is a Fake that also reports its free room — the eighth optional
// interface, which app.ask reads for the pass and status reports.
type roomy struct {
	*verifytest.Fake
	room verify.Vacancy
	err  error
}

func (r *roomy) Vacancy(context.Context) (verify.Vacancy, error) { return r.room, r.err }

// streaming is a Fake that also hands its live log over — the ninth
// optional interface, which --trace watches a build through.
type streaming struct {
	*verifytest.Fake
	log string
}

func (s *streaming) Stream(_ context.Context, _ verify.Job, w io.Writer) error {
	_, err := io.WriteString(w, s.log)
	return err
}

// stoppable is a Fake that can end its work WITHOUT destroying the
// environment — verify.Stopper, the tenth optional interface, which a
// --timeout reap needs and which Release cannot stand in for: Release
// takes the guest with it, and the guest is the thing being kept.
type stoppable struct {
	*verifytest.Fake
	stopped []string
}

func (s *stoppable) Stop(_ context.Context, job verify.Job) error {
	s.stopped = append(s.stopped, job.ID)
	return nil
}

// recorder is a progress.Sink that keeps what it was told, so a test can
// assert that a refusal a person needs to see was actually said.
type recorder struct{ lines []string }

func (r *recorder) Stage(string, string)              {}
func (r *recorder) Say(_ progress.Level, text string) { r.lines = append(r.lines, text) }
func (r *recorder) Stream(io.Reader)                  {}
func (r *recorder) said(want string) bool {
	for _, l := range r.lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// sequoia is a concrete release, because an attempt's slot is a (change,
// platform) pair and the zero release would make every fixture share one.
var sequoia = platform.Releases[0]

// enqueued mints a change through the Change operation with a provider
// present, so the record, the branch, the attempt and the lease all come
// from the roads that write them rather than from a literal.
func enqueued(t *testing.T, repo *git.Repo, st *statestore.Store, prov verify.Verifier, port, version, slug string) Result {
	t.Helper()
	op := Change{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Me: me(), Now: now,
	}
	res, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, port, version),
		Delivery: Enqueue, Platform: sequoia, Slug: slug,
	})
	require.NoError(t, err)
	return res
}

// ---------------------------------------------------------------- Status

// STATUS AT ITS THREE DEPTHS, which are three different claims about
// what a reader is holding and not three verbosities.
//
// --no-update is ONE READ: it settles nothing, polls nothing, asks no
// forge, and says so on the result rather than leaving a reader to guess
// from an empty Settled — which is also what a default status that found
// nothing to settle looks like (rule 7).
func TestStatusNoUpdateReadsAndJudgesNothing(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	res := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, res.Did, "the fixture is a running build")
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}

	s := Status{Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(fake), Me: me(), Residency: Residency{State: NoDispatcher}, Now: now}
	out, err := s.Run(t.Context(), StatusRequest{NoUpdate: true})
	require.NoError(t, err)

	assert.True(t, out.NoUpdate, "the depth rides on the value, because nothing downstream can recover it")
	assert.Empty(t, out.Settled)
	assert.Empty(t, out.Obligations, "a pure read asks the provider nothing")
	assert.Empty(t, fake.Released, "and destroys nothing")

	stored, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, stored.Attempts[res.Attempt].Active(), "the attempt was not judged")
}

// WITH NO DISPATCHER, STATUS IS THE JUDGE: it settles what this checkout
// is running, which is what releases the idle 33 GB guest. One job, one
// judge — and here nobody else is holding the chair.
func TestStatusSettlesWhenNoDispatcherIsResident(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	res := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}
	fake.Logs = map[string]string{"fake-1": "--->  Building jq\n"}

	s := Status{Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(fake), Me: me(), Residency: Residency{State: NoDispatcher}, Now: now}
	out, err := s.Run(t.Context(), StatusRequest{Forge: 0})
	require.NoError(t, err)

	assert.Equal(t, []string{res.Attempt}, out.Settled)
	assert.Equal(t, []string{"fake-1"}, fake.Released, "the verdict frees the environment")

	stored, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, stored.Attempts[res.Attempt].Settled())
	assert.Equal(t, record.Passed, stored.Attempts[res.Attempt].Runs["jq"].State)
}

// A RESIDENT DISPATCHER OWNS THE CHAIR. Status still reports — the
// obligations, the vacancy, the record — and judges nothing, because two
// judges over one job is the defect residency exists to prevent.
func TestStatusUnderAResidentDispatcherReportsWithoutJudging(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	res := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}
	prov := &roomy{Fake: fake, room: verify.Vacancy{Known: true, Free: 1, Limit: 2, AsOf: clock}}

	s := Status{Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(prov), Me: me(),
		Residency: Residency{State: DispatcherResident, Holder: record.OwnerID{PID: 999}}, Now: now}
	out, err := s.Run(t.Context(), StatusRequest{})
	require.NoError(t, err)

	assert.Empty(t, out.Settled, "the scheduler settles it, not this process")
	assert.Empty(t, fake.Released)
	assert.Equal(t, verify.Vacancy{Known: true, Free: 1, Limit: 2, AsOf: clock}, out.Vacancy,
		"the machine's free room is reported by the provider that can answer it")

	stored, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, stored.Attempts[res.Attempt].Active())
}

// A PROVIDER THAT CANNOT REPORT ITS ROOM LEAVES THE ANSWER UNKNOWN, and
// unknown is a fact the report shows rather than a zero that reads as a
// full machine.
func TestStatusReportsUnknownRoomForAProviderThatCannotAnswer(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")

	s := Status{Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(fake), Me: me(), Residency: Residency{State: DispatcherResident}, Now: now}
	out, err := s.Run(t.Context(), StatusRequest{})
	require.NoError(t, err)
	assert.False(t, out.Vacancy.Known)
	assert.False(t, out.Vacancy.Admits(1), "an unknown vacancy admits nothing")
}

// ---------------------------------------------------------------- Cancel

// CANCEL IS TWO CALLS: run.Finish with Interrupt Canceled for each
// Active attempt on the tip, and lease.Release for each SETTLED
// attempt's kept environment. Both lifecycles, in the one road a person
// types, and the verdict a settled attempt already earned stands.
func TestCancelStopsTheLiveBuildAndFreesItsEnvironment(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	res := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: has(fake),
		Local: quiet{}, Me: me(), Now: now}
	out, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)

	assert.Equal(t, []string{res.Attempt}, out.Stopped)
	assert.Equal(t, []string{"fake-1"}, fake.Released, "stopping a job IS releasing its environment")

	stored, err := st.Read(t.Context())
	require.NoError(t, err)
	a := stored.Attempts[res.Attempt]
	assert.True(t, a.Settled())
	assert.Equal(t, record.Canceled, verdictOf(a), "a cancellation ends without concluding")
}

// A KEPT ENVIRONMENT IS HANDED BACK AND THE VERDICT STANDS. This is the
// second of Cancel's two lifecycles, and it is a different population
// from the first: the attempt is already settled, and what cancel frees
// is the guest a --keep-env debug session left holding a slot.
func TestCancelReleasesAKeptEnvironmentWithoutTouchingItsVerdict(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	res := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	// The build fails, which is the disposition that KEEPS the guest: what
	// a failed verification hands back is the environment it failed in.
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "w-1"}}
	fake.Logs = map[string]string{"fake-1": "Error: jq did not build\n"}

	s := Status{Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(fake), Me: me(), Residency: Residency{State: NoDispatcher}, Now: now}
	_, err := s.Run(t.Context(), StatusRequest{})
	require.NoError(t, err)
	require.Empty(t, fake.Released, "a failure keeps the environment it failed in")

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: has(fake),
		Local: quiet{}, Me: me(), Now: now}
	out, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)

	assert.Empty(t, out.Stopped, "nothing was running")
	require.Len(t, out.Released, 1, "the kept guest is handed back")
	assert.Equal(t, []string{"fake-1"}, fake.Released)

	stored, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, record.Failed, verdictOf(stored.Attempts[res.Attempt]),
		"the verdict it earned is not rewritten by handing the slot back")
}

// NOTHING HELD NEEDS NO PROVIDER, which is the difference between a
// `cancel` that works on a laptop with no tart and one that refuses.
func TestCancelWithNoProviderSucceedsWhenNothingIsHeld(t *testing.T) {
	repo, st := fixture(t)
	op := changeOp(repo, st)
	_, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: hasNone,
		Local: quiet{}, Me: me(), Now: now}
	out, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Empty(t, out.Stopped)
	assert.Empty(t, out.Released)
}

// A MOVED REF STOPS NOTHING. The person moved the branch themselves, and
// they are asked which of the two they meant before an environment is
// destroyed.
func TestCancelRefusesARefAHandMoved(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	moved := gittest.Commit(t, repo, "moved", "dockhand/jq-1.8", "sysutils/jq/Portfile", "version 1.9\n", "by hand")
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefUpdate{
		{Ref: change.BranchRef("dockhand/jq-1.8"), New: moved, Old: refOf(t, repo, "dockhand/jq-1.8")},
	}))

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: has(fake),
		Local: quiet{}, Me: me(), Now: now}
	_, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.Error(t, err)
	assert.True(t, isTipDisagrees(err), "45, and nothing was stopped")
	assert.Empty(t, fake.Released)
}

func refOf(t *testing.T, repo *git.Repo, branch string) string {
	t.Helper()
	sha, err := repo.RevParse(t.Context(), branch)
	require.NoError(t, err)
	return sha
}

// ---------------------------------------------------------------- Verify

// THE ADOPT ROAD: a dockhand/ branch with no record at all. `verify`
// writes the record and the branch's assert line in ONE Amend, enqueues
// an attempt over the tip it just read, and the Ref it returns is the
// WITNESS that the batch landed — resolved after the commit, never
// carried across it.
func TestVerifyAdoptsABranchWithNoRecordAndAnnouncesIt(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	tip := gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia}}

	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}
	res, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia}})
	require.NoError(t, err)

	assert.True(t, res.Adopted, "a branch dockhand had not tracked is adopted, and the road says so")
	assert.Equal(t, "dockhand/jq-1.9", res.Change.Branch())
	assert.Equal(t, tip, res.Change.Tip(), "the Ref is resolved after the Amend, so it witnesses the batch")
	require.Len(t, res.Attempts, 1)
	assert.Equal(t, Started, res.Attempts[0].Did)
	assert.Equal(t, 0, res.Exit())

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	c := s.Changes[string(res.Change.ID())]
	assert.Equal(t, record.MintedAdopted, c.MintedVia, "the record says where it came from")
	assert.Equal(t, tip, c.Tip)
	require.Len(t, fake.Submitted, 1, "the adoption enqueued and started one attempt")
}

// A HOST WITH NO PROVIDER REFUSES AND ENQUEUES NOTHING. `verify` is the
// one road with no branch to leave behind as its excuse, so the refusal
// comes before any record is written.
func TestVerifyRefusesAHostWithNoProviderAndAdoptsNothing(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")

	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: hasNone, Me: me(), Now: now}
	_, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia}})
	require.ErrorIs(t, err, verify.ErrNoProvider)

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, s.Changes, "nothing was adopted")
}

// --trace ON A PROVIDER THAT CANNOT STREAM SAYS SO. It used to discard
// run.Follow's ErrUnsupported into a goroutine nobody read, which
// printed nothing and explained nothing — indistinguishable, to the
// person who typed the flag, from a build that produced no output.
func TestVerifyTraceSaysSoWhenTheProviderCannotStream(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia},
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}}
	rec := &recorder{}
	var out bytes.Buffer
	wait := time.Second

	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now, Progress: rec, Out: &out}
	_, err := v.Run(t.Context(), VerifyRequest{
		Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia},
		Trace: true, Wait: &wait, Residency: Residency{State: NoDispatcher},
	})
	require.NoError(t, err)
	assert.True(t, rec.said("--trace"), "the refusal a person needs to see was said: %v", rec.lines)
	assert.Empty(t, out.String(), "and nothing was written to the trace stream")
}

// --trace ON A PROVIDER THAT CAN STREAM WATCHES THE BUILD, which the
// ruled surface calls the only way to watch a build happen. The stream
// judges nothing: the verdict still arrives through the record.
func TestVerifyTraceStreamsTheProvidersBytes(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia},
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}}
	prov := &streaming{Fake: fake, log: "--->  Building jq\n"}
	rec := &recorder{}
	traced := &syncBuffer{}
	wait := 5 * time.Second

	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Me: me(), Now: now, Progress: rec, Out: traced}
	res, err := v.Run(t.Context(), VerifyRequest{
		Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia},
		Trace: true, Wait: &wait, Residency: Residency{State: NoDispatcher},
	})
	require.NoError(t, err)
	require.Len(t, res.Attempts, 1)
	assert.Equal(t, Stood, res.Attempts[0].Did, "the verdict came from the record, not from the stream")
	assert.Equal(t, record.Passed, res.Attempts[0].Verdict)
	assert.Eventually(t, func() bool { return traced.String() == "--->  Building jq\n" },
		5*time.Second, 10*time.Millisecond, "the provider's bytes reached the trace stream")
}

// syncBuffer is the --trace sink, written by run.Follow's goroutine and
// read by the test, so the race detector has one mutex to see rather
// than a data race to report.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ----------------------------------------------------------------- Cycle

// order is a provider that records the SEQUENCE of what it was asked,
// which is the only way to assert a stage order from outside: the pass's
// stages are not a column to read down, they are edges, and each edge is
// an argument about what a crash leaves or what a slot costs.
type order struct {
	*verifytest.Fake
	calls []string
}

func (o *order) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	o.calls = append(o.calls, "poll "+job.ID)
	return o.Fake.Poll(ctx, job)
}

func (o *order) Release(ctx context.Context, job verify.Job) error {
	o.calls = append(o.calls, "release "+job.ID)
	return o.Fake.Release(ctx, job)
}

func (o *order) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	job, err := o.Fake.Submit(ctx, req)
	if err == nil {
		o.calls = append(o.calls, "submit "+job.ID)
	}
	return job, err
}

func cycleOp(repo *git.Repo, st *statestore.Store, prov verify.Verifier) Cycle {
	return Cycle{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Grants: Grants{Invoker: record.Human}, Me: me(), PassID: "pass-1", Now: now,
	}
}

// SETTLE COMES BEFORE DRAIN, AND THE EDGE IS THE WHOLE ARGUMENT: a
// settle frees a slot and produces the verdicts the later stages read,
// so a pass that drained first would start nothing on a machine its own
// finished builds were still holding.
//
// Asserted over the PROVIDER'S CALL SEQUENCE rather than over the store,
// because the pass's stages are edges and not a column: what has to be
// true is that the running build was polled and its environment handed
// back before the queued one was submitted.
func TestCycleSettlesBeforeItDrains(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	prov := &order{Fake: fake}

	// A running build on one change, and a queued attempt on another that
	// met a full machine when it was minted.
	running := enqueued(t, repo, st, prov, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, running.Did)
	fake.SubmitErr = &verify.NoVacancyError{Busy: 2, Limit: 2}
	waiting := enqueued(t, repo, st, prov, "oniguruma", "6.9", "oniguruma-6.9")
	require.Equal(t, Queued, waiting.Did, "the machine was full when this one was minted")

	// The room frees the moment the first build's verdict is in.
	fake.SubmitErr = nil
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}
	fake.Logs = map[string]string{"fake-1": "--->  Building jq\n"}
	prov.calls = nil // what the fixture asked for is not what the pass asked for

	p, err := cycleOp(repo, st, prov).Run(t.Context(), CycleRequest{
		Discharge: true, DischargeAfter: time.Hour, Retirement: ReportOnly, Forge: 1,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"poll fake-1", "release fake-1", "submit fake-2"}, prov.calls,
		"the verdict and the freed slot come first; the queue is drained into what they left")
	assert.Equal(t, Stood, p.Changes[recordID(running)].Did)
	assert.Equal(t, record.Passed, p.Changes[recordID(running)].Verdict)
	assert.Equal(t, Started, p.Changes[recordID(waiting)].Did)
	assert.Equal(t, 0, p.Exit(), "a pass that did its work needs nobody")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, s.Attempts[running.Attempt].Settled())
	assert.True(t, s.Attempts[waiting.Attempt].Active())
}

// A REFUSAL IN ONE STAGE DOES NOT ABORT THE PASS. The retire stage meets
// a publication whose forge it cannot ask; that is a Refusal row and a
// person's business, and the drain behind it still runs — because a pass
// that stopped on the first thing it could not do would leave a machine
// idle over a `gh` that has been uninstalled for a week, with the
// absence of retirements as the operator's only signal.
func TestCycleCarriesOnPastARefusalInAnEarlierStage(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{SubmitErr: &verify.NoVacancyError{Busy: 2, Limit: 2}}
	waiting := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	require.Equal(t, Queued, waiting.Did)

	// An open publication on that change. Env is zero here, so the forge
	// cannot be asked at all — which is exactly the fact rule 7 says must
	// not be collapsed into "still open".
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{
			ID: "pub-1", Change: recordID(waiting), By: record.Human, Outcome: record.Open,
		})
		return nil
	}))
	fake.SubmitErr = nil

	p, err := cycleOp(repo, st, fake).Run(t.Context(), CycleRequest{
		Discharge: true, DischargeAfter: time.Hour, Retirement: ReportOnly, Forge: 1,
	})
	require.NoError(t, err, "a refusal is a row, never a stop")

	require.Len(t, p.Refusals, 1)
	assert.Equal(t, recordID(waiting), p.Refusals[0].Change)
	assert.True(t, p.Attention(), "a family nobody argued into the quiet list is loud")
	assert.Equal(t, 84, p.Exit())

	assert.Equal(t, Started, p.Changes[recordID(waiting)].Did, "the drain ran behind the refusal")
	require.Len(t, fake.Submitted, 1, "the drain seated the attempt the full machine had left queued")
}

// THE VACANCY IS REPORTED AND NEVER CONSULTED, and it is asked AFTER the
// drain has already stopped, so it can never become a gate by accident
// of ordering: this machine says it is full, and the pass started
// everything it had anyway.
func TestCycleReportsTheVacancyItNeverGatedOn(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{SubmitErr: &verify.NoVacancyError{Busy: 2, Limit: 2}}
	waiting := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	require.Equal(t, Queued, waiting.Did)
	fake.SubmitErr = nil
	prov := &roomy{Fake: fake, room: verify.Vacancy{Known: true, Free: 0, Limit: 2, AsOf: clock}}

	p, err := cycleOp(repo, st, prov).Run(t.Context(), CycleRequest{
		Discharge: true, DischargeAfter: time.Hour, Retirement: ReportOnly, Forge: 1,
	})
	require.NoError(t, err)

	assert.Equal(t, verify.Vacancy{Known: true, Free: 0, Limit: 2, AsOf: clock}, p.Vacancy)
	assert.False(t, p.Vacancy.Admits(1))
	assert.Equal(t, Started, p.Changes[recordID(waiting)].Did,
		"the sentinel is the authority; the observation is an economy and never a gate")
}

// A PASS ON A MACHINE WITH NO PROVIDER STILL RUNS. It discharges
// nothing, settles nothing and drains nothing — every one of those needs
// a provider — and it still closes the change lifecycle's other deaths
// and reports, which is what makes a tart-less host a place a pass is
// worth running at all.
func TestCycleRunsWithoutAProvider(t *testing.T) {
	repo, st := fixture(t)
	_, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	c := cycleOp(repo, st, nil)
	c.Verifier = hasNone
	p, err := c.Run(t.Context(), CycleRequest{
		Discharge: true, DischargeAfter: time.Hour, Retirement: ReportOnly, Forge: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, p.Refusals)
	assert.Empty(t, p.Owed, "nothing could be asked, so nothing is claimed to be owed")
	assert.False(t, p.Vacancy.Known, "and the machine's room is unknown rather than zero")
	assert.Equal(t, 0, p.Exit())
}

func recordID(r Result) record.ChangeID { return r.Ref.ID() }

// seed makes the state ref exist without writing a record into it: an
// empty Amend, which is what the first write of any lifecycle would
// leave behind. The operations that READ before they write need it,
// since statestore.ErrNoState is a fact about the repository and not an
// empty store.
func seed(t *testing.T, st *statestore.Store) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(*statestore.Txn) error { return nil }))
}

// ---------------------------------------------------------------- Accept

// ACCEPT EXTENDS THE BRANCH THAT ALREADY CARRIES THE CHANGE. Its ticks
// are extend + answer and never begin or mint: ONE Amend moves the
// branch to the cohort commit (the record's CAS and the ref's update
// line in the same batch), marks the proposal Accepted, and enqueues the
// cohort's own verification — and the Ref that comes back is resolved
// AFTER the batch, so it witnesses that the branch actually moved.
func TestAcceptExtendsTheBranchAndAnswersTheProposal(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia}}
	minted := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	before := minted.Ref.Tip()

	// The proposal a settled verification would have written: oniguruma
	// depends on jq's ABI and wants a revision bump beside it.
	propose(t, st, minted.Ref.ID(), record.Candidate{Port: "oniguruma", Portdir: "devel/oniguruma", Proposed: true})

	a := Accept{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now,
		Prepare: func(_ context.Context, _ string, cands []record.Candidate, _ string) (change.Prepared, error) {
			require.Len(t, cands, 1)
			return change.Prepared{
				Portdir:  change.TreePath("devel/oniguruma"),
				Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"}, Portdir: "devel/oniguruma", Intent: "revision"}},
				Files:    []change.File{{Path: "Portfile", Content: []byte("version 6.8\nrevision 1\n")}},
				Intent:   "revision",
				Summary:  "oniguruma: revbump for jq",
			}, nil
		},
	}
	res, err := a.Run(t.Context(), AcceptRequest{Branch: "dockhand/jq-1.8", Platform: sequoia})
	require.NoError(t, err)

	assert.Equal(t, Started, res.Did)
	assert.NotEqual(t, before, res.Ref.Tip(), "the branch carries the cohort commit now")
	assert.Equal(t, "dockhand/jq-1.8", res.Ref.Branch(), "it extends; it never mints a second branch")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	c := s.Changes[string(minted.Ref.ID())]
	assert.Equal(t, record.ChangeExtended, c.State)
	assert.Equal(t, res.Ref.Tip(), c.Tip, "the record's CAS and the ref's update line landed together")
	assert.Equal(t, []string{"jq", "oniguruma"}, portsOf(c.Subjects), "the headline keeps its place")
	require.Len(t, c.Findings, 1)
	assert.Equal(t, record.Accepted, c.Findings[0].Disposition, "the answer is given once, and recorded")

	require.Len(t, fake.Submitted, 2, "the cohort's own verification was enqueued and started")
	assert.Equal(t, []string{"jq", "oniguruma"}, fake.Submitted[1].Ports)
}

// A CHANGE WITH NOTHING TO ACCEPT REFUSES BY NAME AND WRITES NOTHING.
// The proposal is what this road answers, so its absence is exit 10's
// decline and never an empty cohort commit.
func TestAcceptRefusesAChangeCarryingNoProposal(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia}}
	minted := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	before := minted.Ref.Tip()

	a := Accept{Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}
	_, err := a.Run(t.Context(), AcceptRequest{Branch: "dockhand/jq-1.8", Platform: sequoia})
	require.ErrorIs(t, err, change.ErrNoProposal)

	assert.Equal(t, before, refOf(t, repo, "dockhand/jq-1.8"), "the branch did not move")
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, s.Changes[string(minted.Ref.ID())].Tip)
}

// --no-verify EXTENDS AND ENQUEUES NOTHING, which is the same narrowing
// the mint road makes: the branch carries the cohort, and the person
// says when it is verified.
func TestAcceptUnderNoVerifyExtendsWithoutEnqueueing(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia}}
	minted := enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")
	propose(t, st, minted.Ref.ID(), record.Candidate{Port: "oniguruma", Portdir: "devel/oniguruma", Proposed: true})

	a := Accept{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now,
		Prepare: func(context.Context, string, []record.Candidate, string) (change.Prepared, error) {
			return change.Prepared{
				Portdir:  change.TreePath("devel/oniguruma"),
				Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"}, Portdir: "devel/oniguruma", Intent: "revision"}},
				Files:    []change.File{{Path: "Portfile", Content: []byte("version 6.8\nrevision 1\n")}},
				Intent:   "revision", Summary: "oniguruma: revbump for jq",
			}, nil
		},
	}
	res, err := a.Run(t.Context(), AcceptRequest{Branch: "dockhand/jq-1.8", Platform: sequoia, NoVerify: true})
	require.NoError(t, err)

	assert.Equal(t, Minted, res.Did, "extended, and nothing was asked of a provider")
	assert.Empty(t, res.Attempt)
	require.Len(t, fake.Submitted, 1, "only the original bump's own submit")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, record.ChangeExtended, s.Changes[string(minted.Ref.ID())].State)
}

// propose writes the cohort finding a settled verification's propose
// step would have written, so the fixture is the record Accept reads and
// not a shape invented beside it.
func propose(t *testing.T, st *statestore.Store, id record.ChangeID, cands ...record.Candidate) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return change.ProposeIn(tx, id, record.Finding{
			Kind:       record.KindABIDependents,
			Candidates: cands,
			Criterion:  "the install name moved between the two builds",
		}, clock)
	}))
}

func portsOf(subjects []record.Subject) []string {
	out := make([]string, 0, len(subjects))
	for _, s := range subjects {
		out = append(out, s.Port)
	}
	return out
}

// CANCEL REVOKES QUEUED INTENT, and for a long time it did not.
//
// The verb handled Active attempts and settled ones holding an
// environment, and had no case at all for QUEUED work — so it reported
// success and left an attempt the next pass's drain started, booting a
// guest for a change a person had just cancelled. WithdrawIn was already
// the durable transition; cancellation was the one road that did not
// compose it.
func TestCancelWithdrawsQueuedWorkSoADrainCannotStartIt(t *testing.T) {
	repo, st := fixture(t)
	// A full machine: Start refuses with ErrNoVacancy and leaves the
	// attempt QUEUED, which is the population this test is about and the
	// one cancel had no case for.
	fake := &verifytest.Fake{SubmitErr: verify.ErrNoVacancy}
	op := Change{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now,
	}
	res, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"),
		Delivery: Enqueue, Platform: sequoia, Slug: "jq-1.8",
	})
	require.NoError(t, err)
	fake.SubmitErr = nil

	before, err := st.Read(t.Context())
	require.NoError(t, err)
	require.True(t, before.Attempts[res.Attempt].Queued(), "the fixture is a queued attempt")

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: has(fake),
		Local: quiet{}, Me: me(), Now: now}
	out, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)

	assert.Equal(t, []string{res.Attempt}, out.Withdrawn,
		"reported as withdrawn and not as stopped: nothing was running to stop")
	assert.Empty(t, out.Stopped)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	a := after.Attempts[res.Attempt]
	assert.False(t, a.Queued(), "no drain can start it now")
	assert.True(t, a.Settled())
	assert.Equal(t, record.Canceled, verdictOf(a))

	queued, _ := run.Pending(after, now())
	assert.Empty(t, queued, "and the queue no longer offers it")
}

// The no-provider shortcut counted an environment and not the intent, so
// a cancel on a host with no tart reported success over queued work and
// revoked nothing. There is nothing to stop and there IS something to
// revoke, and the revocation needs no provider at all.
func TestCancelWithNoProviderStillWithdrawsQueuedWork(t *testing.T) {
	repo, st := fixture(t)
	// Queued on a machine that had a provider; cancelled from a shell
	// where none resolves, which is the ordinary shape of this.
	op := Change{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(&verifytest.Fake{SubmitErr: verify.ErrNoVacancy}), Me: me(), Now: now,
	}
	res, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"),
		Delivery: Enqueue, Platform: sequoia, Slug: "jq-1.8",
	})
	require.NoError(t, err)
	require.NotEmpty(t, res.Attempt)

	c := Cancel{Repo: repo, Ledger: ledger.Open(repo), State: st, Verifier: hasNone,
		Local: quiet{}, Me: me(), Now: now}
	out, err := c.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, []string{res.Attempt}, out.Withdrawn)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.False(t, after.Attempts[res.Attempt].Queued())
}

// A DRY RUN PERFORMS NOTHING, and this is the probe that used to pass
// the other way.
//
// The flag was a field each stage consulted, and three did not: the
// drain, the settle loop and the note re-export ran unguarded. Given one
// queued attempt, DryRun true and a working provider, a "dry" cycle
// counted one Submit and left an Active attempt — it booted a virtual
// machine. It also polled builds, released environments and closed
// publication rows.
func TestADryRunSubmitsNothingAndSettlesNothing(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	prov := &order{Fake: fake}

	// One running build and one queued attempt: the two populations the
	// unguarded stages acted on.
	running := enqueued(t, repo, st, prov, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, running.Did)
	fake.SubmitErr = &verify.NoVacancyError{Busy: 2, Limit: 2}
	waiting := enqueued(t, repo, st, prov, "oniguruma", "6.9", "oniguruma-6.9")
	require.Equal(t, Queued, waiting.Did)

	fake.SubmitErr = nil
	fake.States = map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}
	fake.Logs = map[string]string{"fake-1": "--->  Building jq\n"}
	// What the FIXTURE asked the provider for is not what the pass asked
	// for, and the assertions below are about the pass.
	prov.calls, fake.Submitted, fake.Released = nil, nil, nil

	p, err := cycleOp(repo, st, prov).Run(t.Context(), CycleRequest{
		DryRun: true, Discharge: true, DischargeAfter: time.Hour, Retirement: Demolish, Forge: 1,
	})
	require.NoError(t, err)

	assert.Empty(t, fake.Submitted, "no build was started")
	assert.Empty(t, fake.Released, "no environment was destroyed")
	assert.NotContains(t, prov.calls, "submit fake-2")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, s.Attempts[running.Attempt].Active(), "the running build was not settled")
	assert.True(t, s.Attempts[waiting.Attempt].Queued(), "the queued attempt was not started")

	// And it SAYS what it would have done, which is the whole point of
	// asking.
	require.NotNil(t, p.Would)
	assert.Equal(t, []string{running.Attempt}, p.Would.Settle)
	assert.Equal(t, []string{waiting.Attempt}, p.Would.Start)
}

// The survey holds no path to a mutator, so the store it read is the
// store it leaves: a dry run writes no state commit at all.
func TestADryRunWritesNoStateCommit(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	enqueued(t, repo, st, fake, "jq", "1.8", "jq-1.8")

	before, err := st.Read(t.Context())
	require.NoError(t, err)

	_, err = cycleOp(repo, st, fake).Run(t.Context(), CycleRequest{
		DryRun: true, Discharge: true, DischargeAfter: time.Hour, Retirement: Demolish,
		Superseded: true, Forge: 1, Compact: &statestore.Retention{Set: true, ClosedFor: 1},
	})
	require.NoError(t, err)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before.At, after.At, "the state ref did not move")
}

// AN ADOPTED ATTEMPT IS NOT HANDED TO A FUNCTION THAT REFUSES IT.
// run.Adoptable draws from three states — Queued, Active and settled
// Passed — and every caller passed its answer straight to run.Start,
// which takes only Queued. So the two cases adoption exists FOR were
// exactly the two that failed the whole verb: a tip somebody had already
// verified, and one still building.
func TestVerifyResumesAnAttemptThatAlreadyPassedRatherThanStartingIt(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia},
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "w-1"}}}
	wait := 5 * time.Second
	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}

	first, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9",
		Platforms: []platform.Release{sequoia}, Wait: &wait, Residency: Residency{State: NoDispatcher}})
	require.NoError(t, err)
	require.Len(t, first.Attempts, 1)
	require.Equal(t, Stood, first.Attempts[0].Did)

	second, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9",
		Platforms: []platform.Release{sequoia}})
	require.NoError(t, err, "adopting a passed attempt is the free road, not an error")
	require.Len(t, second.Attempts, 1)
	assert.Equal(t, first.Attempts[0].Attempt, second.Attempts[0].Attempt, "the same attempt was adopted")
	assert.Equal(t, Stood, second.Attempts[0].Did, "the verdict is earned; there is nothing to start or wait for")
	assert.Equal(t, record.Passed, second.Attempts[0].Verdict)
	assert.Len(t, fake.Submitted, 1, "adoption is what stops the machine paying twice")
}

// AND ONE STILL RUNNING IS JOINED, NOT RESTARTED. The lease it already
// holds is the one the row names.
func TestVerifyResumesAnAttemptThatIsStillRunning(t *testing.T) {
	repo, st := fixture(t)
	seed(t, st)
	gittest.Commit(t, repo, "dockhand/jq-1.9", "HEAD", "sysutils/jq/Portfile", "version 1.9\n", "jq: 1.9")
	fake := &verifytest.Fake{Platforms: []platform.Release{sequoia}} // stays Running
	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}

	first, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia}})
	require.NoError(t, err)
	require.Equal(t, Started, first.Attempts[0].Did)

	second, err := v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.9", Platforms: []platform.Release{sequoia}})
	require.NoError(t, err)
	require.Len(t, second.Attempts, 1)
	assert.Equal(t, first.Attempts[0].Attempt, second.Attempts[0].Attempt)
	assert.Equal(t, Started, second.Attempts[0].Did)
	assert.Equal(t, first.Attempts[0].Lease, second.Attempts[0].Lease, "the running attempt's own lease, not a new one")
	assert.Len(t, fake.Submitted, 1, "one guest, asked for once")
}

// A RE-DERIVATION MUST NOT BE VERIFIED AGAINST THE ARCHIVE IT REPLACES.
// `refresh-checksums` leaves the version where it was, so the published
// binary archive is the one built from the bytes the change has just
// corrected — installing it would prove nothing and would say "passed".
//
// The whole road for this existed and none of it was ever fed: the spec
// hashes FromSource, the frozen roster carries it, run.Plan intersects
// it, verify.Request declares it and tart reads it per member to pass
// `-s`. Nothing in the tree produced one.
func TestARederivationIsBuiltFromSourceAndAVersionBumpIsNot(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	c := Change{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}

	p := preparedBump(t, repo, "jq", "1.9")
	p.Subjects[0].Intent, p.Intent = IntentRefresh, IntentRefresh
	_, err := c.Run(t.Context(), ChangeRequest{Prepared: p, Delivery: Enqueue, Platform: sequoia, Slug: "jq-refresh"})
	require.NoError(t, err)
	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].FromSource,
		"the archive that matches a re-derivation predates the change")

	// And a version bump does NOT ask for it: the new version yields an
	// archive name that does not exist yet, so MacPorts builds from
	// source on its own and forcing -s buys nothing.
	fake.Submitted = nil
	q := preparedBump(t, repo, "oniguruma", "6.9.10")
	_, err = c.Run(t.Context(), ChangeRequest{Prepared: q, Delivery: Enqueue, Platform: sequoia, Slug: "oniguruma-6.9.10"})
	require.NoError(t, err)
	require.Len(t, fake.Submitted, 1)
	assert.Empty(t, fake.Submitted[0].FromSource)
}

// A COHORT VERIFIED THROUGH `verify` SEATS WHAT THE RECORD SAYS, and a
// member the proposal marked Solo is BUMPED AND NOT BUILT.
//
// rosterOf seats every subject and withholds nothing. Its own doc says
// why that is right — "for a freshly minted change there is no Accepted
// cohort finding and no attempt with runs, so Roster's answer is exactly
// this" — and a change that has ACCEPTED A COHORT is not freshly minted.
// `accept` used run.Roster and `verify` did not, so a cohort verified
// through this road handed the guest both mkvtoolnix and
// mkvtoolnix-devel: the collision Solo exists to prevent.
func TestVerifySeatsACohortFromTheRecordAndWithholdsItsSoloMember(t *testing.T) {
	repo, st := fixture(t)
	fake := &verifytest.Fake{}
	// Minted without a build, so nothing holds a lease before the verify
	// under test.
	minted, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	// The shape `accept` leaves: a second subject, and an accepted cohort
	// whose member conflicts with the headline the same cohort builds.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		c := tx.State().Changes[string(minted.Ref.ID())]
		c.Subjects = append(c.Subjects,
			record.Subject{Port: "oniguruma", Names: []string{"oniguruma"}, Portdir: "devel/oniguruma"})
		c.Findings = []record.Finding{{
			Kind: record.KindABIDependents, Disposition: record.Accepted,
			Candidates: []record.Candidate{
				{Port: "oniguruma", Portdir: "devel/oniguruma", Proposed: true, Solo: true,
					Over: "jq", Reason: "conflicts with jq, which this cohort builds"},
			},
		}}
		tx.PutChange(c)
		return nil
	}))

	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(fake), Me: me(), Now: now}
	_, err = v.Run(t.Context(), VerifyRequest{Target: "dockhand/jq-1.8", Platforms: []platform.Release{sequoia}})
	require.NoError(t, err)

	require.Len(t, fake.Submitted, 1)
	assert.NotContains(t, fake.Submitted[0].Ports, "oniguruma",
		"a Solo member is bumped and NOT built; the guest must not hold it beside the member it conflicts with")
	assert.Contains(t, fake.Submitted[0].Ports, "jq")
}

// counter wraps a provider and records how often a wait loop asks it
// anything. Poll is the expensive question — for the tart provider it is
// a `tart list` plus an exec into the guest agent — so counting it
// counts the load a `--timeout` puts on the machine it is waiting for.
type counter struct {
	*verifytest.Fake
	polls atomic.Int64
}

func (c *counter) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	c.polls.Add(1)
	return c.Fake.Poll(ctx, job)
}

// A WAIT MUST NOT SPIN ON THE GUEST IT IS WAITING FOR.
//
// The watching role has always been paced: AwaitFor polls the record on
// a timer. The JUDGING role was not — run.Finish returns as soon as it
// has looked and does not sleep, so with no dispatcher the loop asked
// the provider as fast as the host could fork the processes, for the
// whole of a build that can run for hours.
//
// Measured in the field: a cohort's guest trapped inside Apple's
// Virtualization framework on an XPC event-handler thread, three times,
// four to seven minutes in — and `tart exec` reaches the guest agent
// over that same channel. Whether the hammering was a cause is not
// settled here. That it was waste is.
//
// The bound is what makes the claim testable: a wait shorter than one
// interval asks once. Unpaced, this same window was hundreds.
func TestAWaitWithNoDispatcherDoesNotSpinOnTheGuest(t *testing.T) {
	repo, st := fixture(t)
	c := &counter{Fake: &verifytest.Fake{Platforms: []platform.Release{sequoia}}}
	res := enqueued(t, repo, st, c, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, res.Did, "the fixture is a running build")
	require.Less(t, time.Second, judgeEvery, "this test's window must be inside one interval")

	before := c.polls.Load()
	wait := 900 * time.Millisecond
	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(c), Me: me(), Now: now}
	_, err := v.Run(t.Context(), VerifyRequest{
		Target: "dockhand/jq-1.8", Platforms: []platform.Release{sequoia},
		Wait: &wait, Residency: Residency{State: NoDispatcher}})
	require.NoError(t, err)

	asked := c.polls.Load() - before
	assert.LessOrEqual(t, asked, int64(2),
		"a wait shorter than one interval asks the guest once, not continuously")
}

// A --timeout STOPS THE BUILD AND KEEPS ITS ENVIRONMENT. Both halves are
// the claim, and neither was true of the flag this replaced: expiry used
// to DETACH — the build ran on to completion, the caller simply stopped
// looking — which is a deadline in name only, and which is not what a
// person typing the word "timeout" is asking for.
//
// The environment is kept because of what the person stopped. They did
// not decide the work was unwanted; they decided they would not wait for
// it, which leaves them with an unanswered question about how far it got
// and `dockhand shell` as the place to ask. It is retained on
// lease.KeepFor like any other kept guest, so it costs a slot for a day
// rather than forever.
func TestATimeoutStopsTheBuildAndKeepsItsEnvironment(t *testing.T) {
	repo, st := fixture(t)
	// A provider that never settles: Poll answers Running for a job it
	// has no script for, which is the build that outlives its deadline.
	prov := &stoppable{Fake: &verifytest.Fake{}}
	res := enqueued(t, repo, st, prov, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, res.Did, "the fixture is a running build")

	wait := 50 * time.Millisecond
	said := &recorder{}
	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Me: me(), Now: now, Progress: said}
	_, err := v.Run(t.Context(), VerifyRequest{
		Target: "dockhand/jq-1.8", Platforms: []platform.Release{sequoia},
		Wait: &wait, Residency: Residency{State: NoDispatcher}})
	require.NoError(t, err)

	assert.NotEmpty(t, prov.stopped, "the deadline passed and the build was left running")

	// A BLOCKING INVOCATION SAYS WHAT IT IS BLOCKING ON. Everything else
	// in this tool is built so that walking away is free, so the one road
	// that asks a person to stay owes them the reason, the scale and the
	// way out — a terminal silent for forty minutes is indistinguishable
	// from one that has hung, and a person who cannot tell reaches for
	// the thing that loses their work.
	assert.True(t, said.said("waiting on the build"), "the wait names what it is waiting for")
	assert.True(t, said.said("minutes to hours"), "and how long that is likely to be")
	assert.True(t, said.said("Ctrl-C is safe"), "and that leaving costs nothing")

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, after.Attempts, 1)
	for _, a := range after.Attempts {
		require.Equal(t, record.Finished, a.Phase, "a reap settles the attempt")
		for port, r := range a.Runs {
			assert.Equal(t, record.Canceled, r.State, "%s: a reap concluded nothing", port)
			assert.Contains(t, r.Detail, "--timeout",
				"the record says why it stopped, in the words the person typed")
		}
		lse, held := after.Leases[a.Lease]
		require.True(t, held, "the environment is kept, so its lease stands")
		assert.NotNil(t, lse.Retain,
			"and it is kept ON A DEADLINE — lease.KeepFor — rather than until somebody remembers")
	}
}

// A PROVIDER THAT CANNOT STOP IS SAID SO AND NOT PAPERED OVER (rule 7).
// The attempt still settles, because the caller asked to stop waiting
// and that much is always deliverable — but the build really is still
// running, and a record that claimed otherwise would be contradicted by
// the person's own `dockhand log` a minute later.
func TestAReapSaysSoWhenTheBuildCouldNotBeStopped(t *testing.T) {
	repo, st := fixture(t)
	prov := &verifytest.Fake{} // implements no verify.Stopper
	res := enqueued(t, repo, st, prov, "jq", "1.8", "jq-1.8")
	require.Equal(t, Started, res.Did)

	wait := 50 * time.Millisecond
	v := Verify{Repo: repo, Ledger: ledger.Open(repo), State: st, Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Me: me(), Now: now}
	_, err := v.Run(t.Context(), VerifyRequest{
		Target: "dockhand/jq-1.8", Platforms: []platform.Release{sequoia},
		Wait: &wait, Residency: Residency{State: NoDispatcher}})
	require.NoError(t, err)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	for _, a := range after.Attempts {
		for _, r := range a.Runs {
			assert.Contains(t, r.Detail, "still running",
				"a reap that could not reap says so rather than reporting a quiet death")
		}
	}
}
