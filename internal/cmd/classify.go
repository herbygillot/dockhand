package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/classify"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// Classify builds the classify subcommand: survey ports for
// version-style tractability.
func Classify() *cobra.Command {
	var (
		workers  int
		all      bool
		declines bool
	)
	c := &cobra.Command{
		Use:   "classify [port|category|portdir ...]",
		Short: "Survey ports for version-style tractability",
		RunE: func(cmd *cobra.Command, args []string) error {
			treeRoot, err := cmd.Flags().GetString("tree")
			if err != nil {
				return err
			}
			targets, err := resolveTargets(treeRoot, all, args)
			if err != nil {
				return err
			}
			// The census sweeps whole Portfiles, so targets flatten to
			// unique portdirs; the subport a name resolved to matters
			// to point commands, not to a survey.
			var portdirs []string
			seen := make(map[string]bool)
			for _, tgt := range targets {
				if !seen[tgt.Portdir] {
					seen[tgt.Portdir] = true
					portdirs = append(portdirs, tgt.Portdir)
				}
			}
			pfx, err := resolvePrefix(cmd)
			if err != nil {
				return err
			}
			p, err := pool.New(cmd.Context(), pfx, workers)
			if err != nil {
				return err
			}
			defer p.Close()

			var census classify.Census
			classify.Sweep(cmd.Context(), p, portdirs, func(r classify.Result) {
				census.Add(r)
				if declines && r.Outcome != classify.Located {
					fmt.Printf("%-14s %s\t%s\n", r.Outcome, r.Portdir, r.Detail)
				}
			})
			fmt.Print(census.String())
			return nil
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
