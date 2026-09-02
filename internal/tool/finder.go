package tool

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// ErrNotFound reports a tool the finder could not resolve. It sits
// mid-sentence in the text — "gh not found on PATH" — because that
// sentence is what doctor and the verbs that need gh already print,
// and the sentinel lets a caller branch on the fact without reading it.
var ErrNotFound = errors.New("not found")

// Finder resolves tools to executables, memoizing each success: PATH
// does not change under a run, and the memo is what makes "checked
// once by doctor, used everywhere" a single fact rather than nine
// independent lookups. Misses are never memoized — a tool installed
// mid-process is found, not remembered absent.
//
// One Finder is built per process at the composition root and handed
// to everything that execs; a test builds its own over a fake lookup.
// The memo is per Finder, so two finders are two independent views of
// the machine, which is exactly what a test wants and a run never has.
// The zero Finder is usable — it searches PATH and allocates its memo
// on the first hit — because the type is exported and a zero value
// that panics is a trap.
type Finder struct {
	lookup func(string) (string, error)
	mu     sync.Mutex
	memo   map[Tool]string
}

// NewFinder builds a finder over a search function of exec.LookPath's
// shape. A nil lookup is os/exec's own PATH search — the real thing,
// which is what the composition root wants and what no test does; a
// test hands in a function that answers for the tools it stubs and
// falls through to exec.LookPath for the ones it needs for real.
func NewFinder(lookup func(name string) (string, error)) *Finder {
	return &Finder{lookup: lookup}
}

// Find resolves a tool to its executable path. The lookup runs under
// the finder's lock, so concurrent callers asking for the same tool
// get one lookup and one answer.
func (f *Finder) Find(t Tool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.memo[t]; ok {
		return p, nil
	}
	path, err := f.search(string(t))
	if err != nil {
		return "", notFound(t)
	}
	f.remember(t, path)
	return path, nil
}

// search runs the finder's lookup; with none set it is PATH.
func (f *Finder) search(name string) (string, error) {
	if f.lookup == nil {
		return exec.LookPath(name)
	}
	return f.lookup(name)
}

// remember records a hit. The caller holds the lock; the first hit
// allocates the memo.
func (f *Finder) remember(t Tool, path string) {
	if f.memo == nil {
		f.memo = map[Tool]string{}
	}
	f.memo[t] = path
}

// FindWith resolves a tool, trying an explicit fallback path when the
// PATH search misses — doctor's port-tclsh probe falls back to the
// conventional prefix location this way. A fallback that hits is
// memoized under the tool, so a later Find answers with it too.
func (f *Finder) FindWith(t Tool, fallback string) (string, error) {
	if path, err := f.Find(t); err == nil {
		return path, nil
	}
	if fallback != "" {
		f.mu.Lock()
		defer f.mu.Unlock()
		if path, err := f.search(fallback); err == nil {
			f.remember(t, path)
			return path, nil
		}
	}
	return "", notFound(t)
}

// Have reports availability without caring where.
func (f *Finder) Have(t Tool) bool {
	_, err := f.Find(t)
	return err == nil
}

// notFound words a miss for the way the tool was named: a bare name
// was searched for on PATH, a path (Tar) was checked where it says.
func notFound(t Tool) error {
	if strings.ContainsRune(string(t), '/') {
		return fmt.Errorf("%s %w", t, ErrNotFound)
	}
	return fmt.Errorf("%s %w on PATH", t, ErrNotFound)
}
