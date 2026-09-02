package lifecycle

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

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// ChangedPort names the one context a branch's change is about, by
// evaluation: the portdir is materialized at the merge base and at the
// tip, both snapshot totally (D13), and the diff names the contexts
// that moved. Exactly one moved context is the answer; several is a
// refusal naming them, because a verification must know what it
// verifies. No moved context at all — a patch-file-only change
// evaluates identically — falls back to the portdir's base name,
// which is today's behavior for exactly the branches it is right for.
func ChangedPort(ctx context.Context, rs *runstate.Context, repo *git.Repo, tip, rel string) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return "", err
	}
	pfx, err := rs.Prefix()
	if err != nil {
		return "", err
	}
	p, err := pool.New(ctx, pfx, 1)
	if err != nil {
		return "", err
	}
	defer p.Close()

	root, err := rs.TempDir()
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
		h := port.New(tree.Target{Portdir: filepath.Join(stage, filepath.FromSlash(rel))}, p.Evaluators()[0])
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
	return "", fmt.Errorf("lifecycle: the branch changes %d contexts (%s); name the one to verify: `dockhand verify <subport>`",
		len(names), strings.Join(names, ", "))
}
