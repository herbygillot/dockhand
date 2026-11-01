package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/fetch"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// Moves reads where a repository moved from GitHub's web page for it,
// which answers a renamed or transferred repository with a 301 naming the
// new one, as GitHub documents, outside the API's limits and its pacer.
// GitHub's git serves a moved repository under its old name with no word
// of the move, so a repository whose tags came from git is asked here: a
// look's 416 repositories were asked in 26.6 s, eight at once, 20 of them
// moved, with no throttling (the web redirect's look, 2026-11-01). The page
// is no API contract, so whatever it doesn't answer as expected is left
// unsaid, and a 429 or a refusal pauses the reads; nothing waits on it but
// the note it gives. Each answer is kept seven days, a 200's and a 301's
// alike, in the process and in a file where one is given, so a daily look
// asks about sixty and serve's looks no more.
type Moves struct {
	// BaseURL is GitHub's web, https://github.com where empty; a test
	// points it at a stub site.
	BaseURL string
	// HTTP sends the reads, which follow no redirect whatever it says;
	// dockhand's own transport where nil.
	HTTP *http.Client

	path string
	// now is the clock, time.Now where nil.
	now func() time.Time

	loaded sync.Once
	slots  chan struct{}
	mu     sync.Mutex
	kept   map[string]keptMove
	reads  map[string]*moveRead
	// pausedUntil is when the reads may start again after GitHub
	// throttled or refused one.
	pausedUntil time.Time
}

// keptMove is one repository's answer: the owner/name its page redirected
// to, empty where it answered 200, and when it was read.
type keptMove struct {
	To string    `json:"to,omitempty"`
	At time.Time `json:"at"`
}

// moveRead is one repository's read, in flight or answered: done closes
// once it is, and known says whether it answered.
type moveRead struct {
	done  chan struct{}
	to    string
	known bool
}

const (
	// movesAtOnce is how many pages are read at once, as the look read
	// them.
	movesAtOnce = 8
	// moveKeptFor is how long a page's answer stands before it's asked
	// again.
	moveKeptFor = 7 * 24 * time.Hour
	// movesPausedFor is how long a 429 or a refusal stops the reads: the
	// rest of the look it met, and not serve's next one.
	movesPausedFor = time.Hour
	// moveReadWait bounds one read; the look's slowest took 0.86 s.
	moveReadWait = 15 * time.Second
)

// NewMoves keeps the answers in the process, and in the file at path where
// it isn't empty.
func NewMoves(path string) *Moves {
	return &Moves{path: path}
}

func (m *Moves) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// load reads the kept answers once, a file that can't be read keeping
// none.
func (m *Moves) load() {
	m.loaded.Do(func() {
		m.slots = make(chan struct{}, movesAtOnce)
		m.reads = map[string]*moveRead{}
		m.kept = m.readKept()
	})
}

func (m *Moves) readKept() map[string]keptMove {
	kept := map[string]keptMove{}
	if m.path == "" {
		return kept
	}
	data, err := os.ReadFile(m.path)
	if err != nil || json.Unmarshal(data, &kept) != nil {
		return map[string]keptMove{}
	}
	return kept
}

// start is the read of a repository's page: its kept answer where it was
// read within seven days, the read already started, or a new one, begun
// now so it overlaps the rest of the repository's look. A read that can't
// be made, as while the reads are paused, is answered unknown at once.
func (m *Moves) start(ctx context.Context, name string) *moveRead {
	m.load()
	key := strings.ToLower(name)
	now := m.clock()
	m.mu.Lock()
	defer m.mu.Unlock()
	read := &moveRead{done: make(chan struct{})}
	if kept, ok := m.kept[key]; ok && !kept.At.After(now) && now.Sub(kept.At) < moveKeptFor {
		read.to, read.known = kept.To, true
		close(read.done)
		return read
	}
	if inFlight, ok := m.reads[key]; ok {
		return inFlight
	}
	if now.Before(m.pausedUntil) {
		close(read.done)
		return read
	}
	m.reads[key] = read
	go m.read(ctx, name, key, read)
	return read
}

// read asks for the repository's page, in its turn among the reads. Its
// answer is kept; one it didn't get is asked again by the next look.
func (m *Moves) read(ctx context.Context, name, key string, read *moveRead) {
	defer close(read.done)
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.reads, key)
	}()
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		return
	}
	m.mu.Lock()
	paused := m.clock().Before(m.pausedUntil)
	m.mu.Unlock()
	if paused {
		return
	}
	to, err := m.ask(ctx, name)
	switch {
	case err == nil:
		read.to, read.known = to, true
		m.keep(key, keptMove{To: to, At: m.clock()})
	case errors.Is(err, errUnreadMove) || ctx.Err() != nil:
		// A redirect naming no repository, or a look cut short, is no
		// refusal; it's left unsaid.
	default:
		m.mu.Lock()
		m.pausedUntil = m.clock().Add(movesPausedFor)
		m.mu.Unlock()
	}
}

// errUnreadMove is a 301 whose Location names no repository of GitHub's.
var errUnreadMove = errors.New("github: the page's redirect names no repository")

// ask sends a HEAD for the repository's page, unauthenticated, following
// no redirect: its 200 is the repository where it was, and its 301 names
// the owner/name it moved to. Any other answer is a refusal.
func (m *Moves) ask(ctx context.Context, name string) (string, error) {
	base, err := url.Parse(strings.TrimSuffix(m.base(), "/"))
	if err != nil {
		return "", err
	}
	page := base.JoinPath(name)
	ctx, cancel := context.WithTimeout(ctx, moveReadWait)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, page.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	response, err := m.client().Do(request)
	if err != nil {
		return "", err
	}
	_ = response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		return "", nil
	case http.StatusMovedPermanently:
		location, err := page.Parse(response.Header.Get("Location"))
		if err != nil || !strings.EqualFold(location.Host, base.Host) {
			return "", errUnreadMove
		}
		moved := strings.Trim(strings.TrimPrefix(location.Path, base.Path), "/")
		if !githubapi.ValidRepositoryName(moved) {
			return "", errUnreadMove
		}
		return moved, nil
	}
	return "", errors.New("github: the repository's page answered " + response.Status)
}

func (m *Moves) base() string {
	if m.BaseURL != "" {
		return m.BaseURL
	}
	return publicInstance
}

// client is the reads' HTTP, made to follow no redirect.
func (m *Moves) client() *http.Client {
	client := http.Client{Transport: fetch.Transport}
	if m.HTTP != nil {
		client = *m.HTTP
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

// keep notes an answer, and writes the answers to the file over what
// another process wrote there meanwhile, the newer answer for each
// repository standing. A file that won't take them leaves them kept for
// the process.
func (m *Moves) keep(key string, answer keptMove) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kept[key] = answer
	if m.path == "" {
		return
	}
	for name, theirs := range m.readKept() {
		if ours, ok := m.kept[name]; !ok || theirs.At.After(ours.At) {
			m.kept[name] = theirs
		}
	}
	data, err := json.Marshal(m.kept)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(m.path), 0o700) == nil {
		_ = atomicfile.Write(m.path, data, 0o600)
	}
}

// wait is the read's answer: the owner/name the repository moved to, empty
// where it answered as itself, and false where it didn't answer, or ctx
// ended first.
func (r *moveRead) wait(ctx context.Context) (string, bool) {
	select {
	case <-r.done:
		return r.to, r.known
	case <-ctx.Done():
		return "", false
	}
}
