package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
)

func editCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	cmd := &cobra.Command{
		Use:   "edit <port>",
		Short: "Open a port's files in your editor",
		Long: `Brings a port's directory into the branch's worktree, when a sparse worktree
does not hold it yet, and opens its Portfile in $VISUAL or $EDITOR. Without
a terminal or an editor, it prints the Portfile's path.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, started, err := chooseBranch(ctx, e, streams, where, args[0], "edit")
			if err != nil {
				return err
			}
			if started {
				fmt.Fprintf(streams.Out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
			}
			directory, err := e.Edit(ctx, branch, args[0])
			if err != nil {
				return err
			}
			portfile := filepath.Join(branch.Worktree, filepath.FromSlash(directory), "Portfile")
			streams.emit(map[string]any{"branch": branchRef(branch), "started": started, "directory": directory, "portfile": portfile})
			editor := firstOf(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
			if !streams.terminal() || editor == "" {
				fmt.Fprintln(streams.Out, portfile)
				return nil
			}
			fmt.Fprintf(streams.Err, "%s · %s\n", branch.ShortName(), tilde(portfile))
			command := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "sh", portfile)
			command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := command.Run(); err != nil {
				return fmt.Errorf("the editor failed: %w", err)
			}
			fmt.Fprintln(streams.Out, "Next: dockhand checksums "+args[0]+" if you changed the version, then dockhand check")
			return nil
		},
	}
	where.flags(cmd)
	return cmd
}

func revbumpCommand(s *settings, streams Streams) *cobra.Command {
	var selector, subject string
	var plan bool
	cmd := &cobra.Command{
		Use:   "revbump <port>... --subject <reason>",
		Short: "Bump ports' revisions, for a rebuild",
		Long: `Increases each port's revision, so users rebuild it, and records the
reason as its commit subject for tidy: "<port>: <reason>", such as
--subject "rebuild for poppler 25.09.0". A revision shared by several
subports is bumped for all of them.

The branch is --branch, else the one checked out here. Otherwise a new
branch is started, since a rebuild has its own reason.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if strings.TrimSpace(subject) == "" {
				return errors.New(`revbump needs the reason as --subject, such as --subject "rebuild for poppler 25.09.0"; maintainers read it`)
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, started, err := revbumpBranch(ctx, e, selector, args[0], plan)
			if err != nil {
				return err
			}
			out := streams.Out
			if started {
				fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), plural(len(args), "port"))
			width := 0
			for _, port := range args {
				width = max(width, len(port))
			}
			result := revbumpJSON{Branch: branchRef(branch), Started: started, Subject: strings.TrimSpace(subject), Applied: !plan, Ports: []revbumpedJSON{}}
			defer func() { streams.emit(result) }()
			for _, port := range args {
				update, err := e.Update(ctx, engine.UpdateRequest{Branch: branch, Action: record.BumpRevision, Port: port, Subject: subject, Plan: plan})
				if err != nil {
					return fmt.Errorf("%s: %w", port, err)
				}
				result.Ports = append(result.Ports, revbumpedJSON{Port: update.Port, Before: update.Before.Revision, After: update.After.Revision})
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
	cmd.Flags().StringVar(&selector, "branch", "", "work in this tracked branch")
	cmd.Flags().StringVar(&subject, "subject", "", "the reason, which becomes each commit's subject after the port's name")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edits and change nothing")
	return cmd
}

// revbumpBranch is --branch, else the branch checked out here, else a new
// one named for the first port.
func revbumpBranch(ctx context.Context, e *engine.Engine, selector, port string, plan bool) (model.Branch, bool, error) {
	if selector != "" {
		branch, err := e.Resolve(ctx, selector)
		return branch, false, err
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, engine.ErrNoBranch) {
		return branch, false, err
	}
	if current, currentErr := e.Repo.CurrentBranch(ctx); currentErr == nil && current != "master" && current != "main" {
		return model.Branch{}, false, err
	}
	if plan {
		return model.Branch{}, false, errors.New("--plan changes nothing, so it starts no branch; plan in an existing one with --branch")
	}
	return startFor(ctx, e, port)
}

func retryCommand(s *settings, streams Streams) *cobra.Command {
	var enqueue bool
	cmd := &cobra.Command{
		Use:   "retry <run>",
		Short: "Run a finished check's exact request again",
		Long: `Queues a finished check again, such as check-42: the same files, the same
plan, and the same environments, whatever the branch holds now. check
captures newer edits instead. Until results are reused port by port, the
whole plan is built again.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			previous, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return err
			}
			run, err := e.Retry(ctx, previous)
			if err != nil {
				return err
			}
			revision, err := e.Revision(ctx, run.Revision)
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "%s repeats %s: %s\n", run.Name(), previous.Name(), engine.Describe(revision))
			return runQueued(ctx, e, run, streams, enqueue)
		},
	}
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue it and return")
	return cmd
}

func rebaseCommand(s *settings, streams Streams) *cobra.Command {
	var selector string
	cmd := &cobra.Command{
		Use:   "rebase",
		Short: "Move the branch's commits onto fresh master",
		Long: `Fetches MacPorts' master and replays the branch's commits on it, in the
branch's worktree, keeping the old history as a checkpoint that dockhand
restore brings back. A branch with uncommitted edits is refused, and a
rebase that conflicts is abandoned with the branch as it was.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			rebased, err := e.Rebase(ctx, branch)
			if err != nil {
				return err
			}
			result := map[string]any{"branch": branch.ShortName(), "from": rebased.From, "to": rebased.To, "up_to_date": rebased.UpToDate, "commits": rebased.Commits}
			if rebased.Checkpoint != nil {
				result["checkpoint"] = rebased.Checkpoint.Name()
			}
			streams.emit(result)
			if rebased.UpToDate {
				fmt.Fprintf(streams.Out, "%s already starts from master %s (fetched just now).\n", branch.ShortName(), engine.Short(rebased.To))
				return nil
			}
			name := rebased.Checkpoint.Name()
			fmt.Fprintf(streams.Out, "Rebased %s (%s) from master %s onto %s.\nCheckpoint %s keeps the old history (dockhand restore %s).\n",
				branch.ShortName(), plural(rebased.Commits, "commit"), engine.Short(rebased.From), engine.Short(rebased.To), name, name)
			if branch.PullRequest != nil {
				fmt.Fprintf(streams.Out, "#%d still has the old commits; dockhand submit replaces them, if no one else has pushed.\n", branch.PullRequest.Number)
			}
			fmt.Fprintln(streams.Out, "Next: dockhand check, since the files it builds on have changed")
			return nil
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "rebase this tracked branch")
	return cmd
}

func archiveCommand(s *settings, streams Streams) *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "archive [branch]",
		Short: "Hide a branch you are not working on",
		Long: `Hides a branch from status without touching its files, its Git branch, or
its pull request; status --all still shows it. --undo brings it back.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			branch, err = e.Archive(ctx, branch, undo)
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"branch": branch.ShortName(), "state": branch.State})
			if undo {
				fmt.Fprintf(streams.Out, "%s is back among your open branches.\n", branch.ShortName())
				return nil
			}
			fmt.Fprintf(streams.Out, "Archived %s; its files, Git branch, and pull request are untouched. dockhand archive --undo %s brings it back.\n", branch.ShortName(), branch.ShortName())
			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "bring an archived branch back")
	return cmd
}
