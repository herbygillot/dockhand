package courtesy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTransport is a host under the test's control: it counts what it
// was asked, remembers what validator it was handed, and answers
// whatever the test set it to answer.
type fakeTransport struct {
	mu sync.Mutex
	// calls is how many round trips the cache made.
	calls int
	// sawValidator is what the cache handed over on the last call —
	// the ETag it is revalidating against.
	sawValidator string
	// answer is what the host says. A nil answer says "not modified".
	body      string
	validator string
	err       error
}

func (f *fakeTransport) fn(_ context.Context, validator string) (Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.sawValidator = validator
	if f.err != nil {
		return Answer{}, f.err
	}
	if f.body == "" {
		return Answer{NotModified: true, Validator: validator}, nil
	}
	return Answer{Validator: f.validator, Body: json.RawMessage(f.body)}, nil
}

func newCache(t *testing.T, ttl time.Duration) (*Cache, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	return NewCache(t.TempDir(), ttl, clock), clock
}

// The cache's whole reason to exist: a second ask inside the TTL costs
// no round trip at all. On a sweep this is what turns two subports of
// one Portfile, or a rerun ten minutes later, into one question.
func TestCacheServesAFreshObservationWithoutAsking(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	host := &fakeTransport{body: `["1.2.0"]`, validator: `W/"abc"`}
	ctx := context.Background()

	got, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src)
	assert.JSONEq(t, `["1.2.0"]`, string(got.Body))

	got, src, err = c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fresh, src)
	assert.JSONEq(t, `["1.2.0"]`, string(got.Body))
	assert.Equal(t, 1, host.calls, "the host was asked twice for one observation")
	assert.False(t, Fresh.Asked(), "a fresh observation is the only source that costs nothing")
}

// Past the TTL the host is asked again, and it is handed the validator
// so that an unchanged answer costs a 304 rather than a body. The
// stored body stands and its clock is refreshed.
func TestCacheRevalidatesWithTheStoredValidator(t *testing.T) {
	c, clock := newCache(t, time.Hour)
	host := &fakeTransport{body: `["1.2.0"]`, validator: `W/"abc"`}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	clock.advance(2 * time.Hour)

	host.body = "" // the host says nothing has changed
	got, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Revalidated, src)
	assert.Equal(t, `W/"abc"`, host.sawValidator, "the cache revalidated against nothing")
	assert.JSONEq(t, `["1.2.0"]`, string(got.Body), "a 304 keeps the body it already had")

	// And the refresh restarted the clock: the next ask inside the TTL
	// is free again.
	_, src, err = c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fresh, src)
	assert.Equal(t, 2, host.calls)
}

// A changed answer replaces the stored one, validator and all.
func TestCacheStoresAChangedAnswer(t *testing.T) {
	c, clock := newCache(t, time.Hour)
	host := &fakeTransport{body: `["1.2.0"]`, validator: `W/"abc"`}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	clock.advance(2 * time.Hour)
	host.body, host.validator = `["1.3.0"]`, `W/"def"`

	got, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src)
	assert.JSONEq(t, `["1.3.0"]`, string(got.Body))

	clock.advance(time.Minute)
	host.body = ""
	_, _, err = c.Do(ctx, "k", host.fn) // still fresh; no ask
	require.NoError(t, err)
	assert.Equal(t, 2, host.calls)
}

// Keys are separate observations. Nothing about this is clever, and it
// is asserted because the whole staging rests on two ports of one
// repository sharing a key while two repositories do not.
func TestCacheKeysAreSeparateObservations(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	host := &fakeTransport{body: `["1"]`, validator: "v"}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "one", host.fn)
	require.NoError(t, err)
	_, src, err := c.Do(ctx, "two", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src)
	assert.Equal(t, 2, host.calls)
}

// The TTL boundary is exclusive of itself: an observation exactly as
// old as the TTL is stale. Asserted because "less than" and "at most"
// are one character apart and the difference is a sweep that never
// revalidates.
func TestCacheTTLBoundaryIsStale(t *testing.T) {
	c, clock := newCache(t, time.Hour)
	host := &fakeTransport{body: `["1"]`, validator: "v"}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	clock.advance(time.Hour - time.Nanosecond)
	_, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fresh, src)

	clock.advance(time.Nanosecond)
	host.body = "" // the host will say nothing has changed
	_, src, err = c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Revalidated, src, "an observation exactly as old as the TTL is stale")
}

// A cache that cannot be read is a miss and never a failure. Losing
// the whole tree costs a sweep its round trips and nothing else, which
// is the promise that lets the user delete the directory whenever they
// like.
func TestCacheCorruptionIsAMiss(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock()
	c := NewCache(dir, time.Hour, clock)
	host := &fakeTransport{body: `["1"]`, validator: "v"}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)

	// Truncate every stored entry, as a killed run might have.
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.WriteFile(p, []byte(`{"key":"k","bod`), 0o600)
	}))

	got, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src, "a truncated entry must read as a miss, not as an error")
	assert.JSONEq(t, `["1"]`, string(got.Body))
	assert.Empty(t, host.sawValidator, "a miss has no validator to revalidate against")
}

// A host that will not answer is the caller's problem, not the
// cache's: the error travels and nothing is stored.
func TestCacheDoesNotStoreAFailedAsk(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	boom := errors.New("ls-remote: connection refused")
	host := &fakeTransport{err: boom}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "k", host.fn)
	require.ErrorIs(t, err, boom)

	host.err, host.body, host.validator = nil, `["1"]`, "v"
	_, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src, "the failure must not have been cached as an answer")
}

// A transport that revalidates against a cache holding nothing is a
// bug in the transport. Raised rather than papered over: returning an
// empty observation would read as a forge with no tags at all.
func TestCacheRefusesRevalidationWithNoBaseline(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	host := &fakeTransport{} // answers NotModified unconditionally
	_, _, err := c.Do(context.Background(), "k", host.fn)
	require.ErrorIs(t, err, ErrNoBaseline)
}

// An empty directory or a non-positive TTL is a cache that stores
// nothing and asks every time — what --no-cache wants, with no second
// code path to keep in step.
func TestCacheDisabledAsksEveryTime(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]*Cache{
		"no directory": NewCache("", time.Hour, newFakeClock()),
		"no ttl":       NewCache(t.TempDir(), 0, newFakeClock()),
		"nil":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			host := &fakeTransport{body: `["1"]`, validator: "v"}
			for range 3 {
				got, src, err := c.Do(ctx, "k", host.fn)
				require.NoError(t, err)
				assert.Equal(t, Uncached, src)
				assert.JSONEq(t, `["1"]`, string(got.Body))
			}
			assert.Equal(t, 3, host.calls)
		})
	}
}

// A transport that returns something other than JSON gets no caching
// rather than a corrupt entry: the observation is still correct, it
// just costs a round trip every time.
func TestCacheRefusesANonJSONBody(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, time.Hour, newFakeClock())
	host := &fakeTransport{body: "not json at all", validator: "v"}
	ctx := context.Background()

	got, src, err := c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src)
	assert.Equal(t, "not json at all", string(got.Body), "the answer still reaches the caller")

	_, src, err = c.Do(ctx, "k", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fetched, src, "nothing was stored, so the next ask is a miss")
}

// Entries expire by their TTL but their files do not, so a tree swept
// for a year would keep one file per repository forever.
func TestCachePruneRemovesWhatIsPastItsAge(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock()
	c := NewCache(dir, time.Hour, clock)
	host := &fakeTransport{body: `["1"]`, validator: "v"}
	ctx := context.Background()

	_, _, err := c.Do(ctx, "old", host.fn)
	require.NoError(t, err)

	// Age the file on disk, since Prune reads the filesystem's clock
	// and not the injected one.
	old := clock.Now().Add(-72 * time.Hour)
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.Chtimes(p, old, old)
	}))
	_, _, err = c.Do(ctx, "new", host.fn)
	require.NoError(t, err)

	assert.Equal(t, 1, c.Prune(48*time.Hour))
	_, src, err := c.Do(ctx, "new", host.fn)
	require.NoError(t, err)
	assert.Equal(t, Fresh, src, "prune took the entry that was still in use")

	assert.Zero(t, NewCache("", time.Hour, clock).Prune(time.Hour))
	assert.Zero(t, c.Prune(0))
}

func TestSourceNames(t *testing.T) {
	assert.Equal(t, "fresh", Fresh.String())
	assert.Equal(t, "revalidated", Revalidated.String())
	assert.Equal(t, "fetched", Fetched.String())
	assert.Equal(t, "uncached", Uncached.String())
	assert.Equal(t, "unknown source", Source(99).String())
	for _, s := range []Source{Revalidated, Fetched, Uncached} {
		assert.True(t, s.Asked(), s.String())
	}
}

// Dir is the user's cache directory, which is the directory an
// operating system is allowed to empty.
func TestDirIsUnderTheUserCacheDir(t *testing.T) {
	got, err := Dir()
	require.NoError(t, err)
	base, err := os.UserCacheDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "dockhand", "upstream"), got)
}

// Two workers that want the same observation at the same moment cost
// one round trip, not two.
//
// This is the saving the cache's own comment promises and the one a
// sweep needs most: the forge observation is keyed by repository, so
// two subports of one Portfile — adjacent in a selector's target list
// and therefore concurrent on two workers — are one question. Without
// it a full sweep of this tree pays roughly nine hundred duplicate
// requests to one host.
func TestCacheCollapsesConcurrentMissesOnOneKey(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls int
	var mu sync.Mutex
	slow := func(_ context.Context, _ string) (Answer, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		entered <- struct{}{}
		<-release
		return Answer{Validator: `W/"one"`, Body: json.RawMessage(`["1.2.0"]`)}, nil
	}

	ctx := context.Background()
	type result struct {
		src Source
		err error
	}
	out := make(chan result, 2)
	go func() {
		_, src, err := c.Do(ctx, "tags\x00repo", slow)
		out <- result{src, err}
	}()
	<-entered // the leader is inside the transport
	go func() {
		_, src, err := c.Do(ctx, "tags\x00repo", slow)
		out <- result{src, err}
	}()
	// The follower must be waiting on the leader rather than entering
	// the transport: nothing else may arrive on entered.
	select {
	case <-entered:
		t.Fatal("the second worker ran the transport instead of joining the first")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	var sources []Source
	for range 2 {
		r := <-out
		require.NoError(t, r.err)
		sources = append(sources, r.src)
	}
	mu.Lock()
	assert.Equal(t, 1, calls, "one round trip served both workers")
	mu.Unlock()
	assert.Contains(t, sources, Fetched)
	assert.Contains(t, sources, Shared)

	// The answer the follower got is the leader's, whole.
	ans, src, err := c.Do(ctx, "tags\x00repo", failingTransport(t))
	require.NoError(t, err)
	assert.Equal(t, Fresh, src, "the leader's answer was stored, so a later ask is free")
	assert.JSONEq(t, `["1.2.0"]`, string(ans.Body))
}

// A shared answer costs nothing, and the census has to read it that
// way: a follower that counted as a round trip would report a budget
// twice the size of the one actually spent.
func TestSharedIsNotARoundTrip(t *testing.T) {
	assert.False(t, Shared.Asked())
	assert.Equal(t, "shared", Shared.String())
}

// A leader whose context died was interrupted, and the interruption is
// that port's own three minutes running out. A follower still under a
// live context leads its own call rather than inheriting a failure
// that says nothing about it.
func TestCacheDoesNotSpreadOneWorkersCancellation(t *testing.T) {
	c, _ := newCache(t, time.Hour)
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var calls int
	tr := func(ctx context.Context, _ string) (Answer, error) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if !first {
			return Answer{Validator: `W/"two"`, Body: json.RawMessage(`["9.9"]`)}, nil
		}
		entered <- struct{}{}
		<-release
		return Answer{}, ctx.Err()
	}

	dying, kill := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := c.Do(dying, "tags\x00repo", tr)
		done <- err
	}()
	<-entered
	follower := make(chan Answer, 1)
	ferr := make(chan error, 1)
	go func() {
		ans, _, err := c.Do(context.Background(), "tags\x00repo", tr)
		follower <- ans
		ferr <- err
	}()
	kill()
	close(release)

	require.Error(t, <-done, "the interrupted worker keeps its own interruption")
	require.NoError(t, <-ferr, "the follower's context is alive, so it asks for itself")
	assert.JSONEq(t, `["9.9"]`, string((<-follower).Body))
}

// failingTransport fails the test if the cache asks the host at all.
func failingTransport(t *testing.T) Transport {
	t.Helper()
	return func(context.Context, string) (Answer, error) {
		t.Error("the host was asked when a stored observation should have answered")
		return Answer{}, errors.New("asked")
	}
}
