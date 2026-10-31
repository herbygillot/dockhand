package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
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
