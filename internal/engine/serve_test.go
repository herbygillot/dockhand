package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/store"
)

// A port that may have a newer release is named once for what was set
// aside, and counted after, not named every morning: dolt's misspelled
// old tag leaves it uncertain every day it's otherwise current. One whose
// set-aside tags change is named again.
func TestAnUncertainPortIsSaidOnceForWhatWasSetAside(t *testing.T) {
	t.Parallel()
	first := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.15"}}}
	require.Equal(t, "serve: 1 port may have newer releases and need review: dolt. Run dockhand outdated dolt for details", uncertainWords(OutdatedLook{}, first))
	require.Equal(t, "serve: 1 port may still have newer releases; see dockhand status for the list", uncertainWords(first, first))
	next := OutdatedLook{Uncertain: []string{"dolt", "yq"}, SetAside: map[string][]string{"dolt": {"v040.15"}, "yq": {"v5.0"}}}
	require.Equal(t, "serve: 1 port may have newer releases and need review: yq. Run dockhand outdated yq for details; 1 previously reported", uncertainWords(first, next))
	moved := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.16"}}}
	require.Contains(t, uncertainWords(first, moved), "need review: dolt")
	require.Empty(t, uncertainWords(first, OutdatedLook{}))
}

// A file serve couldn't write is said once a day, each file for itself,
// where it was dropped, and status read an older state without a word
// (the architecture review's L3e).
func TestServeSaysAFileItCouldntWriteOnceADay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	var said []string
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	s := newServer(e, ServeOptions{Say: func(line string) { said = append(said, line) }, Now: func() time.Time { return now }})
	full := errors.New("no space left on device")
	s.written("serving.json", nil)
	s.written("serving.json", full)
	s.written("serving.json", full)
	s.written("outdated.json", full)
	require.Equal(t, []string{
		"serve: couldn't write " + e.serveFile("serving.json") + ", so status may read an older state: no space left on device",
		"serve: couldn't write " + e.serveFile("outdated.json") + ", so status may read an older state: no space left on device",
	}, said)
	now = now.AddDate(0, 0, 1)
	s.written("serving.json", full)
	require.Len(t, said, 3, "said again the next day")
}

// What status and queue read of serve's login is written before serve
// says it, so a reader that follows serve's line finds it: D-C3 read
// queue right after "GitHub rejected its login" and found serve acting
// (the rc9 full stage).
func TestServeWritesItsLoginStateBeforeSayingIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	answers := []error{nil, fmt.Errorf("%w: GitHub rejected the credential", github.ErrAuthentication), nil}
	read := map[string]string{}
	s := newServer(e, ServeOptions{
		IdentityEvery: time.Nanosecond,
		Identity: func(context.Context) (string, error) {
			err := answers[0]
			answers = answers[1:]
			return "ada", err
		},
		Say: func(line string) {
			serving, _ := e.lastServing()
			read[line] = serving.NotActing
		},
	})
	g := &loginGate{s: s}
	for range 3 {
		g.last = time.Time{}
		g.look(t.Context())
	}
	require.Equal(t, "GitHub rejected the login; dockhand setup github",
		read["serve: GitHub rejected the credentials. GitHub writes are paused; run dockhand setup github to sign in again"])
	got, ok := read["serve: logged in again as ada; acting on GitHub again"]
	require.True(t, ok)
	require.Empty(t, got, "acting again, written before it's said")
}

// A pass serve's own stop cancelled says nothing and keeps its state, where
// D-I5 on rc5 read "could not verify the GitHub account (… context
// canceled) … retrying each minute" before "serve: stopped"; GitHub out of
// reach while serve runs is still said, once.
func TestServeSaysNothingOfWhatItsStopCancelled(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	var said []string
	unreachable := errors.New("couldn't reach GitHub (no such host)")
	s := newServer(e, ServeOptions{
		IdentityEvery: time.Nanosecond,
		Refresh:       time.Nanosecond,
		Identity: func(ctx context.Context) (string, error) {
			if ctx.Err() != nil {
				return "", fmt.Errorf("Get \"https://api.github.com/user\": %w", ctx.Err())
			}
			return "", unreachable
		},
		Say: func(line string) { said = append(said, line) },
	})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	g := &loginGate{s: s}
	g.look(cancelled)
	(&follower{s: s}).maybe(cancelled)
	require.Empty(t, said)
	require.Empty(t, g.problem)
	for range 2 {
		g.last = time.Time{}
		g.look(t.Context())
	}
	require.Equal(t, []string{"serve: could not verify the GitHub account (couldn't reach GitHub (no such host)). Continuing with the current authentication state; retrying each minute"}, said)
}

// What the work reports under serve is said as serve's lines, where it
// went to standard error plain, and a fetch of master nobody waits on
// isn't said (rc6's B7: "Fetching master from …" 87 times in 16 hours).
func TestServesWorkReportsAsServesLines(t *testing.T) {
	t.Parallel()
	var said []string
	s := newServer(nil, ServeOptions{Say: func(line string) { said = append(said, line) }})
	plain := 0
	ctx := s.reporting(progress.WithReporter(t.Context(), func(progress.Update) { plain++ }))
	progress.Report(ctx, "Looking up 3 ports")
	progress.VerboseReport(ctx, "behind the scenes")
	require.Equal(t, []string{"serve: Looking up 3 ports"}, said)
	require.Zero(t, plain, "nothing goes to standard error plain")
	require.True(t, serving(ctx))
}

// A worker's problem is said once while it stands, forgotten once a pass
// doesn't find it, and said again when it comes back; a pass serve's stop
// cut short forgets nothing (the code-organization review's serve finding
// 3: cleanup and the day's look never forgot one).
func TestServeSaysAWorkersProblemOnceWhileItStands(t *testing.T) {
	t.Parallel()
	var said []string
	s := newServer(nil, ServeOptions{Say: func(line string) { said = append(said, line) }})
	pass := func(ctx context.Context, problems ...string) {
		p := s.problems.pass("cleanup")
		for _, problem := range problems {
			p.say(ctx, problem)
		}
		p.done(ctx)
	}
	ctx := t.Context()
	pass(ctx, "serve: cleanup: disk full")
	pass(ctx, "serve: cleanup: disk full")
	require.Equal(t, []string{"serve: cleanup: disk full"}, said, "said once while it stands")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	pass(cancelled)
	pass(ctx, "serve: cleanup: disk full")
	require.Len(t, said, 1, "a pass serve's stop cut short forgets nothing")
	pass(ctx)
	pass(ctx, "serve: cleanup: disk full")
	require.Equal(t, []string{"serve: cleanup: disk full", "serve: cleanup: disk full"}, said, "said again once it came back")
	other := s.problems.pass("look")
	other.say(ctx, "serve: cleanup: disk full")
	require.Len(t, said, 3, "each worker's own")
}

// A leader whose lease another serve took, as a standby that judged it
// dead does, stops rather than drive work twice, where no test reached it
// (the test analysis's step b).
func TestALeaderThatLostItsLeaseStops(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	other := session(t, e)
	leading := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- e.Serve(t.Context(), session(t, e), ServeOptions{Poll: 10 * time.Millisecond, Say: func(line string) {
			if strings.HasPrefix(line, "serve: leading") {
				once.Do(func() { close(leading) })
			}
		}})
	}()
	select {
	case <-leading:
	case err := <-done:
		t.Fatalf("serve ended before it led: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve never led")
	}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		_, err := tx.AcquireLease(coord.LeaderResource, other.ID())
		return err
	}))
	select {
	case err := <-done:
		require.EqualError(t, err, "serve: another serve took over the lead; stopping")
	case <-time.After(30 * time.Second):
		t.Fatal("the leader that lost its lease kept serving")
	}
}

// serve settles once its queue empties after work: the memory its checks
// used goes back to macOS, and Settled, which DOCKHAND_HEAPPROFILE's heap
// profile is taken in, is called (the roadmap's "serve gives back a look's
// memory once its work ends").
func TestServeSettlesOnceItsQueueEmptiesAfterWork(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	queued := queuedHarborRun(t, e, tahoeArm)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	settled := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		done <- e.Serve(ctx, session(t, e), ServeOptions{Poll: 10 * time.Millisecond, Settled: func() { settled <- struct{}{} }})
	}()
	select {
	case <-settled:
	case err := <-done:
		t.Fatalf("serve ended before it settled: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve never settled")
	}
	run, err := e.Run(t.Context(), queued.ID)
	require.NoError(t, err)
	require.True(t, run.State.Finished(), "settled once the check ended: %s", run.State)
	time.Sleep(100 * time.Millisecond)
	require.Empty(t, settled, "once, not each quiet turn")
	cancel()
	<-done
}

// serveCountFailure is a store whose first serve.submit event fails to
// record.
type serveCountFailure struct {
	store.Store
	fail *bool
}

type serveCountFailureTx struct {
	store.Tx
	fail *bool
}

func (s serveCountFailure) Update(ctx context.Context, repo model.RepositoryID, fn func(store.Tx) error) error {
	return s.Store.Update(ctx, repo, func(tx store.Tx) error { return fn(serveCountFailureTx{tx, s.fail}) })
}

func (tx serveCountFailureTx) AppendEvent(event model.Event) (int64, error) {
	if event.Kind == model.EventServeSubmit && *tx.fail {
		*tx.fail = false
		return 0, errors.New("counting serve's submission failed")
	}
	return tx.Tx.AppendEvent(event)
}

// serve.submit_limit holds where counting a submission fails: the count
// is written with the pull request's record, and the pass stops at a
// submission that failed after its pull request opened. Written after it,
// a failed count let serve open two under a limit of one (#118; Codex's
// review of b2e7b616, its probe).
func TestServesDailyLimitSurvivesAFailedCount(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	where := Where{Environments: []model.Environment{{Provider: "command"}}}
	release := &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.8.1", CurrentVersion: "1.7.1"}, Version: "1.8.1", Forge: "github", Tag: "v1.8.1"}
	for _, name := range []string{"first-auto-update", "second-auto-update"} {
		prepared := e.ship(t.Context(), shipTarget{Port: "jq", Version: "1.8.1", Release: release, Name: name}, shipOptions{Origin: model.OriginServe, To: ShipCheck, Where: where, Answered: model.AnsweredNobody})
		require.Empty(t, prepared.Problem)
		require.NotNil(t, prepared.Run)
		run, err := e.Drive(t.Context(), session(t, e), prepared.Run.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State)
	}
	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	kept, fail := e.Store, true
	e.Store = serveCountFailure{kept, &fail}
	var said []string
	s := newServer(e, ServeOptions{SubmitLimit: 1, Now: e.now, Say: func(line string) { said = append(said, line) }})
	(&passingSubmitter{s: s}).maybe(t.Context())
	e.Store = kept
	require.Len(t, g.PRs, 1, "%q", said)
}

// findFailsOnce is a forge whose first Find after a pull request opened
// fails, as the readback after a lost create did.
type findFailsOnce struct {
	*forgetest.GitHub
	failed bool
}

func (g *findFailsOnce) Find(ctx context.Context, query forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	if !g.failed && len(g.PRs) > 0 {
		g.failed = true
		return forge.PullRequestObservation{}, errors.New("GitHub didn't answer")
	}
	return g.GitHub.Find(ctx, query)
}

// A serve submission whose create GitHub took, its reply lost and the
// readback failing too, has no pull request number, and stops the pass
// all the same: it continued, and opened a second over a limit of one
// (#118's remainder, architecture's check of 2026-11-10).
func TestServesDailyLimitSurvivesALostCreate(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	where := Where{Environments: []model.Environment{{Provider: "command"}}}
	release := &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.8.1", CurrentVersion: "1.7.1"}, Version: "1.8.1", Forge: "github", Tag: "v1.8.1"}
	for _, name := range []string{"first-auto-update", "second-auto-update"} {
		prepared := e.ship(t.Context(), shipTarget{Port: "jq", Version: "1.8.1", Release: release, Name: name}, shipOptions{Origin: model.OriginServe, To: ShipCheck, Where: where, Answered: model.AnsweredNobody})
		require.Empty(t, prepared.Problem)
		run, err := e.Drive(t.Context(), session(t, e), prepared.Run.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State)
	}
	g.CreateFails, g.Lost = true, true
	e.Forge = &findFailsOnce{GitHub: g}
	var said []string
	s := newServer(e, ServeOptions{SubmitLimit: 1, Now: e.now, Say: func(line string) { said = append(said, line) }})
	(&passingSubmitter{s: s}).maybe(t.Context())
	require.Len(t, g.PRs, 1, "%q", said)
}
