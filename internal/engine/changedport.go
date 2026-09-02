package engine

// Field case (macports-ports-46): verify of a hand-made subport branch
// submitted the portdir's MAIN port — devel/pcre's base name is pcre,
// the branch changed pcre2, and the VM built the untouched 8.45 and
// would have called the branch verified. A portdir's name is not the
// name of what a branch changed; only evaluation can say that.

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// ChangedPortdirs derives the one portdir a branch changes against its
// merge base with the primary branch — from git alone, so a human
// commit's changes count the same as a minted one's.
func ChangedPortdirs(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return "", err
	}
	paths, err := repo.DiffNames(ctx, base, tip)
	if err != nil {
		return "", err
	}
	portdirs := map[string]bool{}
	for _, p := range paths {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) >= 3 {
			portdirs[parts[0]+"/"+parts[1]] = true
		}
	}
	if len(portdirs) != 1 {
		return "", fmt.Errorf("verify: %s changes %d portdirs against %s; one at a time for now", branch, len(portdirs), git.Abbrev(base))
	}
	for d := range portdirs {
		return d, nil
	}
	return "", nil // unreachable
}

// SubjectOf names what a branch verification builds. The portdir's
// base name is NOT the answer — devel/pcre's branch may change pcre2,
// and building the parent verifies nothing about the change
// (field-caught: the VM built the untouched pcre 8.45 and would have
// called the pcre2 branch verified). Resolution, most direct authority
// first: the port the user themselves named (a target that matched the
// branch as dockhand/<target>-*, the mint's own naming); the tip note's
// recorded port (written from the plan's subport at bump time); and for
// a hand-made branch with neither, the context the branch's own diff
// moves under evaluation.
func (e *Engine) SubjectOf(ctx context.Context, repo *git.Repo, target, branch, tip, rel string) (string, error) {
	if target != branch {
		return target, nil
	}
	if n, err := e.Ledger(repo).Read(ctx, tip); err == nil && n.Port != "" {
		return n.Port, nil
	}
	return e.changedPort(ctx, repo, tip, rel)
}

// changedPort names the one context a branch's change is about, by
// evaluation: the portdir is materialized at the merge base and at the
// tip, both snapshot totally (D13), and the diff names the contexts
// that moved. Exactly one moved context is the answer; several is a
// refusal naming them, because a verification must know what it
// verifies. No moved context at all — a patch-file-only change
// evaluates identically — falls back to the portdir's base name,
// which is today's behavior for exactly the branches it is right for.
//
// Its two refusals are prefixed `lifecycle:` and stay that way. The
// package that carried the words is gone, but the words themselves
// reached users before it did, and moving code is not a licence to
// reword what the move happens to pass through; renaming the prefix is
// a change to what dockhand says and belongs to a step that says so.
func (e *Engine) changedPort(ctx context.Context, repo *git.Repo, tip, rel string) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return "", err
	}
	// A session of its own, closed here: it evaluates two materialized
	// snapshots under temporary directories this function removes, and
	// the run's own evaluator would outlive them.
	ev, err := e.Session(ctx)
	if err != nil {
		return "", err
	}
	defer ev.Close()

	root, err := e.Temp()
	if err != nil {
		return "", err
	}
	snapshotAt := func(sha, purpose string) (info.Snapshot, error) {
		stage, remove, err := root.MakeDir(purpose)
		if err != nil {
			return nil, err
		}
		defer remove()
		if err := repo.Materialize(ctx, sha, rel, stage); err != nil {
			return nil, err
		}
		h := port.New(tree.Target{Portdir: filepath.Join(stage, filepath.FromSlash(rel))}, ev)
		snap, err := h.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("lifecycle: evaluating %s at %s: %w", rel, git.Abbrev(sha), err)
		}
		return snap, nil
	}
	before, err := snapshotAt(base, "portname-base")
	if err != nil {
		return "", err
	}
	after, err := snapshotAt(tip, "portname-tip")
	if err != nil {
		return "", err
	}

	d := before.Diff(after)
	moved := map[string]bool{}
	for key := range d.Changed {
		moved[key.Subport] = true
	}
	for key := range d.Added {
		moved[key.Subport] = true
	}
	switch len(moved) {
	case 1:
		for name := range moved {
			return name, nil
		}
	case 0:
		return filepath.Base(rel), nil
	}
	names := make([]string, 0, len(moved))
	for name := range moved {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", &AmbiguousContextError{Contexts: names}
}

// AmbiguousContextError is a branch whose change moved several
// evaluation contexts at once: the verification needs one subject and
// the branch names more than one.
//
// It shares its band with the ambiguous branch target, because it is
// the same shape one level down — the request did not say enough, and
// naming the one settles it. The `lifecycle:` prefix outlived the
// package that gave it: the code moved to the engine and the sentence
// did not, because the words are what a user reads and a move does not
// get to reword them.
type AmbiguousContextError struct{ Contexts []string }

func (e *AmbiguousContextError) Error() string {
	return fmt.Sprintf("lifecycle: the branch changes %d contexts (%s); name the one to verify: `dockhand verify <subport>`",
		len(e.Contexts), strings.Join(e.Contexts, ", "))
}

// DockhandExit: the declined band's ambiguity code — say which.
func (e *AmbiguousContextError) DockhandExit() int { return exitcode.Ambiguous }

// Code names the refusal for a machine.
func (e *AmbiguousContextError) Code() string { return "ambiguous-context" }
