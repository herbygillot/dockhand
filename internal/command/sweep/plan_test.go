// Package sweep is the fault sweep (docs/designs/fault-sweep.md, #129):
// each workflow run once to find its outside effects, then once for each
// effect and each way it can fail, recovered as a person would, and the
// world compared with the clean run's. It sits above compose, at the
// command line, in process, and reaches the engine through compose's two
// seams, engine.Options.WrapStore and GitHook.
package sweep

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
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
	// Failed is an effect that failed before it happened.
	Failed bool
	index  int
}

func (e effect) address() string {
	return fmt.Sprintf("%s@%s#%d", e.Boundary, e.Site, e.Occurrence)
}

// plan is a run's log of effects, shared by every boundary so they make
// one sequence, and safe to write from several goroutines: a check's
// driver and a session's heartbeat write while the command does.
type plan struct {
	mu     sync.Mutex
	log    []effect
	counts map[string]int
	// fault is the effect to fail and how, where one is armed; reached
	// says it was met, and dead that it was dead and every later effect
	// fails too.
	fault   *fault
	reached bool
	dead    bool
}

// mode is how an effect fails (docs/designs/fault-sweep.md, "Five ways
// an effect fails").
type mode string

const (
	// failed returns an error, and the effect didn't happen.
	failed mode = "fail"
	// dead fails it and every later effect, as a kill just before it.
	dead mode = "dead"
	// lost is a remote effect that happened, its reply lost.
	lost mode = "lost"
	// landedUnknown is a store transaction or a local ref update that
	// happened and returned that its outcome is unknown, and
	// notLandedUnknown one that didn't and returned the same.
	landedUnknown    mode = "landed-unknown"
	notLandedUnknown mode = "not-landed-unknown"
)

// fault is an effect's address and the way it fails.
type fault struct {
	address string
	mode    mode
}

// errFault is what a failed effect returns.
var errFault = errors.New("sweep: the effect failed")

// fires says how the effect just recorded fails, where it does: the
// armed fault's mode at its address, or dead's for every effect after it.
func (p *plan) fires(e effect) (mode, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		p.log[e.index].Failed = true
		return dead, true
	}
	if p.fault == nil || p.fault.address != e.address() {
		return "", false
	}
	p.reached = true
	switch p.fault.mode {
	case dead:
		p.dead = true
		p.log[e.index].Failed = true
	case failed, notLandedUnknown:
		p.log[e.index].Failed = true
	}
	return p.fault.mode, true
}

// disarm ends the fault, before recovery.
func (p *plan) disarm() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fault, p.dead = nil, false
}

func newPlan() *plan { return &plan{counts: map[string]int{}} }

// record logs an effect at the boundary, addressed by its call site.
func (p *plan) record(boundary, detail string) effect {
	site := callSite()
	p.mu.Lock()
	defer p.mu.Unlock()
	key := boundary + "@" + site
	p.counts[key]++
	e := effect{Boundary: boundary, Site: site, Occurrence: p.counts[key], Detail: detail, index: len(p.log)}
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
	e := s.plan.record("store.register", commonDir)
	switch how, fires := s.plan.fires(e); {
	case !fires:
	case how == landedUnknown:
		if _, err := s.Store.Register(ctx, commonDir); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", store.ErrUncertain, e.address())
	case how == notLandedUnknown:
		return "", fmt.Errorf("%w: %s", store.ErrUncertain, e.address())
	default:
		return "", fmt.Errorf("%w: %s", errFault, e.address())
	}
	return s.Store.Register(ctx, commonDir)
}

func (s recordingStore) Update(ctx context.Context, repository model.RepositoryID, fn func(store.Tx) error) error {
	e := s.plan.record("store.update", "")
	switch how, fires := s.plan.fires(e); {
	case !fires:
	case how == landedUnknown:
		if err := s.Store.Update(ctx, repository, fn); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", store.ErrUncertain, e.address())
	case how == notLandedUnknown:
		return fmt.Errorf("%w: %s", store.ErrUncertain, e.address())
	default:
		return fmt.Errorf("%w: %s", errFault, e.address())
	}
	return s.Store.Update(ctx, repository, fn)
}

// gitWrites are the git subcommands that change a repository's refs,
// index, worktree, or a remote, each an effect; gitReads are those that
// don't, objects written included, as hash-object's and commit-tree's.
// A subcommand that's either by its arguments is classed by gitWrite.
// The listing test requires every subcommand the git package issues to
// be in one of them (TestEveryGitSubcommandIsClassed).
var (
	gitWrites = []string{"push", "fetch", "update-ref", "update-index", "read-tree", "reset", "switch", "add", "worktree", "sparse-checkout", "branch", "symbolic-ref", "remote", "config"}
	gitReads  = []string{"show", "ls-tree", "write-tree", "ls-files", "hash-object", "var", "status", "show-ref", "rev-parse", "rev-list", "reflog", "mktree", "merge-tree", "merge-base", "ls-remote", "init", "for-each-ref", "diff-tree", "diff", "cherry", "cat-file", "commit-tree", "version", "check-ref-format", "log", "count-objects", "describe", "name-rev"}
)

// gitWrite reports whether a git command line writes: its subcommand is a
// write, and for those that also read by their arguments, these do.
func gitWrite(args []string) bool {
	name := subcommand(args)
	if !slicesContains(gitWrites, name) {
		return false
	}
	var rest []string
	for i, arg := range args {
		if arg == name {
			rest = args[i+1:]
			break
		}
	}
	positional := 0
	for _, arg := range rest {
		if !strings.HasPrefix(arg, "-") {
			positional++
		}
	}
	switch name {
	case "worktree", "sparse-checkout":
		return len(rest) > 0 && rest[0] != "list"
	case "symbolic-ref":
		return positional >= 2 || slicesContains(rest, "--delete") || slicesContains(rest, "-d")
	case "remote":
		return len(rest) > 0 && rest[0] != "get-url" && rest[0] != "-v" && rest[0] != "show"
	case "config":
		return !slicesContains(rest, "--get") && !slicesContains(rest, "--get-all") && !slicesContains(rest, "--list") && positional >= 2
	case "branch":
		return slicesContains(rest, "-m") || slicesContains(rest, "-M") || slicesContains(rest, "-d") || slicesContains(rest, "-D") || slicesContains(rest, "-f")
	}
	return true
}

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
	if !gitWrite(args) {
		return run()
	}
	e := p.record("git "+subcommand(args), strings.Join(args, " "))
	switch how, fires := p.fires(e); {
	case !fires:
	case how == lost || how == landedUnknown:
		if err := run(); err != nil {
			return err
		}
		if how == landedUnknown {
			return fmt.Errorf("%w: %s", git.ErrRefUpdateUncertain, e.address())
		}
		return fmt.Errorf("%w, its reply lost: %s", errFault, e.address())
	case how == notLandedUnknown:
		return fmt.Errorf("%w: %s", git.ErrRefUpdateUncertain, e.address())
	default:
		return fmt.Errorf("%w: %s", errFault, e.address())
	}
	return run()
}

// modes are the ways an effect at a boundary can fail: every effect
// fails or dies; a remote one's reply is lost; a store transaction's or
// a local ref update's outcome is unknown, landed or not.
func modes(boundary string) []mode {
	switch boundary {
	case "store.update", "store.register":
		return []mode{failed, dead, landedUnknown, notLandedUnknown}
	case "git push", "git fetch":
		return []mode{failed, dead, lost}
	case "git update-ref":
		return []mode{failed, dead, landedUnknown, notLandedUnknown}
	}
	if strings.HasPrefix(boundary, "forge.") {
		return []mode{failed, dead, lost}
	}
	return []mode{failed, dead}
}

func slicesContains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
