package courtesy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is time under the test's control: Sleep records what it
// was asked for and advances the clock by it, so a spacing measured in
// hundreds of milliseconds is measured in nothing at all.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	if d > 0 {
		c.now = c.now.Add(d)
	}
	return nil
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// The pacer's first promise: requests to one host are spaced by the
// interval, and the first one is not made to wait for a host nobody
// has asked yet.
func TestPacerSpacesRequestsToOneHost(t *testing.T) {
	clock := newFakeClock()
	p := NewPacer(Policy{Interval: time.Second, Ceiling: 4}, clock)
	ctx := context.Background()

	for range 4 {
		require.NoError(t, p.Ask(ctx, "github.com", func(context.Context) error { return nil }))
	}
	assert.Equal(t, []time.Duration{0, time.Second, time.Second, time.Second}, clock.waits(),
		"the first ask is free and every one after it waits out the interval")
}

// Budgets are per host, which is the whole design: a sweep that
// touches a thousand forges is polite at any rate, and a sweep that
// touches one nine hundred times is the one that needs bounding.
func TestPacerBudgetsArePerHost(t *testing.T) {
	clock := newFakeClock()
	p := NewPacer(Policy{Interval: time.Second, Ceiling: 4}, clock)
	ctx := context.Background()

	require.NoError(t, p.Ask(ctx, "github.com", func(context.Context) error { return nil }))
	require.NoError(t, p.Ask(ctx, "codeberg.org", func(context.Context) error { return nil }))
	require.NoError(t, p.Ask(ctx, "git.sr.ht", func(context.Context) error { return nil }))
	assert.Equal(t, []time.Duration{0, 0, 0}, clock.waits(),
		"three hosts are three budgets; none of them waits for the others")
}

// Jitter adds to the gap and never subtracts from it. A perfectly
// regular request train is itself a signature, and a jitter that could
// shorten the interval would be a politeness setting that made the
// tool less polite.
func TestPacerJitterOnlyLengthens(t *testing.T) {
	clock := newFakeClock()
	p := NewPacer(Policy{Interval: time.Second, Jitter: 0.5, Ceiling: 4}, clock)
	p.rnd = func() float64 { return 1 } // the top of the jitter range
	ctx := context.Background()

	for range 3 {
		require.NoError(t, p.Ask(ctx, "h", func(context.Context) error { return nil }))
	}
	assert.Equal(t, []time.Duration{0, 1500 * time.Millisecond, 1500 * time.Millisecond}, clock.waits())

	clock2 := newFakeClock()
	p2 := NewPacer(Policy{Interval: time.Second, Jitter: 0.5, Ceiling: 4}, clock2)
	p2.rnd = func() float64 { return 0 } // the bottom
	for range 2 {
		require.NoError(t, p2.Ask(ctx, "h", func(context.Context) error { return nil }))
	}
	assert.Equal(t, []time.Duration{0, time.Second}, clock2.waits(),
		"the bottom of the jitter range is the interval itself, never less")
}

// The ceiling is the second bound and it is not the same as the
// interval: the interval limits the rate and the ceiling limits the
// burst. Real concurrency here, because that is the property.
func TestPacerCeilingBoundsConcurrency(t *testing.T) {
	p := NewPacer(Policy{Ceiling: 2}, nil)
	ctx := context.Background()

	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Ask(ctx, "h", func(context.Context) error {
				entered <- struct{}{}
				<-release
				return nil
			})
		}()
	}
	<-entered
	<-entered
	select {
	case <-entered:
		t.Fatal("a third request was in flight to one host under a ceiling of two")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	assert.Len(t, entered, 3, "the other three ran once the first two finished")
}

// A host that refuses dockhand is left alone, and every other port on
// that host is refused with it. That is the part no single call site
// can do for itself and the reason the wall lives here.
func TestPacerWallRefusesEveryPortOnTheHost(t *testing.T) {
	clock := newFakeClock()
	p := NewPacer(Policy{Ceiling: 2, Backoff: 15 * time.Minute}, clock)
	ctx := context.Background()

	p.Wall("api.github.com", errors.New("API rate limit exceeded"))

	asked := 0
	err := p.Ask(ctx, "api.github.com", func(context.Context) error { asked++; return nil })
	require.Error(t, err)
	assert.Zero(t, asked, "the request the wall exists to prevent was made anyway")
	require.ErrorIs(t, err, ErrWalled)

	var walled *WalledError
	require.ErrorAs(t, err, &walled)
	assert.Equal(t, "api.github.com", walled.Host)
	assert.Equal(t, 15*time.Minute, walled.Retry)
	require.ErrorContains(t, err, "API rate limit exceeded",
		"the one refusal that raised the wall is quoted by every port behind it")

	// A different host is untouched.
	require.NoError(t, p.Ask(ctx, "codeberg.org", func(context.Context) error { return nil }))

	// And the wall comes down on its own.
	left, up := p.Walled("api.github.com")
	assert.True(t, up)
	assert.Equal(t, 15*time.Minute, left)
	clock.advance(16 * time.Minute)
	_, up = p.Walled("api.github.com")
	assert.False(t, up)
	require.NoError(t, p.Ask(ctx, "api.github.com", func(context.Context) error { asked++; return nil }))
	assert.Equal(t, 1, asked)
}

// A zero backoff raises no wall. It is the right default for a test
// and the wrong one for anything that talks to a real forge, which is
// why Default sets it.
func TestPacerZeroBackoffRaisesNoWall(t *testing.T) {
	p := NewPacer(Policy{Ceiling: 1}, nil)
	p.Wall("h", errors.New("429"))
	_, up := p.Walled("h")
	assert.False(t, up)
	assert.NoError(t, p.Ask(context.Background(), "h", func(context.Context) error { return nil }))
}

// An interrupted sweep must not sit out its pacing. The context is
// checked where the waiting happens, so a Ctrl-C during a paced sweep
// stops at the next request rather than after the last interval.
func TestPacerStopsOnACancelledContext(t *testing.T) {
	p := NewPacer(Policy{Interval: time.Hour, Ceiling: 1}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	asked := 0
	err := p.Ask(ctx, "h", func(context.Context) error { asked++; return nil })
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, asked)
}

// The defaults are the ruling, so they are pinned: changing them is a
// decision somebody makes on purpose rather than a diff nobody reads.
func TestDefaultPolicyIsThePolitenessRuling(t *testing.T) {
	assert.Equal(t, 500*time.Millisecond, Default.Interval)
	assert.Equal(t, 2, Default.Ceiling)
	assert.Positive(t, Default.Jitter)
	assert.Equal(t, 15*time.Minute, Default.Backoff)
}

func TestHostIsTheBudgetKey(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/foo/bar":       "github.com",
		"https://GitHub.com/foo/bar.git":   "github.com",
		"http://git.example.org:3000/a/b":  "git.example.org:3000",
		"https://git.sr.ht/~sircmpwn/aerc": "git.sr.ht",
		"not a url":                        "not a url",
	} {
		assert.Equal(t, want, Host(raw), raw)
	}
}

func TestUserAgentNamesDockhand(t *testing.T) {
	assert.Equal(t, "dockhand/1.2.3 (+https://github.com/herbygillot/dockhand)", UserAgent("1.2.3"))
	assert.Contains(t, UserAgent(""), "dockhand/dev",
		"an unknown version says so rather than pretending to a release")
}
