package author

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/spf13/cobra"
)

func EditCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "edit <port>",
		Short: "Open a port's files in your editor",
		Long: `Brings a port's directory into the branch's worktree, when a sparse worktree
does not hold it yet, and opens its Portfile in $VISUAL or $EDITOR. Without
a terminal or an editor, or with --no-open, it prints the Portfile's path.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			chosen, err := branchflags.ChooseBranch(ctx, e, streams, where, args[0], engine.WorkEdit)
			branch, started := chosen.Branch, chosen.Started
			if err != nil {
				return err
			}
			if started {
				fmt.Fprintf(streams.Out, "Started %s from master %s (fetched just now)\n", branch.Name, branch.Base.Short())
			}
			directory, err := e.Edit(ctx, branch, args[0])
			if err != nil {
				return err
			}
			portfile := filepath.Join(branch.Worktree, filepath.FromSlash(directory), "Portfile")
			streams.Emit(map[string]any{"branch": view.BranchRef(branch), "started": started, "directory": directory, "portfile": portfile})
			editor := settings.EditorCommand(s)
			// --no-open only brings the port's files in, for an editor of
			// the person's own, where EDITOR=true stood in for it (field
			// testing's batch 13).
			if noOpen || !streams.Terminal() || editor == "" {
				fmt.Fprintln(streams.Out, portfile)
				return nil
			}
			fmt.Fprintf(streams.Err, "%s · %s\n", branch.ShortName(), s.Places().Tilde(portfile))
			if err := settings.RunEditor(ctx, editor, portfile); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Next: "+branchflags.NextIn(ctx, e, branch, "dockhand checksums "+args[0]+" (if you changed the version)", "dockhand check"))
			return nil
		},
	}
	where.Register(cmd)
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "bring the port's files into the worktree and print the Portfile's path, opening no editor")
	settings.SupportsJSON(cmd)
	return cmd
}

func RevbumpCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var selector, subject, newName string
	var plan bool
	cmd := &cobra.Command{
		Use:   "revbump <port>... --subject <reason>",
		Short: "Bump ports' revisions, for a rebuild",
		Long: `Increases each port's revision, so users rebuild it, and records the
reason as its commit subject for tidy: "<port>: <reason>", such as
--subject "rebuild for poppler 25.09.0". A revision shared by several
subports is bumped for all of them.

The branch is --branch, an existing one, or --new=<name>, a new one of the
name, else the one checked out here. Otherwise a new branch is started,
since a rebuild has its own reason.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if strings.TrimSpace(subject) == "" {
				return errors.New(`revbump needs the reason as --subject, such as --subject "rebuild for poppler 25.09.0"; maintainers read it`)
			}
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var branch model.Branch
			var started bool
			if newName != "" {
				if plan {
					return errors.New("--plan changes nothing, so it starts no branch; plan in an existing one with --branch")
				}
				branch, err = e.Start(ctx, engine.StartRequest{Name: newName, Ports: []string{args[0]}})
				started = err == nil
			} else {
				branch, started, err = revbumpBranch(ctx, e, selector, args[0], plan)
			}
			if err != nil {
				return err
			}
			out := streams.Out
			if started {
				fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, branch.Base.Short())
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), prose.Plural(len(args), "port"))
			width := 0
			for _, port := range args {
				width = max(width, len(port))
			}
			result := view.RevbumpJSON{Branch: view.BranchRef(branch), Started: started, Subject: strings.TrimSpace(subject), Applied: !plan, Ports: []view.RevbumpedJSON{}}
			defer func() { streams.Emit(result) }()
			for _, port := range args {
				update, err := e.Update(ctx, engine.UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: port, Subject: subject, Plan: plan})
				if err != nil {
					return fmt.Errorf("%s: %w", port, err)
				}
				result.Ports = append(result.Ports, view.RevbumpedJSON{Port: update.Port, Before: update.Before.Revision, After: update.After.Revision})
				fmt.Fprintf(out, "  %-*s  revision %d → %d\n", width, update.Port, update.Before.Revision, update.After.Revision)
				if plan {
					fmt.Fprint(out, update.Diff)
				}
			}
			if plan {
				fmt.Fprintln(out, "Plan, nothing changed.")
				return nil
			}
			fmt.Fprintf(out, "Recorded the subject for tidy: \"<port>: %s\"\n", strings.TrimSpace(subject))
			return nil
		},
	}
	cmd.Flags().StringVarP(&selector, "branch", "b", "", "work in this existing branch, by its exact name (the dockhand/ prefix is optional)")
	cmd.Flags().StringVar(&newName, "new", "", "start a new branch of this name for the rebuild")
	cmd.MarkFlagsMutuallyExclusive("branch", "new")
	cmd.Flags().StringVar(&subject, "subject", "", "the reason, which becomes each commit's subject after the port's name")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edits and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}

// revbumpBranch is --branch, else the branch checked out here, else a new
// one named for the first port.
func revbumpBranch(ctx context.Context, e *engine.Engine, selector, port string, plan bool) (model.Branch, bool, error) {
	if selector != "" {
		// -b names an existing branch, planned in or not; --new=<name>
		// starts one.
		selection, err := e.Select(ctx, engine.Selector{Branch: selector, Policy: engine.SelectOrStart}, engine.WorkRevbump)
		return selection.Branch, false, err
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, engine.ErrNoBranch) {
		return branch, false, err
	}
	if e.Untracked(ctx) != "" {
		return model.Branch{}, false, err
	}
	if plan {
		return model.Branch{}, false, errors.New("--plan changes nothing, so it starts no branch; plan in an existing one with --branch")
	}
	return branchflags.StartFor(ctx, e, port, engine.WorkRevbump)
}
