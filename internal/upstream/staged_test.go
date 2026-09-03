package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
	"github.com/herbygillot/dockhand/internal/upstream/forge"
)

// The staging is judgment — which witness is worth paying for, and in
// what order — so it is tested the way the rest of dockhand's judgment
// is: with the world scripted. A fake git on PATH, a scripted gh, a
// scripted livecheck, a Portfile on disk and an oracle that answers
// the two questions a port is asked. Nothing here touches a network,
// which is the only way this can be tested at all: a test that proved
// the pacer paces by making real requests would be making the requests
// the pacer exists to prevent.

// scriptedOracle answers the questions a Handle puts to a port. Only
// Values and Options are ever asked here; the rest are the interface's
// and fail loudly rather than returning a zero value that would read
// as a real answer.
type scriptedOracle struct {
	values info.Values
	opts   map[string]string
}

func (o scriptedOracle) Values(context.Context, string, string, info.VariantSet) (info.Values, error) {
	return o.values, nil
}

func (o scriptedOracle) Options(_ context.Context, _, _ string, _ info.VariantSet, names ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range names {
		if v, ok := o.opts[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

func (scriptedOracle) Snapshot(context.Context, string, info.VariantSet) (info.Snapshot, error) {
	return info.Snapshot{}, errors.New("scriptedOracle: Snapshot not scripted")
}

func (scriptedOracle) Subports(context.Context, string) ([]string, error) {
	return nil, errors.New("scriptedOracle: Subports not scripted")
}

func (scriptedOracle) FetchInfo(context.Context, string, string, info.VariantSet, bool) (eval.FetchInfo, error) {
	return eval.FetchInfo{}, errors.New("scriptedOracle: FetchInfo not scripted")
}

var _ port.Oracle = scriptedOracle{}

// scriptedLivecheck is the expensive witness, counted. Counting is the
// point of most of these tests: the staging's whole claim is that this
// does not run.
type scriptedLivecheck struct {
	mu     sync.Mutex
	calls  int
	result portfetch.LivecheckResult
	err    error
}

func (l *scriptedLivecheck) Livecheck(context.Context, string, string) (portfetch.LivecheckResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return l.result, l.err
}

func (l *scriptedLivecheck) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// fakeGit puts a git on PATH that prints canned ls-remote output and
// counts how often it was run. A real subprocess, so the parsing, the
// environment and the error handling under test are the real ones.
type fakeGit struct {
	dir string
}

func newFakeGit(t *testing.T, lines string) *fakeGit {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\necho run >> %q\ncat <<'REFS'\n%s\nREFS\n", counter, lines)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755)) //nolint:gosec // a test fixture that must be executable
	return &fakeGit{dir: dir}
}

func (g *fakeGit) finder() *tool.Finder {
	return tool.NewFinder(func(name string) (string, error) {
		if name == "git" {
			return filepath.Join(g.dir, "git"), nil
		}
		return exec.LookPath(name)
	})
}

func (g *fakeGit) calls() int {
	b, err := os.ReadFile(filepath.Join(g.dir, "calls"))
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "run")
}

// portAt writes a Portfile and returns a handle on it.
func portAt(t *testing.T, body string, vals info.Values, opts map[string]string) port.Handle {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(body), 0o600))
	return port.New(tree.Target{Portdir: dir}, scriptedOracle{values: vals, opts: opts})
}

// githubPort is the common fixture: a github.setup carrier at version,
// with a v tag prefix.
func githubPort(t *testing.T, version string) port.Handle {
	t.Helper()
	body := "PortSystem 1.0\nPortGroup github 1.0\n\ngithub.setup dockhand widget " + version + " v\n"
	return portAt(t, body,
		info.Values{Name: "widget", Version: version},
		map[string]string{
			"github.author":     "dockhand",
			"github.project":    "widget",
			"github.tag_prefix": "v",
		})
}

func observerWith(t *testing.T, git *fakeGit, lc Livechecker) *Observer {
	t.Helper()
	return &Observer{
		Tools:   git.finder(),
		Fetcher: lc,
		Pacer:   courtesy.NewPacer(courtesy.Policy{Ceiling: 2}, nil),
		Cache:   courtesy.NewCache(t.TempDir(), time.Hour, nil),
		Agent:   courtesy.UserAgent("test"),
	}
}

const refsCurrent = "aaaa111\trefs/tags/v1.2.0\nbbbb222\trefs/tags/v1.3.0"

// Stage one is the whole saving: a port whose forge holds nothing
// newer is answered by one ls-remote, and the expensive witness is
// never run.
func TestStageOneAnswersWithoutRunningLivecheck(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{}
	o := observerWith(t, git, lc)

	row := o.Observe(context.Background(), githubPort(t, "1.3.0"))

	assert.Equal(t, OutcomeCurrent, row.Outcome)
	assert.Zero(t, lc.count(), "the expensive witness ran for a port stage one had already answered")
	assert.Equal(t, 1, git.calls())
	assert.Equal(t, []Witnessed{{Witness: WitnessLsRemote, Source: "fetched"}}, row.Stages)
	assert.Equal(t, "bbbb222", row.Sha, "the sha column names the object the newest tag points at")
	assert.Equal(t, exitcode.OK, row.Code)
}

// The trap ForgeCurrent exists for, asserted directly because it is
// the one mistake that would have shipped: an observation with no
// livecheck reading in it, handed to Judge, comes back LivecheckRot —
// and a staged sweep would have charged thousands of healthy ports
// with a broken regex it never executed.
func TestStageOneNeverChargesAnUnrunLivecheckWithRot(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{}
	row := observerWith(t, git, lc).Observe(context.Background(), githubPort(t, "1.3.0"))

	assert.Equal(t, ForgeCurrent.String(), row.Verdict)
	assert.NotEqual(t, LivecheckRot.String(), row.Verdict)
	assert.NotContains(t, row.Detail, "rot")

	// The shape of the mistake, made on purpose, so that the reason
	// for the verdict is on the record rather than in a comment.
	naive := Judge(Observation{Current: "1.3.0", ForgeVersions: []string{"1.2.0", "1.3.0"}})
	assert.Equal(t, LivecheckRot, naive.Verdict,
		"Judge cannot tell a livecheck that matched nothing from one that was never asked")
}

// A candidate pays for the second witness, and the second witness is
// what resolves the version. This is the other half of the staging:
// the ports that need the expensive question get it.
func TestCandidatePaysForTheSecondWitness(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, Version: "1.3.0"}}
	o := observerWith(t, git, lc)

	row := o.Observe(context.Background(), githubPort(t, "1.2.0"))

	assert.Equal(t, OutcomeOutdated, row.Outcome)
	assert.Equal(t, "1.2.0", row.Current)
	assert.Equal(t, "1.3.0", row.Latest)
	assert.Equal(t, "bbbb222", row.Sha)
	assert.Equal(t, Agreement.String(), row.Verdict)
	assert.Equal(t, 1, lc.count())
	assert.Equal(t, []Witnessed{
		{Witness: WitnessLsRemote, Source: "fetched"},
		{Witness: WitnessLivecheck, Source: "fetched"},
	}, row.Stages)
	assert.Equal(t, exitcode.OK, row.Code, "being outdated is the report's subject, not an error")
}

// A stable port is not made a candidate by a newer release candidate:
// that would send a maintainer to look at a beta, and it would pay for
// the expensive witness to do it. The detail says what happened, so
// "current" beside a repository the reader knows has moved is not a
// mystery.
func TestPrereleaseAloneIsNotACandidate(t *testing.T) {
	git := newFakeGit(t, "aaaa111\trefs/tags/v1.2.0\ncccc333\trefs/tags/v1.3.0-rc1")
	lc := &scriptedLivecheck{}
	row := observerWith(t, git, lc).Observe(context.Background(), githubPort(t, "1.2.0"))

	assert.Equal(t, OutcomeCurrent, row.Outcome)
	assert.Zero(t, lc.count())
	assert.Contains(t, row.Detail, "1.3.0-rc1")
	assert.Contains(t, row.Detail, "prerelease")
}

// A port already riding a prerelease is judged against every tag,
// because upstream has cut nothing else and a higher prerelease is the
// only move it has. A stable-only candidacy test would close that
// before any verdict was reached.
func TestPrereleaseRidingPortIsACandidate(t *testing.T) {
	git := newFakeGit(t, "aaaa111\trefs/tags/v1.3.0-rc1\ncccc333\trefs/tags/v1.3.0-rc2")
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, Version: "1.3.0-rc2"}}
	row := observerWith(t, git, lc).Observe(context.Background(), githubPort(t, "1.3.0-rc1"))

	assert.Equal(t, 1, lc.count(), "the port's only update path was closed without asking")
	assert.Equal(t, OutcomeOutdated, row.Outcome)
	assert.Equal(t, "1.3.0-rc2", row.Latest)
	assert.Equal(t, PrereleaseLateral.String(), row.Verdict)
}

// --deep pays for the second witness on every port. It is the setting
// that finds an update no forge tag reveals, and the cost of the
// default is exactly this.
func TestDeepRunsTheSecondWitnessAnyway(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, UpToDate: true}}
	o := observerWith(t, git, lc)
	o.Deep = true

	row := o.Observe(context.Background(), githubPort(t, "1.3.0"))
	assert.Equal(t, 1, lc.count())
	assert.Equal(t, OutcomeCurrent, row.Outcome)
	assert.Equal(t, Agreement.String(), row.Verdict)
}

// A port with no forge has no cheap stage, so it goes straight to the
// expensive one. Stage one concluded nothing about it, and reporting
// it current on that silence would be a lie about a third of the tree.
func TestNoForgeGoesStraightToLivecheck(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, Version: "2.0"}}
	h := portAt(t, "PortSystem 1.0\n\nname widget\nversion 1.0\n",
		info.Values{Name: "widget", Version: "1.0"}, nil)

	row := observerWith(t, git, lc).Observe(context.Background(), h)

	assert.Equal(t, 1, lc.count())
	assert.Zero(t, git.calls(), "a port with no forge has no forge to ask")
	assert.Equal(t, OutcomeOutdated, row.Outcome)
	assert.Equal(t, LivecheckOnly.String(), row.Verdict)
	assert.Empty(t, row.Sha, "there is no tag behind a livecheck-only answer")
}

// Two ports built from one repository are one observation of one
// forge. The cache key is the repository and not the port, which is
// what makes two subports of one Portfile — and a port beside its
// -devel sibling — cost one round trip between them.
func TestASecondPortOnTheSameRepoCostsNoRoundTrip(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{}
	o := observerWith(t, git, lc)
	ctx := context.Background()

	first := o.Observe(ctx, githubPort(t, "1.3.0"))
	second := o.Observe(ctx, githubPort(t, "1.3.0"))

	assert.Equal(t, 1, git.calls(), "the second port asked the forge the same question again")
	assert.Equal(t, "fetched", first.Stages[0].Source)
	assert.Equal(t, "fresh", second.Stages[0].Source)
}

// A host that refused dockhand is its own state, not a failure of the
// port. The band is upstream's, the remedy is to ask again later, and
// the census must not count it as something broken.
func TestWalledHostIsItsOwnState(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{}
	o := observerWith(t, git, lc)
	o.Pacer = courtesy.NewPacer(courtesy.Policy{Ceiling: 2, Backoff: time.Hour}, nil)
	o.Pacer.Wall("github.com", errors.New("You have exceeded a secondary rate limit"))

	row := o.Observe(context.Background(), githubPort(t, "1.2.0"))

	assert.Equal(t, OutcomeWalled, row.Outcome)
	assert.False(t, row.Outcome.Hard(), "a host that refused us is not a broken port")
	assert.Equal(t, exitcode.WitnessAPI, row.Code)
	assert.Equal(t, "upstream", row.Family)
	assert.Equal(t, "witness-walled", row.Reason)
	assert.Contains(t, row.Detail, "secondary rate limit")
	assert.Zero(t, git.calls(), "the request the wall exists to prevent was made anyway")
	assert.Zero(t, lc.count(), "a port reported on one witness while the other is walled claims two")
}

// A forge that answers a refusal raises the wall for every other port
// on it. Reading the refusal is the caller's because there is no
// status code to test — just a forge's words on the error of whichever
// tool asked it.
func TestARefusalRaisesTheWallForTheWholeHost(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"),
		[]byte("#!/bin/sh\necho 'fatal: too many requests, retry-after 60' >&2\nexit 128\n"), 0o755)) //nolint:gosec // a test fixture that must be executable
	tools := tool.NewFinder(func(name string) (string, error) {
		if name == "git" {
			return filepath.Join(dir, "git"), nil
		}
		return exec.LookPath(name)
	})
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, UpToDate: true}}
	o := &Observer{
		Tools:   tools,
		Fetcher: lc,
		Pacer:   courtesy.NewPacer(courtesy.Policy{Ceiling: 2, Backoff: time.Hour}, nil),
		Cache:   courtesy.NewCache(t.TempDir(), time.Hour, nil),
	}
	ctx := context.Background()

	// The first port meets the refusal. Its forge witness failed, so
	// it falls through to livecheck rather than being lost.
	first := o.Observe(ctx, githubPort(t, "1.2.0"))
	assert.Equal(t, OutcomeCurrent, first.Outcome)
	assert.Contains(t, first.Detail, "retry-after")

	left, up := o.Pacer.Walled("github.com")
	assert.True(t, up, "a forge that said retry-after is still being asked")
	assert.Positive(t, left)

	// The second port on the same host is refused without a request.
	second := o.Observe(ctx, githubPort(t, "1.2.0"))
	assert.Equal(t, OutcomeWalled, second.Outcome)
}

// A forge that stops answering without saying why is walled by the
// failures themselves.
//
// This is the expensive direction and the reason the strike exists. An
// unreachable github.com — DNS, a captive network, an outage — matches
// no refusal phrase, so nothing would wall it; and because an empty
// forge answer promotes a port to a candidate, the sweep would convert
// itself from one ls-remote per port into one livecheck per port
// against thousands of unrelated web sites, at the moment the cheap
// witness stopped working. Three in a row is the pool's own number for
// the same judgment.
func TestAForgeThatStoppedAnsweringIsWalledByItsFailures(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"),
		[]byte("#!/bin/sh\necho 'fatal: unable to access: Could not resolve host: github.com' >&2\nexit 128\n"), 0o755)) //nolint:gosec // a test fixture that must be executable
	tools := tool.NewFinder(func(name string) (string, error) {
		if name == "git" {
			return filepath.Join(dir, "git"), nil
		}
		return exec.LookPath(name)
	})
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, UpToDate: true}}
	o := &Observer{
		Tools:   tools,
		Fetcher: lc,
		Pacer:   courtesy.NewPacer(courtesy.Policy{Ceiling: 2, Backoff: time.Hour, Strikes: 3}, nil),
		// No cache: every port asks, which is what a sweep of
		// different repositories on one dead host does.
		Cache: courtesy.NewCache("", 0, nil),
	}
	ctx := context.Background()

	// The first three ports each meet the outage. Falling through to
	// livecheck is right for one bad repository, and it is exactly the
	// cost that must not go on for four thousand ports.
	for i := range 3 {
		row := o.Observe(ctx, githubPort(t, "1.2.0"))
		assert.Equal(t, OutcomeCurrent, row.Outcome, "port %d", i)
		assert.Contains(t, row.Detail, "Could not resolve host")
	}
	assert.Equal(t, 3, lc.count(), "the expensive witness ran for each port the forge failed on")

	left, up := o.Pacer.Walled("github.com")
	require.True(t, up, "a host that failed three times in a row is still being asked")
	assert.Positive(t, left)

	// The fourth port is walled: no forge request, and no livecheck
	// either. That is the whole saving.
	row := o.Observe(ctx, githubPort(t, "1.2.0"))
	assert.Equal(t, OutcomeWalled, row.Outcome)
	assert.False(t, row.Outcome.Hard(), "a host that stopped answering is not a broken port")
	assert.Contains(t, row.Detail, "3 requests in a row failed")
	assert.Equal(t, 3, lc.count(), "the walled port ran the expensive witness anyway")
}

// A run of failures walls; scattered ones do not. A host that answers
// between two bad repositories is a working host, and walling it would
// cost a sweep thousands of ports it could have had.
func TestASuccessClearsTheStreak(t *testing.T) {
	p := courtesy.NewPacer(courtesy.Policy{Ceiling: 2, Backoff: time.Hour, Strikes: 3}, nil)
	boom := errors.New("fatal: repository not found")
	p.Struck("github.com", boom)
	p.Struck("github.com", boom)
	p.Cleared("github.com")
	p.Struck("github.com", boom)
	p.Struck("github.com", boom)
	_, up := p.Walled("github.com")
	assert.False(t, up, "two failures either side of a success walled the host")

	p.Struck("github.com", boom)
	_, up = p.Walled("github.com")
	assert.True(t, up, "three in a row did not wall the host")
}

// Witnesses that ran and left nothing anybody may act on are
// upstream's silence: the LatestUnresolved band, exit 53, and not a
// hard error — a rotted livecheck says nothing about the sweep.
func TestRottedLivecheckIsUnresolvedAndNotHard(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, NoMatch: true}}
	row := observerWith(t, git, lc).Observe(context.Background(), githubPort(t, "1.0"))

	assert.Equal(t, OutcomeUnresolved, row.Outcome)
	assert.False(t, row.Outcome.Hard())
	assert.Equal(t, exitcode.LatestUnresolved, row.Code)
	assert.Equal(t, "witness-unresolved", row.Reason)
	assert.Equal(t, LivecheckRot.String(), row.Verdict)
}

// A port that cannot be evaluated at all is the one thing that IS a
// hard error: the sweep could not examine it.
func TestAnUnevaluablePortIsHard(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	h := port.New(tree.Target{Portdir: filepath.Join(t.TempDir(), "gone")}, scriptedOracle{})
	row := observerWith(t, git, &scriptedLivecheck{}).Observe(context.Background(), h)

	assert.Equal(t, OutcomeFailed, row.Outcome)
	assert.True(t, row.Outcome.Hard())
	assert.Equal(t, "gone", row.Port,
		"a row with no port leaves the census naming nowhere to start")
}

// serialProbe measures how many callers are inside the witness at
// once. It holds itself open for a few milliseconds, which is what
// makes the measurement deterministic rather than a race the scheduler
// may or may not lose.
type serialProbe struct {
	mu       sync.Mutex
	inFlight int
	most     int
	calls    int
	result   portfetch.LivecheckResult
}

func (l *serialProbe) Livecheck(context.Context, string, string) (portfetch.LivecheckResult, error) {
	l.mu.Lock()
	l.inFlight++
	l.calls++
	if l.inFlight > l.most {
		l.most = l.inFlight
	}
	l.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
	return l.result, nil
}

// A sweep runs one worker per evaluator and every one of them may
// reach the livecheck witness. The fetch session underneath is serial
// by its own contract — "not safe for concurrent use", in its own
// words — so the observer has to honour that. The pacer cannot: it
// spaces requests, and spacing is not mutual exclusion.
//
// The ports here name no forge, so the only witness in play is the one
// under test and nothing else is serializing the workers by accident.
func TestLivecheckWitnessIsSerialized(t *testing.T) {
	git := newFakeGit(t, refsCurrent)
	lc := &serialProbe{result: portfetch.LivecheckResult{Ran: true, UpToDate: true}}
	o := observerWith(t, git, lc)
	// A ceiling of eight and no interval: nothing but the observer's
	// own promise stands between these workers and the witness.
	o.Pacer = courtesy.NewPacer(courtesy.Policy{Ceiling: 8}, nil)

	const workers = 8
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range workers {
		// A distinct version per port, so no two share a cache key and
		// every one of them really reaches the witness.
		h := portAt(t, fmt.Sprintf("PortSystem 1.0\n\nname widget\nversion 1.%d\n", i),
			info.Values{Name: "widget", Version: fmt.Sprintf("1.%d", i)}, nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.Equal(t, OutcomeCurrent, o.Observe(ctx, h).Outcome)
		}()
	}
	wg.Wait()

	assert.Equal(t, workers, lc.calls, "every port was owed the witness")
	assert.Equal(t, 1, lc.most,
		"two workers were inside a witness that is serial by its own contract")
	assert.Zero(t, git.calls(), "these ports name no forge")
}

// A port that was never evaluated still has a name: the subport the
// target names, or the portdir's own base name.
func TestPortName(t *testing.T) {
	assert.Equal(t, "p5-boolean", PortName(tree.Target{Portdir: "/tree/perl/p5-boolean"}))
	assert.Equal(t, "libftdi0", PortName(tree.Target{Portdir: "/tree/devel/libftdi", Subport: "libftdi0"}))
}

// Hard is a total ruling over a closed set, and an outcome nobody
// declared cannot be argued to be benign.
func TestOutcomeHardIsTotal(t *testing.T) {
	for _, o := range Outcomes {
		switch o {
		case OutcomeAbandoned, OutcomeFailed:
			assert.True(t, o.Hard(), string(o))
		case OutcomeOutdated, OutcomeCurrent, OutcomeWalled, OutcomeUnresolved, OutcomeExcluded:
			assert.False(t, o.Hard(), string(o))
		}
	}
	assert.True(t, Outcome("invented").Hard())
	assert.Len(t, Outcomes, 7, "an outcome added without a Hard ruling breaks the census's arithmetic")
}

// The releases witness asks conditionally and reads the answer's
// status line, so an unchanged feed costs a 304 and no body.
func TestParseGhResponse(t *testing.T) {
	body := `[{"tag_name":"v1.3.0"}]`
	answer, err := parseGhResponse("HTTP/2.0 200 OK\r\nEtag: W/\"abc\"\r\nX-Other: 1\r\n\r\n"+body, "")
	require.NoError(t, err)
	assert.False(t, answer.NotModified)
	assert.Equal(t, `W/"abc"`, answer.Validator)
	assert.JSONEq(t, body, string(answer.Body))

	answer, err = parseGhResponse("HTTP/2.0 304 Not Modified\r\nEtag: W/\"abc\"\r\n\r\n", `W/"abc"`)
	require.NoError(t, err)
	assert.True(t, answer.NotModified)
	assert.Equal(t, `W/"abc"`, answer.Validator, "a 304 keeps the validator it revalidated against")

	// A gh that stops honouring --include costs a body every time and
	// is still correct, which is the whole reason for the fallback.
	answer, err = parseGhResponse(body, "")
	require.NoError(t, err)
	assert.False(t, answer.NotModified)
	assert.JSONEq(t, body, string(answer.Body))
}

func TestReleasesArgsAreConditional(t *testing.T) {
	args := releasesArgs("dockhand", "widget", `W/"abc"`, "dockhand/test")
	assert.Equal(t, []string{
		"api", "repos/dockhand/widget/releases?per_page=100", "--include",
		"-H", `If-None-Match: W/"abc"`,
		"-H", "User-Agent: dockhand/test",
	}, args)
	assert.NotContains(t, releasesArgs("a", "b", "", ""), "If-None-Match: ",
		"a cold key has nothing to revalidate against")
}

// The releases feed is authoritative and replaces the tags, and the
// tags stay in hand — which is what makes the corroboration free here
// where Check pays a second ls-remote for it.
func TestReleasesReplaceTagsAndTagsStillCorroborate(t *testing.T) {
	git := newFakeGit(t, "aaaa111\trefs/tags/v1.2.0\nbbbb222\trefs/tags/v1.3.0")
	lc := &scriptedLivecheck{result: portfetch.LivecheckResult{Ran: true, Version: "1.3.0"}}
	o := observerWith(t, git, lc)
	var asked [][]string
	o.Gh = func(_ context.Context, args ...string) (string, error) {
		asked = append(asked, args)
		// Upstream cut a release for 1.2.0 and never one for 1.3.0,
		// which it nonetheless tagged: the gopass shape.
		return "HTTP/2.0 200 OK\r\nEtag: W/\"r1\"\r\n\r\n" + `[{"tag_name":"v1.2.0"}]`, nil
	}

	row := o.Observe(context.Background(), githubPort(t, "1.2.0"))

	require.Len(t, asked, 1)
	assert.Equal(t, TagWithoutRelease.String(), row.Verdict,
		"the tags are already in hand, so the corroboration costs nothing")
	assert.Equal(t, "1.3.0", row.Latest)
	assert.Equal(t, "bbbb222", row.Sha)
	assert.Equal(t, 1, git.calls(),
		"the corroboration went back to the forge for tags it was already holding")
}

// The digest is the git remote's stand-in for an ETag, so it must
// change when upstream moves and only then. A forge that reorders its
// refs has not moved.
func TestDigestIsStableUnderReorderingAndMovesWithTheShas(t *testing.T) {
	a := []RawRef{{Sha: "1", Tag: "v1"}, {Sha: "2", Tag: "v2"}}
	b := []RawRef{{Sha: "2", Tag: "v2"}, {Sha: "1", Tag: "v1"}}
	assert.Equal(t, Digest(a), Digest(b), "a reordered ref list is the same answer")

	moved := []RawRef{{Sha: "1", Tag: "v1"}, {Sha: "9", Tag: "v2"}}
	assert.NotEqual(t, Digest(a), Digest(moved),
		"a tag moved to another commit is upstream moving, with an identical name list")
	assert.NotEqual(t, Digest(a), Digest(append(a, RawRef{Sha: "3", Tag: "v3"})))
	assert.NotEqual(t, Digest(nil), Digest(a))
}

// Tags keeps its signature and its answer for the callers that had it
// before the sha column existed.
func TestTagsIsUnchangedByTheShaColumn(t *testing.T) {
	git := newFakeGit(t, "aaaa111\trefs/tags/v1.2.0\nbbbb222\trefs/tags/v1.3.0\n"+
		"cccc333\trefs/tags/v1.3.0^{}\ndddd444\trefs/tags/nightly\neeee555\trefs/heads/main")
	repo := Repo{URL: "https://github.com/dockhand/widget", TagPrefix: "v"}

	got, err := Tags(context.Background(), git.finder(), repo)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.2.0", "1.3.0"}, got,
		"peeled refs, branches and tags outside the scheme are still excluded")

	refs, err := TagRefs(context.Background(), git.finder(), repo)
	require.NoError(t, err)
	assert.Equal(t, []Ref{{Version: "1.2.0", Sha: "aaaa111"}, {Version: "1.3.0", Sha: "bbbb222"}}, refs)
	assert.Equal(t, got, Versions(refs))
}

func TestShaOfMatchesTheVersionsSpelling(t *testing.T) {
	refs := []Ref{{Version: "1.2.0", Sha: "aaaa"}, {Version: "1.03", Sha: "bbbb"}}
	assert.Equal(t, "aaaa", ShaOf(refs, "1.2.0"))
	assert.Equal(t, "bbbb", ShaOf(refs, "1.3"), "vercmp-equal spellings are the same version")
	assert.Empty(t, ShaOf(refs, "9.9"))
	assert.Empty(t, ShaOf(refs, ""))
}

// A missing git is the forge witness being unavailable, not the
// machine being unfit: the livecheck witness may still answer.
func TestNoGitIsAWitnessFailure(t *testing.T) {
	tools := tool.NewFinder(func(string) (string, error) { return "", errors.New("nope") })
	_, err := LsRemote(context.Background(), tools, "", "https://github.com/a/b")
	require.ErrorIs(t, err, ErrNoGit)
	var we *WitnessError
	require.ErrorAs(t, err, &we)
	assert.Equal(t, exitcode.WitnessUnreachable, we.DockhandExit())
}

func TestRefusedReadsAForgesWords(t *testing.T) {
	for _, msg := range []string{
		"API rate limit exceeded for 1.2.3.4",
		"You have exceeded a secondary rate limit",
		"HTTP 429: Too Many Requests",
		"fatal: unable to access: The requested URL returned error: 403",
		"retry-after: 60",
	} {
		assert.True(t, refused(errors.New(msg)), msg)
	}
	for _, msg := range []string{
		"fatal: repository not found",
		"ssh: connect to host: Connection timed out",
		"could not read Username for 'https://github.com'",
	} {
		assert.False(t, refused(errors.New(msg)), msg)
	}

	// The shape that actually reaches refused() is the wrapped one, and
	// the wrapper puts the repository URL in front of git's words. The
	// refusal has to be read off the words alone: a repository whose
	// name contains one of the phrases would otherwise wall its whole
	// forge — 4561 of this tree's ports, behind one unrelated error.
	assert.True(t,
		refused(lsRemoteFailed("https://github.com/a/b", errors.New("fatal: unable to access: HTTP 429"))),
		"a real refusal still reads as one through the wrapper")
	assert.False(t,
		refused(lsRemoteFailed("https://github.com/acme/rate-limit-proxy", errors.New("fatal: repository not found"))),
		"a repository NAMED like a refusal must not wall its forge")
	assert.False(t,
		refused(lsRemoteFailed("https://github.com/abuse/403", errors.New("fatal: could not read from remote repository"))),
		"nor a URL carrying two of the phrases at once")
}

func TestCandidacy(t *testing.T) {
	assert.True(t, candidate("1.2.0", []string{"1.2.0", "1.3.0"}))
	assert.False(t, candidate("1.3.0", []string{"1.2.0", "1.3.0"}))
	assert.False(t, candidate("1.3.0", []string{"1.3.0", "1.4.0-beta"}),
		"a stable port is not made outdated by a beta")
	assert.True(t, candidate("1.4.0-beta1", []string{"1.4.0-beta2"}),
		"a port riding prereleases follows them upward")
	assert.True(t, candidate("", []string{"1.0"}),
		"no version to compare against means the expensive witnesses are owed")
	assert.False(t, candidate("1.0", nil))
}

func TestJoinDetailKeepsWhatAnEarlierStageSaid(t *testing.T) {
	assert.Equal(t, "a; b", joinDetail("a", "b"))
	assert.Equal(t, "b", joinDetail("", "b"))
	assert.Equal(t, "a", joinDetail("a", ""))
	assert.Empty(t, joinDetail("", ""))
}

func TestCensusCountsRowsAndTheirBudget(t *testing.T) {
	var c Census
	c.Add(Row{Port: "a", Outcome: OutcomeOutdated, Stages: []Witnessed{
		{Witness: WitnessLsRemote, Source: "fetched"},
		{Witness: WitnessLivecheck, Source: "fetched"},
	}})
	c.Add(Row{Port: "b", Outcome: OutcomeCurrent, Stages: []Witnessed{
		{Witness: WitnessLsRemote, Source: "fresh"},
	}})
	c.Add(Row{Port: "c", Outcome: OutcomeCurrent, Stages: []Witnessed{
		{Witness: WitnessLsRemote, Source: "revalidated"},
	}})

	assert.Equal(t, 3, c.Total())
	assert.Equal(t, 2, c.Count(OutcomeCurrent))
	assert.Equal(t, 2, c.Asked(WitnessLsRemote), "a fresh observation costs no round trip")
	assert.Equal(t, 1, c.Asked(WitnessLivecheck))
	require.NoError(t, c.Err())

	tail := c.String()
	assert.Contains(t, tail, "3 ports examined")
	assert.Contains(t, tail, "outdated")
	assert.Contains(t, tail, "ls-remote        2 asked  (1 fetched, 1 fresh, 1 revalidated)")
	assert.NotContains(t, tail, "abandoned", "an outcome that did not occur buries the ones that did")
}

// A walled sweep exits 0, correctly, and the tail owes the reader the
// sentence the exit status cannot carry: those ports were not examined
// and running again finishes them. A wall lasts a quarter of an hour
// and a paced sweep of a tree takes longer, so a wall raised early
// turns thousands of rows walled and then quietly resumes.
func TestCensusTailNamesTheWalledRemedy(t *testing.T) {
	var c Census
	c.Add(Row{Port: "a", Outcome: OutcomeCurrent})
	assert.NotContains(t, c.String(), "Run again later",
		"a remedy for a wall nobody hit")

	c.Add(Row{Port: "b", Outcome: OutcomeWalled})
	tail := c.String()
	assert.Contains(t, tail, "walled")
	assert.Contains(t, tail, "1 port(s) were not examined")
	assert.Contains(t, tail, "Run again later to finish them")
	require.NoError(t, c.Err(), "a host refusing us is not a broken port")
}

// The exit rule, which is the report's whole contract with a script:
// 0 when every port was examined, 83 when some of them were not, and
// being outdated is never in it.
func TestCensusExitSemantics(t *testing.T) {
	var c Census
	c.Add(Row{Port: "a", Outcome: OutcomeOutdated})
	c.Add(Row{Port: "b", Outcome: OutcomeWalled})
	c.Add(Row{Port: "c", Outcome: OutcomeUnresolved})
	c.Add(Row{Port: "d", Outcome: OutcomeExcluded})
	require.NoError(t, c.Err(), "a report whose exit changed because upstream shipped is unusable")

	c.Add(Row{Port: "e", Outcome: OutcomeAbandoned})
	err := c.Err()
	require.Error(t, err)
	assert.Equal(t, exitcode.SweepHardErrors, exitcode.TwinOf(err).Code)
	assert.Equal(t, "sweep-hard-errors", exitcode.TwinOf(err).Reason)
	require.ErrorContains(t, err, "starting at e")

	var empty Census
	require.NoError(t, empty.Err())
	assert.Equal(t, "0 ports examined\n", empty.String())

	var one Census
	one.Add(Row{Port: "jq", Outcome: OutcomeCurrent})
	assert.Contains(t, one.String(), "1 port examined")
}

// ForgeCurrent is a judgment: the forge answered and dockhand
// concluded from what it said. It must not be banded as upstream's
// silence, which is what its Judged entry is for.
func TestForgeCurrentIsAJudgment(t *testing.T) {
	assert.True(t, Judged(ForgeCurrent))
	assert.Contains(t, ForgeCurrent.String(), "nothing newer")
	assert.NotEqual(t, "unknown verdict", ForgeCurrent.String())
}

// observeForge asks the cheap witness first and reaches the same
// answer.
//
// The order moved and the answer did not, which is the whole claim.
// Releases still REPLACE tags where a repository publishes them, so
// what a verdict is reached over is unchanged; what changed is that the
// unauthenticated, unmetered request goes first, because it yields the
// digest the releases observation is keyed on — so an unmoved forge
// costs a conditional request rather than a body — and because its tags
// are what the LivecheckAhead corroboration needs, which used to be a
// second round trip.
func TestObserveForgeAsksTheCheapWitnessFirst(t *testing.T) {
	repo := Repo{URL: "https://github.com/dockhand/widget", TagPrefix: "v",
		Forge: forgeAt(t, "https://github.com/dockhand/widget")}
	ctx := context.Background()

	// A repository that publishes releases: the feed wins, and the tags
	// are still in hand for the corroboration.
	git := newFakeGit(t, refsCurrent)
	var ghArgs []string
	gh := func(_ context.Context, args ...string) (string, error) {
		ghArgs = args
		return `[{"tag_name":"v1.3.0","prerelease":false,"draft":false}]`, nil
	}
	versions, authoritative, refs, err := observeForge(ctx, git.finder(), gh, repo, Manners{})
	require.NoError(t, err)
	assert.Equal(t, []string{"1.3.0"}, versions)
	assert.True(t, authoritative, "upstream said which of its tags it means")
	assert.Equal(t, []string{"1.2.0", "1.3.0"}, Versions(refs),
		"the tags the corroboration needs are already in hand")
	assert.Equal(t, 1, git.calls(), "the cheap witness is asked once, not twice")
	assert.Contains(t, strings.Join(ghArgs, " "), "repos/dockhand/widget/releases")

	// A repository that publishes none: the tags stand, and that is not
	// a failure of anything.
	git = newFakeGit(t, refsCurrent)
	empty := func(context.Context, ...string) (string, error) { return "[]", nil }
	versions, authoritative, refs, err = observeForge(ctx, git.finder(), empty, repo, Manners{})
	require.NoError(t, err)
	assert.Equal(t, []string{"1.2.0", "1.3.0"}, versions)
	assert.False(t, authoritative)
	assert.Len(t, refs, 2)

	// No gh at all is the same answer by a shorter road.
	git = newFakeGit(t, refsCurrent)
	versions, authoritative, _, err = observeForge(ctx, git.finder(), nil, repo, Manners{})
	require.NoError(t, err)
	assert.Equal(t, []string{"1.2.0", "1.3.0"}, versions)
	assert.False(t, authoritative)
}

// A repository whose git protocol is blocked and whose API is not is
// still fully witnessed. It was answered before the order changed, and
// it has to stay answered: the tags failure is returned only when the
// releases feed did not answer either, because with neither there is no
// third witness and a verdict would read as if both had spoken.
func TestObserveForgeSurvivesOneWitnessFailing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"),
		[]byte("#!/bin/sh\necho 'fatal: unable to access: Could not resolve host' >&2\nexit 128\n"), 0o755)) //nolint:gosec // a test fixture that must be executable
	tools := tool.NewFinder(func(name string) (string, error) {
		if name == "git" {
			return filepath.Join(dir, "git"), nil
		}
		return exec.LookPath(name)
	})
	repo := Repo{URL: "https://github.com/dockhand/widget", TagPrefix: "v",
		Forge: forgeAt(t, "https://github.com/dockhand/widget")}
	ctx := context.Background()

	gh := func(context.Context, ...string) (string, error) {
		return `[{"tag_name":"v1.3.0","prerelease":false,"draft":false}]`, nil
	}
	versions, authoritative, _, err := observeForge(ctx, tools, gh, repo, Manners{})
	require.NoError(t, err, "a repository the API can answer for is not lost to a git failure")
	assert.Equal(t, []string{"1.3.0"}, versions)
	assert.True(t, authoritative)

	// Neither witness answers: the tags failure is what the caller gets,
	// because judging on the livecheck alone would publish a verdict
	// whose words claim two.
	_, _, _, err = observeForge(ctx, tools, func(context.Context, ...string) (string, error) {
		return "[]", nil
	}, repo, Manners{})
	require.Error(t, err)
	var we *WitnessError
	require.ErrorAs(t, err, &we)
	assert.Equal(t, "ls-remote", we.Witness)
}

// forgeAt resolves the forge a repository URL names, so a Repo built by
// hand carries the one its coordinates would have given it.
func forgeAt(t *testing.T, url string) *forge.Forge {
	t.Helper()
	f, ok := forge.FromRepoURL(url)
	require.True(t, ok, url)
	return f
}
