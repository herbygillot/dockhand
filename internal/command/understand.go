package command

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func diffCommand(s *settings, streams Streams) *cobra.Command {
	var selector string
	var stat bool
	cmd := &cobra.Command{
		Use:   "diff [<path>...]",
		Short: "Show the branch's change from master, edits included",
		Long: `Shows what the branch changes from the master it starts from, with the
files as they are now: commits and uncommitted edits alike, which is what
check captures and what a pull request would show. It first lists the
ports CI would build, each changed or revision only, and what CI builds
nothing for.

Paths narrow the diff; they are taken from where you are, or from the top
of the ports tree. --stat lists the changed files instead of the patch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			diff, err := e.Diff(ctx, branch, treePaths(branch, args))
			if err != nil {
				return err
			}
			writeDiffSummary(streams.Out, diff)
			if len(diff.Files) == 0 {
				if len(args) > 0 {
					fmt.Fprintln(streams.Out, "Nothing changed under those paths.")
				}
				return nil
			}
			fmt.Fprintln(streams.Out)
			if stat {
				for _, path := range diff.Files {
					fmt.Fprintln(streams.Out, "  "+path)
				}
				return nil
			}
			_, err = streams.Out.Write(diff.Patch)
			return err
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "show this tracked branch")
	cmd.Flags().BoolVar(&stat, "stat", false, "list the changed files, not the patch")
	return cmd
}

// treePaths takes paths a person typed from where they are, when that is
// in the branch's worktree, else from the top of the tree.
func treePaths(branch model.Branch, args []string) []string {
	root, err := filepath.EvalSymlinks(branch.Worktree)
	here, hereErr := os.Getwd()
	if hereErr == nil {
		here, hereErr = filepath.EvalSymlinks(here)
	}
	var paths []string
	for _, arg := range args {
		path := filepath.ToSlash(filepath.Clean(arg))
		if err == nil && hereErr == nil && branch.Worktree != "" {
			full := arg
			if !filepath.IsAbs(full) {
				full = filepath.Join(here, arg)
			}
			if rel, relErr := filepath.Rel(root, full); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				path = filepath.ToSlash(rel)
			}
		}
		if path == "." {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func writeDiffSummary(out io.Writer, diff engine.BranchDiff) {
	status := diff.Status
	line := fmt.Sprintf("%s · from master %s", status.Branch.ShortName(), engine.Short(status.Branch.Base))
	if len(status.Edited) > 0 {
		line += " · with uncommitted edits to " + plural(len(status.Edited), "file")
	}
	fmt.Fprintln(out, line)
	if len(diff.Ports) == 0 && len(diff.Other) == 0 {
		fmt.Fprintln(out, "  No changes from master.")
		return
	}
	width := 0
	for _, port := range diff.Ports {
		width = max(width, len(port.Directory))
	}
	for _, port := range diff.Ports {
		fmt.Fprintf(out, "  %-*s  %s\n", width, port.Directory, portChangeWords(port))
	}
	if len(diff.Other) > 0 {
		fmt.Fprintf(out, "  CI builds nothing for %s\n", strings.Join(diff.Other, ", "))
	}
}

func portChangeWords(port engine.PortDiff) string {
	switch {
	case port.Added:
		return "new port"
	case port.Deleted:
		return "removed"
	case port.Kind == model.RevisionOnly:
		return "revision only"
	}
	return "changed"
}

func impactCommand(s *settings, streams Streams) *cobra.Command {
	var selector string
	cmd := &cobra.Command{
		Use:   "impact [<port>...]",
		Short: "Show what the branch's change reaches beyond its ports",
		Long: `Lists the ports the branch changes, the other ports that depend on them
directly, and the shared files it changes with the ports that load them.
Dependents come from the port index at the branch's base, and are looked
for of each port changed beyond its revision, or of the ports named. They
are candidates to look at, not proof of anything: check --also builds
some against the branch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			impact, err := e.Impact(ctx, branch, args)
			if err != nil {
				return err
			}
			writeImpact(streams.Out, impact)
			return nil
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "show this tracked branch")
	return cmd
}

func writeImpact(out io.Writer, impact engine.Impact) {
	row := func(label, value string) { fmt.Fprintf(out, "%-17s %s\n", label, value) }
	var changed []string
	for _, port := range impact.Diff.Ports {
		words := filepath.Base(port.Directory)
		if kind := portChangeWords(port); kind != "changed" {
			words += " (" + kind + ")"
		}
		changed = append(changed, words)
	}
	row("Changed ports", orNone(strings.Join(changed, ", ")))

	var of []string
	for _, directory := range impact.Of {
		of = append(of, filepath.Base(directory))
	}
	label := "Other dependents"
	switch {
	case len(impact.Of) == 0:
		row(label, "none looked for; the branch changes no existing port beyond its revision")
	case impact.Unread != "":
		row(label, "not read: "+impact.Unread)
	case len(impact.Dependents) == 0:
		row(label, "none, of "+strings.Join(of, ", "))
	default:
		var dependents []string
		for _, dependent := range impact.Dependents {
			dependents = append(dependents, fmt.Sprintf("%s (%s)", dependent.Name, strings.Join(dependent.Phases, ", ")))
		}
		row(label, strings.Join(dependents, ", ")+"; candidates to look at, not proof of anything")
	}

	if len(impact.Shared) == 0 {
		row("Shared files", "none")
	}
	for i, shared := range impact.Shared {
		words := shared.Path
		if shared.PortGroup != "" {
			words += fmt.Sprintf(": PortGroup %s, loaded by %s", shared.PortGroup, plural(len(shared.Users), "port"))
			if len(shared.Users) > 0 && len(shared.Users) <= 5 {
				var names []string
				for _, user := range shared.Users {
					names = append(names, filepath.Base(user))
				}
				words += " (" + strings.Join(names, ", ") + ")"
			}
		}
		if i == 0 {
			row("Shared files", words)
		} else {
			row("", words)
		}
	}
	if len(impact.Dependents) > 0 {
		names := make([]string, 0, 3)
		for _, dependent := range impact.Dependents[:min(3, len(impact.Dependents))] {
			names = append(names, dependent.Name)
		}
		fmt.Fprintf(out, "Next: dockhand check --also %s builds some against the branch\n", strings.Join(names, ","))
	}
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
