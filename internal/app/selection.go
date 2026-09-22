package app

import (
	"context"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
)

// portReader resolves names against a staged index. mirror, when given, lets
// a cold cache bootstrap from the mirror's index; the commands that stay
// offline pass nil.
func portReader(config Config, repo *git.Repository, mirror *portindex.Mirror) *selection.Reader {
	native := &eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	// One command resolves names repeatedly against the same materialized
	// tree; the generation is installed there once.
	var mu sync.Mutex
	staged := map[string]bool{}
	return &selection.Reader{Evaluator: native, Index: func(ctx context.Context, tree macports.Tree) (*portindex.Index, error) {
		key := tree.Root() + "\x00" + string(tree.Source().Tree)
		mu.Lock()
		defer mu.Unlock()
		if staged[key] {
			return portindex.Open(tree.Root())
		}
		// A workspace projects a tree the repository already validated,
		// and may hold nothing yet; a plain materialization is checked.
		if _, projected := workspace.ScopeOf(tree.Root()); !projected {
			if err := macports.ValidatePortsTree(tree.Root(), repo.Root); err != nil {
				return nil, err
			}
		}
		index, err := surveyIndex(config, true)
		if err != nil {
			return nil, err
		}
		index.Mirror = mirror
		platform := tree.Platform()
		if platform.OS == "" {
			platform, err = native.NativePlatform(ctx)
			if err != nil {
				return nil, err
			}
		}
		source := tree.Source()
		source.Base = "" // Name lookup can reconcile an exact-tree index from the complete Git diff.
		if err = portindex.Stage(ctx, repo, source, platform, index, tree.Root()); err != nil {
			return nil, err
		}
		staged[key] = true
		return portindex.Open(tree.Root())
	}}
}
