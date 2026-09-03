package courtesy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache stores what a host said, so that a sweep asks it once rather
// than once per port and a rerun an hour later asks it not at all.
//
// # This is not the decline memo, and the difference is the design
//
// The memo caches JUDGMENTS and is addressed by CONTENT. Its key is
// the whole of its input — the Portfile blob, the intent, the resolved
// input, the environment digest — so it has no TTL, no revalidation
// and no notion of freshness. A stale memo is not a wrong answer; it
// is a different key, and it misses. That is why a Portfile edited to
// fix the very thing that was declined re-arms the moment it is saved.
//
// This cache answers a different question: what did this forge say
// about these tags a few minutes ago. Upstream is a moving target no
// key can pin — the bytes that would have to change to invalidate the
// answer are on somebody else's server — so it is bounded by time
// instead: a TTL, and a validator so that revalidating costs a 304
// rather than a body. An observation here IS allowed to go stale, and
// bounding by how much is its whole design.
//
// Neither may borrow the other's key. A judgment given a TTL would be
// re-derived for nothing while its inputs sat still, and — worse —
// would keep answering for a Portfile that changed inside the window.
// An observation given a content key would be immortal: "ls-remote
// said 1.2.3" would still be the answer a year after 1.3.0 shipped,
// because nothing in the port's bytes changed to invalidate it.
//
// They meet in exactly one place, ordering, and the rule there is
// about the memo's key: it may be consulted once every component of
// that key is settled — after upstream resolution, for the one verb
// that resolves — and not before. A sweep therefore pays this cache's
// paced, revalidating cost on every port, and the planner's cost only
// on the ports the memo misses.
//
// # Custody
//
// Entries live as one small JSON file each under the user's cache
// directory, which is the directory an operating system is allowed to
// empty and a user is allowed to delete. Nothing here is state: losing
// the whole tree costs a sweep its round trips and nothing else, which
// is why every read and write error below is swallowed into a miss
// rather than raised. A cache that could fail a run would be a
// liability holding an asset's place.
type Cache struct {
	dir   string
	ttl   time.Duration
	clock Clock

	// mu guards flight, which holds the calls that are happening right
	// now, one per key. See Do.
	mu     sync.Mutex
	flight map[string]*flight
}

// flight is one key's round trip, in progress, and what it came back
// with. Followers wait on done and read the rest.
type flight struct {
	done chan struct{}
	ans  Answer
	err  error
}

// Answer is what a Transport came back with.
type Answer struct {
	// NotModified says the host was asked and answered that nothing
	// has changed since the validator was issued. The stored body
	// stands and its clock is refreshed.
	NotModified bool
	// Validator identifies this version of the answer: an HTTP ETag,
	// or a digest the transport computed over what it read. It is
	// handed back to the transport on the next revalidation.
	Validator string
	// Body is the observation, as JSON. JSON rather than opaque bytes
	// so that a cache file can be read by a person debugging a sweep,
	// which is most of what this directory is for once it works.
	Body json.RawMessage
}

// Transport is one witness's round trip. validator is what the cache
// holds for this key, empty when it holds nothing; a transport that
// can revalidate compares it and answers NotModified, and one that
// cannot ignores it and answers with a body.
type Transport func(ctx context.Context, validator string) (Answer, error)

// Source says where an observation came from, which is the sweep's
// request budget as it is actually spent.
type Source int

const (
	// Fresh: served from the cache inside its TTL. The host was not
	// asked at all, and this is the only source that costs nothing.
	Fresh Source = iota
	// Revalidated: the host was asked and said nothing had changed.
	// For an HTTP witness that is a 304 and costs no body; for the git
	// witness there is no conditional request, so it costs a whole
	// ls-remote and what it buys is downstream — an unchanged digest
	// means the releases observation keyed on it is still good.
	Revalidated
	// Fetched: the host was asked and answered with a new body. The
	// expensive one.
	Fetched
	// Uncached: there is no cache, so the host was asked. Told apart
	// from Fetched so that a --no-cache run's budget does not read as
	// a cache that is missing everything.
	Uncached
	// Shared: another port was already asking this exact question, so
	// this one waited for that answer instead of asking again. It costs
	// nothing, and it is told apart from Fresh because the two are
	// different savings: Fresh is an observation held from an earlier
	// run, and this is the round trip a concurrent worker was already
	// paying. A --no-cache sweep has no Fresh rows and plenty of these.
	Shared
)

func (s Source) String() string {
	switch s {
	case Fresh:
		return "fresh"
	case Revalidated:
		return "revalidated"
	case Fetched:
		return "fetched"
	case Uncached:
		return "uncached"
	case Shared:
		return "shared"
	}
	return "unknown source"
}

// Asked reports whether this source cost a round trip to the host.
func (s Source) Asked() bool {
	switch s {
	case Fresh, Shared:
		return false
	case Revalidated, Fetched, Uncached:
		return true
	}
	return true
}

// ErrNoBaseline reports a transport that answered "not modified" when
// the cache held nothing to which it could refer. It is a bug in the
// transport rather than anything about the host, and it is raised
// rather than papered over because the alternative — returning an
// empty observation — would read as a forge with no tags.
var ErrNoBaseline = errors.New("courtesy: transport revalidated against nothing")

// Dir is where observations live: the user's cache directory, under
// dockhand's own name. It is created on first write, not here.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("courtesy: no user cache directory: %w", err)
	}
	return filepath.Join(base, "dockhand", "upstream"), nil
}

// NewCache builds a cache over a directory and a TTL. An empty dir or
// a non-positive TTL is a cache that stores nothing and asks every
// time — which is what --no-cache wants, and what a test that is
// measuring the transport wants, without either of them needing a
// second code path.
func NewCache(dir string, ttl time.Duration, clock Clock) *Cache {
	if clock == nil {
		clock = RealClock{}
	}
	return &Cache{dir: dir, ttl: ttl, clock: clock}
}

// Do returns the observation for key, asking tr only when what is
// stored is missing or past its TTL.
//
// The key is the caller's to compose and is never parsed here. What
// belongs in it is everything that changes the answer: which witness,
// which repository, and — for an observation derived from another —
// the validator of the one it was derived from. What does NOT belong
// in it is anything about the port's own bytes; that is the memo's
// key, and mixing the two is how a cache becomes immortal.
// A key that is already being asked about is not asked about twice.
// The forge observation is keyed by repository and nothing else, so two
// subports of one Portfile — which arrive adjacent in a selector's
// target list and land on different workers at the same moment — would
// otherwise each pay a round trip for the same answer. On this tree
// that is roughly nine hundred duplicate requests to github.com on a
// full sweep, and it is the saving both this file and outdated's own
// comments already promise a reader.
//
// The follower takes the leader's answer, with one exception: a leader
// whose CONTEXT died was interrupted, and the interruption belongs to
// that port's three minutes rather than to whoever joined it. A
// follower still under a live context leads its own call instead, which
// is the only way one port's timeout does not become a failure row for
// another's.
func (c *Cache) Do(ctx context.Context, key string, tr Transport) (Answer, Source, error) {
	if c == nil {
		// No cache object at all: nowhere to hold an in-flight call
		// either, so this is the plain uncached road.
		return uncached(ctx, tr)
	}
	for {
		c.mu.Lock()
		if c.flight == nil {
			c.flight = map[string]*flight{}
		}
		if fl, joined := c.flight[key]; joined {
			c.mu.Unlock()
			select {
			case <-fl.done:
			case <-ctx.Done():
				return Answer{}, Shared, ctx.Err()
			}
			if interrupted(fl.err) && ctx.Err() == nil {
				continue
			}
			return fl.ans, Shared, fl.err
		}
		fl := &flight{done: make(chan struct{})}
		c.flight[key] = fl
		c.mu.Unlock()

		ans, src, err := c.ask(ctx, key, tr)
		fl.ans, fl.err = ans, err
		// Out of the map before the wake-up, so a follower that wakes
		// and decides to lead its own call cannot rejoin this finished
		// one and wait on it forever.
		c.mu.Lock()
		delete(c.flight, key)
		c.mu.Unlock()
		close(fl.done)
		return ans, src, err
	}
}

// interrupted reports an error that is this call's context ending
// rather than anything a host said.
func interrupted(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// uncached is the road for a cache that stores nothing: ask, every
// time.
func uncached(ctx context.Context, tr Transport) (Answer, Source, error) {
	f, err := tr(ctx, "")
	if err != nil {
		return Answer{}, Uncached, err
	}
	if f.NotModified {
		return Answer{}, Uncached, ErrNoBaseline
	}
	return f, Uncached, nil
}

// ask is one leader's whole call: consult what is stored, ask the host
// when it is missing or stale, and keep what came back.
func (c *Cache) ask(ctx context.Context, key string, tr Transport) (Answer, Source, error) {
	if c.dir == "" || c.ttl <= 0 {
		return uncached(ctx, tr)
	}

	stored, held := c.load(key)
	if held && c.clock.Now().Sub(stored.Observed) < c.ttl {
		return Answer{Validator: stored.Validator, Body: stored.Body}, Fresh, nil
	}

	f, err := tr(ctx, stored.Validator)
	if err != nil {
		return Answer{}, Fetched, err
	}
	if f.NotModified {
		if !held {
			return Answer{}, Fetched, ErrNoBaseline
		}
		stored.Observed = c.clock.Now()
		c.store(stored)
		return Answer{Validator: stored.Validator, Body: stored.Body}, Revalidated, nil
	}
	c.store(entry{Key: key, Observed: c.clock.Now(), Validator: f.Validator, Body: f.Body})
	return f, Fetched, nil
}

// entry is one stored observation. Key rides along so that a person
// looking at a hashed filename can tell what it is about.
type entry struct {
	Key       string          `json:"key"`
	Observed  time.Time       `json:"observed"`
	Validator string          `json:"validator,omitempty"`
	Body      json.RawMessage `json:"body"`
}

// path is where a key's entry lives: sharded one level by the first
// byte of the hash, because a tree with a thousand ports on it will
// hold a few thousand files and a single flat directory of those is
// slow to list on exactly the machines this runs on.
func (c *Cache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, h[:2], h+".json")
}

// load reads an entry, treating every failure as a miss. A truncated
// file from a killed run, a file written by a future dockhand, a
// directory that is not readable: none of them is worth failing a
// sweep over, and all of them are fixed by asking the host.
func (c *Cache) load(key string) (entry, bool) {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return entry{}, false
	}
	var e entry
	if json.Unmarshal(b, &e) != nil || e.Key != key || len(e.Body) == 0 {
		return entry{}, false
	}
	return e, true
}

// store writes an entry, best-effort. The write is to a temporary file
// in the same directory and then a rename, so a killed run leaves
// either the old entry or the new one and never half of either — a
// half-written entry would fail to parse and be a miss, which is
// harmless, but the temporary file it left would not be cleaned up by
// anyone.
func (c *Cache) store(e entry) {
	if len(e.Body) > 0 && !json.Valid(e.Body) {
		// A transport that returns something other than JSON gets no
		// caching rather than a corrupt entry. Said out loud, because
		// the symptom is a witness that is silently never cached.
		slog.Debug("observation not cached: body is not JSON", "key", e.Key)
		return
	}
	p := c.path(e.Key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		slog.Debug("observation not cached", "key", e.Key, "err", err)
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		slog.Debug("observation not cached", "key", e.Key, "err", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		slog.Debug("observation not cached", "key", e.Key, "err", err)
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()         //nolint:errcheck,gosec // the write already failed; the file is about to be removed
		_ = os.Remove(name) //nolint:errcheck // the write already failed; the temporary file is being cleaned up and its removal changes nothing
		slog.Debug("observation not cached", "key", e.Key, "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name) //nolint:errcheck // the write already failed; the temporary file is being cleaned up and its removal changes nothing
		slog.Debug("observation not cached", "key", e.Key, "err", err)
		return
	}
	if err := os.Rename(name, p); err != nil {
		_ = os.Remove(name) //nolint:errcheck // the write already failed; the temporary file is being cleaned up and its removal changes nothing
		slog.Debug("observation not cached", "key", e.Key, "err", err)
	}
}

// Prune removes observations older than maxAge, and returns how many
// went.
//
// Entries expire by their TTL but their files do not, so a tree swept
// for a year would accumulate one file per repository forever. Called
// after a sweep that actually fetched something, a walk over a few
// thousand tiny files costs nothing next to the round trips that just
// happened — and it is only ever worth doing then, which is why it is
// the caller's to schedule rather than something this package does on
// its own timer.
func (c *Cache) Prune(maxAge time.Duration) int {
	if c == nil || c.dir == "" || maxAge <= 0 {
		return 0
	}
	cutoff := c.clock.Now().Add(-maxAge)
	removed := 0
	// Errors are ignored throughout for the reason the rest of this
	// file ignores them: housekeeping that can fail a run is worse
	// than housekeeping that does not happen.
	_ = filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".json" {
			return nil //nolint:nilerr // an unreadable corner of a cache is not a reason to stop
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil //nolint:nilerr // same
		}
		if os.Remove(p) == nil {
			removed++
		}
		return nil
	})
	return removed
}
