package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/classify"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// Classify builds the classify subcommand: survey ports for
// version-style tractability.
func Classify(rc *RunContext) *cobra.Command {
	var (
		workers  int
		all      bool
		declines bool
	)
	c := &cobra.Command{
		Use:   "classify [port|category|portdir ...]",
		Short: "Survey ports for version-style tractability",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := resolveTargets(rc.TreeRoot, all, args)
			if err != nil {
				return err
			}
			p, err := rc.Pool(cmd.Context(), workers)
			if err != nil {
				return err
			}

			// A survey redirected to a file must not report success on a
			// partial write: exiting 0 over a truncated census would
			// misreport the tree. Sweep drains its results on this
			// goroutine, so the first failure is captured without a lock.
			var census classify.Census
			var writeErr error
			classify.Sweep(cmd.Context(), p, targets, func(r classify.Result) {
				census.Add(r)
				if declines && r.Outcome != classify.Located {
					if _, err := fmt.Fprintf(rc.Out, "%-14s %s\t%s\n",
						r.Outcome, r.Target.Portdir, r.Detail); err != nil && writeErr == nil {
						writeErr = err
					}
				}
			})
			if writeErr != nil {
				return writeErr
			}
			_, err = fmt.Fprint(rc.Out, census.String())
			return err
		},
	}
	c.Flags().IntVarP(&workers, "workers", "j", min(8, runtime.NumCPU()),
		"evaluator pool size")
	c.Flags().BoolVarP(&all, "all", "a", false,
		"classify the entire tree")
	c.Flags().BoolVarP(&declines, "declines", "d", false,
		"list each port that was not located, as classified")
	return c
}

// resolveTargets turns arguments into Targets: a portdir path directly,
// categories expanded to their portdirs, everything else through
// Tree.Resolve (category/dir, index names — subports included — and
// directory names, all against one Tree with its index opened once).
// With all, the whole tree. Literal repeats dedupe; distinct references
// to the same portdir (two subports of one Portfile) do not — the
// caller decides what its command's unit of work is.
func resolveTargets(treeRoot string, all bool, args []string) ([]tree.Target, error) {
	needTree := all
	for _, a := range args {
		if _, ok := tree.PathTarget(a); !ok {
			needTree = true
		}
	}

	var tr *tree.Tree
	if needTree {
		if treeRoot == "" {
			return nil, usagef("a ports tree is needed: pass --tree or set DOCKHAND_TREE")
		}
		var err error
		if tr, err = tree.Open(treeRoot); err != nil {
			return nil, err
		}
	}

	var out []tree.Target
	seen := make(map[tree.Target]bool)
	add := func(targets ...tree.Target) {
		for _, tgt := range targets {
			if !seen[tgt] {
				seen[tgt] = true
				out = append(out, tgt)
			}
		}
	}

	if all {
		if len(args) > 0 {
			return nil, usagef("--all takes no arguments")
		}
		dirs, err := tr.Portdirs()
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			add(tree.Target{Portdir: d})
		}
		return out, nil
	}
	if len(args) == 0 {
		return nil, usagef("specify ports, categories, or portdirs (or --all for the whole tree)")
	}

	for _, a := range args {
		if tgt, ok := tree.PathTarget(a); ok {
			add(tgt)
			continue
		}
		if tr.HasCategory(a) {
			dirs, err := tr.CategoryPortdirs(a)
			if err != nil {
				return nil, err
			}
			for _, d := range dirs {
				add(tree.Target{Portdir: d})
			}
			continue
		}
		tgt, err := tr.Resolve(a)
		if err != nil {
			return nil, err
		}
		add(tgt)
	}
	return out, nil
}
