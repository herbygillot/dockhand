// Package sweep is the fault sweep (docs/designs/fault-sweep.md, #129):
// each workflow run once to find its outside effects, then once for each
// effect and each way it can fail, recovered as a person would, and the
// world compared with the clean run's. It sits above compose, at the
// command line, in process, and reaches the engine through compose's two
// seams, engine.Options.WrapStore and GitHook.
package sweep

import (
	"context"
	"runtime"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// An effect is one call at a boundary: its boundary, as "store.update" or
// "git push"; its call site, the first caller outside the boundary's own
// package; and which call of that site it is in the run. Its address is
// the three, never its place in the sequence, which a heartbeat's write
// can shift.
type effect struct {
	Boundary, Site string
	Occurrence     int
	// Detail is what it acted on, for a failure's words: the git
	// command's arguments.
	Detail string
}

// plan is a run's log of effects, shared by every boundary so they make
// one sequence, and safe to write from several goroutines: a check's
// driver and a session's heartbeat write while the command does.
type plan struct {
	mu     sync.Mutex
	log    []effect
	counts map[string]int
}

func newPlan() *plan { return &plan{counts: map[string]int{}} }

// record logs an effect at the boundary, addressed by its call site.
func (p *plan) record(boundary, detail string) effect {
	site := callSite()
	p.mu.Lock()
	defer p.mu.Unlock()
	key := boundary + "@" + site
	p.counts[key]++
	e := effect{Boundary: boundary, Site: site, Occurrence: p.counts[key], Detail: detail}
	p.log = append(p.log, e)
	return e
}

// effects are the run's effects so far.
func (p *plan) effects() []effect {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]effect(nil), p.log...)
}

// boundaries are the packages a call site is never in: the boundaries'
// own, and the sweep's.
var boundaries = []string{"/internal/store.", "/internal/store/", "/internal/git.", "/internal/command/sweep.", "runtime."}

// callSite is the first caller outside the boundaries, as its package's
// last element and its function: "engine.(*Engine).ApplySubmit".
func callSite() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		name := frame.Function
		outside := !strings.HasPrefix(name, "runtime.")
		for _, boundary := range boundaries {
			if strings.Contains(name, boundary) {
				outside = false
			}
		}
		if outside && name != "" {
			if slash := strings.LastIndex(name, "/"); slash >= 0 {
				name = name[slash+1:]
			}
			return name
		}
		if !more {
			return "unknown"
		}
	}
}

// recordingStore is the store with each transaction and registration an
// effect.
type recordingStore struct {
	store.Store
	plan *plan
}

func (s recordingStore) Register(ctx context.Context, commonDir string) (model.RepositoryID, error) {
	s.plan.record("store.register", commonDir)
	return s.Store.Register(ctx, commonDir)
}

func (s recordingStore) Update(ctx context.Context, repository model.RepositoryID, fn func(store.Tx) error) error {
	s.plan.record("store.update", "")
	return s.Store.Update(ctx, repository, fn)
}

// gitWrites are the git subcommands that change a repository's refs,
// index, worktree, or a remote, each an effect; commands that only write
// objects, as hash-object and commit-tree, aren't.
var gitWrites = []string{"push", "fetch", "update-ref", "update-index", "read-tree", "reset", "switch", "checkout", "add", "rm", "branch", "worktree", "sparse-checkout", "symbolic-ref", "config", "remote", "restore", "stash", "clean", "commit", "merge", "rebase", "cherry-pick", "tag", "gc", "prune"}

// subcommand is a git command line's subcommand, past any -c options.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" || args[i] == "-C" {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

// gitHook records each writing git command as an effect, and runs it.
func (p *plan) gitHook(ctx context.Context, args []string, run func() error) error {
	name := subcommand(args)
	if !slicesContains(gitWrites, name) {
		return run()
	}
	p.record("git "+name, strings.Join(args, " "))
	return run()
}

func slicesContains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
