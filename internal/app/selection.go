package app

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
)

func portReader(config Config, repo *git.Repository) *selection.Reader {
	native := &eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	return &selection.Reader{Evaluator: native, Index: func(ctx context.Context, tree macports.Tree) (*portindex.Index, error) {
		index, err := surveyIndex(config, true)
		if err != nil {
			return nil, err
		}
		platform := tree.Platform()
		if platform.OS == "" {
			platform, err = native.NativePlatform(ctx)
			if err != nil {
				return nil, err
			}
		}
		source := tree.Source()
		source.Base = "" // Name lookup can reconcile an exact-tree index from the complete Git diff.
		if err = portindex.Stage(ctx, repo, source, platform, index, tree.Root(), http.DefaultClient); err != nil {
			return nil, err
		}
		return portindex.Open(tree.Root())
	}}
}
