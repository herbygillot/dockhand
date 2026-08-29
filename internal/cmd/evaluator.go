package cmd

import (
	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
)

// resolvePrefix resolves the installation the global --prefix flag
// selects: a stated prefix validated, else discovery.
func resolvePrefix(cmd *cobra.Command) (prefix.Prefix, error) {
	prefixPath, err := cmd.Flags().GetString("prefix")
	if err != nil {
		return "", err
	}
	if prefixPath != "" {
		return prefix.New(prefixPath)
	}
	return prefix.Find()
}

// oneEvaluator starts a single evaluator against the selected
// installation, returning it with its pool's closer.
func oneEvaluator(cmd *cobra.Command) (*eval.Evaluator, func(), error) {
	pfx, err := resolvePrefix(cmd)
	if err != nil {
		return nil, nil, err
	}
	p, err := pool.New(cmd.Context(), pfx, 1)
	if err != nil {
		return nil, nil, err
	}
	return p.Evaluators()[0], p.Close, nil
}
